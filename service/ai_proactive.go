package service

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/openai/openai-go/v3"

	"wechat-robot-client/model"
	"wechat-robot-client/repository"
	"wechat-robot-client/vars"
)

const (
	defaultProactiveCooldownSec = 180
	proactiveJudgeInterval      = 30 * time.Second
	proactiveJoinJudgeInterval  = 3 * time.Minute
	proactiveColdMinSilence     = 3 * time.Minute
	proactiveColdMaxSilence     = 30 * time.Minute
	proactiveColdContextWindow  = 30 * time.Minute
	proactiveContextWindow      = 15 * time.Minute
	proactiveContextLimit       = 8
	proactiveJudgeTimeout       = 20 * time.Second
	proactiveReplyMaxRunes      = 40
	proactiveUnansweredWait     = 15 * time.Second
)

var proactiveThinkRegexp = regexp.MustCompile(`(?s)<think>.*?</think>|<thinking>.*?</thinking>`)

type proactiveRoomState struct {
	lastJudgeAt time.Time
	lastReplyAt time.Time
}

type proactiveLimiter struct {
	mu    sync.Mutex
	rooms map[string]*proactiveRoomState
}

var roomProactiveLimiter = &proactiveLimiter{
	rooms: map[string]*proactiveRoomState{},
}

func (s *ChatRoomSettingsService) IsAIProactiveEnabled() bool {
	// 管理后台页面没有这个开关，主动插话固定开启。群 AI 本身没开时，调用方会先拦住。
	return true
}

func (s *ChatRoomSettingsService) proactiveCooldown() time.Duration {
	seconds := defaultProactiveCooldownSec
	if s.chatRoomSettings != nil && s.chatRoomSettings.ChatAIProactiveCooldown != nil {
		seconds = *s.chatRoomSettings.ChatAIProactiveCooldown
	} else if s.globalSettings != nil && s.globalSettings.ChatAIProactiveCooldown != nil {
		seconds = *s.globalSettings.ChatAIProactiveCooldown
	}
	if seconds <= 0 {
		seconds = defaultProactiveCooldownSec
	} else if seconds < 30 {
		seconds = 30
	} else if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}

// ProactiveShortReply 在艾特、点名、引用都没命中时，按气氛决定要不要插一句。
// 不调用工具。闲聊不会打到模型。不再按小时封顶，两次插话之间仍有冷却。
func (s *ChatRoomSettingsService) ProactiveShortReply(ctx context.Context) (string, bool) {
	if s.Message == nil || !s.IsAIChatEnabled() || !s.IsAIProactiveEnabled() {
		return "", false
	}
	if s.Message.SenderWxID != "" && s.Message.SenderWxID == vars.RobotRuntime.WxID {
		return "", false
	}
	content := strings.TrimSpace(s.triggerMessageContent())
	if content == "" || atAllRegexp.MatchString(content) {
		return "", false
	}
	recent := s.loadRecentProactiveMessages(proactiveContextWindow, proactiveContextLimit)
	if latestMessagesIncludeRobot(s.loadRecentProactiveMessages(24*time.Hour, 5), 5) {
		return "", false
	}
	reason := proactiveCandidateReason(content, recent, s.Message.ID)
	if reason == "" {
		return "", false
	}
	minRunes := 4
	switch reason {
	case "question":
		minRunes = 2
	case "mediate":
		minRunes = 1
	}
	if meaningfulRuneCount(content) < minRunes {
		return "", false
	}

	roomID := s.Message.FromWxID
	if !roomProactiveLimiter.allowJudge(roomID, s.proactiveCooldown(), proactiveJudgeGap(reason)) {
		return "", false
	}
	s.logAITrigger("proactive.candidate."+reason, "", content)

	reply, err := s.judgeProactive(ctx, content, recent, reason)
	if err != nil {
		log.Printf("[AITrigger] reason=proactive.error room=%s err=%v", roomID, err)
		return "", false
	}
	if reply == "" {
		s.logAITrigger("proactive.no", "", content)
		return "", false
	}
	roomProactiveLimiter.markReply(roomID)
	s.logAITrigger("proactive.yes", "", reply)
	return reply, true
}

func (s *ChatRoomSettingsService) loadRecentProactiveMessages(window time.Duration, limit int) []*model.Message {
	if s.Message == nil || vars.DB == nil {
		return nil
	}
	msgRepo := repository.NewMessageRepo(s.ctx, vars.DB)
	messages, err := msgRepo.GetLatestChatRoomTextMessages(s.Message.FromWxID, time.Now().Add(-window).Unix(), limit)
	if err != nil {
		log.Printf("[AITrigger] 读取主动插话上下文失败: %v", err)
		return nil
	}
	return messages
}

func (s *ChatRoomSettingsService) judgeProactive(ctx context.Context, content string, recent []*model.Message, reason string) (string, error) {
	aiConfig := s.GetAIConfig()
	if aiConfig.APIKey == "" || aiConfig.BaseURL == "" || aiConfig.Model == "" {
		return "", fmt.Errorf("AI 配置不完整")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	judgeCtx, cancel := context.WithTimeout(ctx, proactiveJudgeTimeout)
	defer cancel()

	names := s.robotDisplayNames()
	nameText := "这个群里的机器人"
	if len(names) > 0 {
		nameText = strings.Join(names, "、")
	}
	prompt := fmt.Sprintf(`你是微信群成员，名字叫小机，大家也可能叫你：%s。
看最近群聊，决定要不要主动插一句。
%s
要说话时只输出一句口语，不超过%d个字，不要解释，不要列点，不要提及工具。`, nameText, proactiveSpeakRule(reason), proactiveReplyMaxRunes)

	client := newOpenAIClient(aiConfig.APIKey, aiConfig.BaseURL)
	req := openai.ChatCompletionNewParams{
		Model: aiConfig.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(prompt),
			openai.UserMessage(buildProactiveTranscript(recent, content)),
		},
		MaxCompletionTokens: openai.Int(128),
	}
	req.SetExtraFields(map[string]any{
		"thinking": map[string]any{"type": "disabled"},
	})
	message, err := streamChatCompletionMessage(judgeCtx, &client, req)
	if err != nil {
		return "", err
	}
	reply := parseProactiveReply(message.Content)
	log.Printf("[AITrigger] reason=proactive.judge room=%s reply=%q", s.Message.FromWxID, truncateRunes(reply, 40))
	return reply, nil
}

func buildProactiveTranscript(messages []*model.Message, current string) string {
	lines := make([]string, 0, proactiveContextLimit)
	for _, message := range messages {
		label := "群成员"
		if message.SenderWxID == vars.RobotRuntime.WxID {
			label = "你"
		} else if message.SenderNickname != "" {
			label = message.SenderNickname
		}
		lines = append(lines, fmt.Sprintf("%s: %s", label, truncateRunes(message.Content, 100)))
	}
	if strings.TrimSpace(current) == "" {
		lines = append(lines, "（之后群里安静了）")
		return strings.Join(lines, "\n")
	}
	currentText := truncateRunes(current, 100)
	if len(lines) == 0 || !strings.HasSuffix(lines[len(lines)-1], ": "+currentText) {
		lines = append(lines, "新消息 群成员: "+currentText)
	} else {
		lines[len(lines)-1] = "新消息 " + lines[len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// proactiveCandidateReason 本地筛选。空字符串表示这句不像该插话。
func proactiveCandidateReason(content string, recent []*model.Message, currentID int64) string {
	if looksLikeConflict(content, recent) {
		return "mediate"
	}
	if looksLikeQuestion(content) {
		return "question"
	}
	if continuesBotThread(recent, currentID) {
		return "bot_thread"
	}
	if hasUnansweredQuestion(recent, currentID) {
		return "unanswered"
	}
	if looksLikeOngoingTopic(content, recent) {
		return "join"
	}
	return ""
}

func proactiveSpeakRule(reason string) string {
	switch reason {
	case "mediate":
		return "这几句像在吵架。只在确实需要拉架时说话：一句把气氛接住，不站队，不说教，不评判谁对谁错。不像吵架就只输出 no。"
	case "cold":
		return "群里刚才还在聊，已经安静了一会儿。只输出一句很轻的话接一下刚才的话题。不要说「怎么都不说话了」。没什么好接的就只输出 no。"
	case "join":
		return "大家正在聊一件具体的事。只有你真能补上一句有用或有意思的话时才说话。闲聊、斗图、你插不上就只输出 no。"
	default:
		return "只有这些情况才说话：有人在求助而且你能帮忙、话题明显在说你、问题还悬着没人接、正在延续你刚参与的话题。闲聊、斗图、已经有人在答、和你无关的讨论，只输出 no。"
	}
}

func proactiveJudgeGap(reason string) time.Duration {
	if reason == "join" {
		return proactiveJoinJudgeInterval
	}
	return proactiveJudgeInterval
}

func looksLikeConflict(content string, recent []*model.Message) bool {
	senders := map[string]struct{}{}
	hit := containsConflict(content)
	checked := 0
	for i := len(recent) - 1; i >= 0 && checked < 4; i-- {
		message := recent[i]
		if message.SenderWxID == "" || message.SenderWxID == vars.RobotRuntime.WxID {
			continue
		}
		checked++
		senders[message.SenderWxID] = struct{}{}
		if containsConflict(message.Content) {
			hit = true
		}
	}
	return hit && len(senders) >= 2
}

func containsConflict(content string) bool {
	for _, word := range []string{
		"傻逼", "脑残", "智障", "废物", "闭嘴", "神经病", "有病吧",
		"妈的", "他妈", "草泥", "操你", "去死", "你算老几", "你懂个屁",
		"别装了", "烦死你", "有本事", "你配吗", "滚出去", "滚蛋",
	} {
		if strings.Contains(content, word) {
			return true
		}
	}
	return false
}

func looksLikeOngoingTopic(content string, recent []*model.Message) bool {
	if meaningfulRuneCount(content) < 6 {
		return false
	}
	humans := map[string]struct{}{}
	substantive := 0
	checked := 0
	for i := len(recent) - 1; i >= 0 && checked < 6; i-- {
		message := recent[i]
		if message.SenderWxID == vars.RobotRuntime.WxID {
			continue
		}
		checked++
		if meaningfulRuneCount(message.Content) < 6 {
			continue
		}
		substantive++
		if message.SenderWxID != "" {
			humans[message.SenderWxID] = struct{}{}
		}
	}
	return substantive >= 3 && len(humans) >= 2
}

func looksLikeQuestion(content string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	if strings.Contains(content, "？") || strings.Contains(content, "?") {
		return true
	}
	for _, word := range []string{
		"怎么", "怎样", "如何", "为什么", "为啥", "怎么办", "咋办",
		"谁知道", "哪里", "哪儿", "能不能", "可不可以", "有没有", "是不是",
		"吗", "呢",
	} {
		if strings.Contains(content, word) {
			return true
		}
	}
	return false
}

func latestMessagesIncludeRobot(recent []*model.Message, limit int) bool {
	robotWxID := vars.RobotRuntime.WxID
	if robotWxID == "" {
		return false
	}
	checked := 0
	for i := len(recent) - 1; i >= 0 && checked < limit; i-- {
		checked++
		if recent[i].SenderWxID == robotWxID {
			return true
		}
	}
	return false
}

func continuesBotThread(recent []*model.Message, currentID int64) bool {
	checked := 0
	for i := len(recent) - 1; i >= 0 && checked < 3; i-- {
		if recent[i].ID == currentID {
			continue
		}
		checked++
		if recent[i].SenderWxID == vars.RobotRuntime.WxID {
			return true
		}
	}
	return false
}

func hasUnansweredQuestion(recent []*model.Message, currentID int64) bool {
	var previous *model.Message
	for i := len(recent) - 1; i >= 0; i-- {
		if recent[i].ID == currentID {
			continue
		}
		previous = recent[i]
		break
	}
	if previous == nil || previous.SenderWxID == vars.RobotRuntime.WxID {
		return false
	}
	if !looksLikeQuestion(previous.Content) {
		return false
	}
	return time.Since(time.Unix(previous.CreatedAt, 0)) >= proactiveUnansweredWait
}

func parseProactiveReply(text string) string {
	text = strings.TrimSpace(proactiveThinkRegexp.ReplaceAllString(text, ""))
	text = strings.Trim(text, "`\"'“” \n\r\t")
	text = strings.TrimSpace(text)
	if text == "" || isSilentProactiveReply(text) {
		return ""
	}
	return limitRunes(text, proactiveReplyMaxRunes)
}

func isSilentProactiveReply(text string) bool {
	compact := strings.Trim(strings.ToLower(text), " .。!！")
	switch compact {
	case "no", "n", "否", "不", "不说", "不插话", "不用回复", "无需回复":
		return true
	default:
		return false
	}
}

func limitRunes(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func meaningfulRuneCount(text string) int {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			count++
		}
	}
	return count
}

func truncateRunes(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func (l *proactiveLimiter) allowJudge(roomID string, cooldown, judgeGap time.Duration) bool {
	if roomID == "" {
		return false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	state := l.rooms[roomID]
	if state == nil {
		state = &proactiveRoomState{}
		l.rooms[roomID] = state
	}
	if !state.lastReplyAt.IsZero() && now.Sub(state.lastReplyAt) < cooldown {
		return false
	}
	if !state.lastJudgeAt.IsZero() && now.Sub(state.lastJudgeAt) < judgeGap {
		return false
	}
	state.lastJudgeAt = now
	return true
}

func (l *proactiveLimiter) markReply(roomID string) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.rooms[roomID]
	if state == nil {
		state = &proactiveRoomState{}
		l.rooms[roomID] = state
	}
	state.lastReplyAt = now
}

// RunColdChatRoomProactive 定时看哪些群刚冷场，符合条件就轻声接一句。
func RunColdChatRoomProactive(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if vars.RobotRuntime.WxID == "" || vars.DB == nil {
		return
	}
	msgRepo := repository.NewMessageRepo(ctx, vars.DB)
	latest, err := msgRepo.ListChatRoomLatestTexts(time.Now().Add(-proactiveColdMaxSilence).Unix())
	if err != nil {
		log.Printf("[AITrigger] 读取冷场群失败: %v", err)
		return
	}
	msgSvc := NewMessageService(ctx)
	for _, last := range latest {
		if last == nil || last.FromWxID == "" {
			continue
		}
		settings := NewChatRoomSettingsService(ctx)
		if err := settings.InitByMessage(last); err != nil {
			log.Printf("[AITrigger] 冷场加载群配置失败 room=%s err=%v", last.FromWxID, err)
			continue
		}
		reply, ok := settings.ColdShortReply(ctx, last)
		if !ok {
			continue
		}
		if err := msgSvc.SendTextMessage(last.FromWxID, reply); err != nil {
			log.Printf("[AITrigger] 冷场发送失败 room=%s err=%v", last.FromWxID, err)
		}
	}
}

// ColdShortReply 群里刚才还在聊、停了 3 到 30 分钟时，接一句。最近五条里已经有自己的话就不再插。
func (s *ChatRoomSettingsService) ColdShortReply(ctx context.Context, last *model.Message) (string, bool) {
	if last == nil || !s.IsAIChatEnabled() || !s.IsAIProactiveEnabled() {
		return "", false
	}
	if last.SenderWxID != "" && last.SenderWxID == vars.RobotRuntime.WxID {
		return "", false
	}
	silence := time.Since(time.Unix(last.CreatedAt, 0))
	if silence < proactiveColdMinSilence || silence > proactiveColdMaxSilence {
		return "", false
	}
	s.Message = last
	if latestMessagesIncludeRobot(s.loadRecentProactiveMessages(24*time.Hour, 5), 5) {
		return "", false
	}
	recent := s.loadRecentProactiveMessages(proactiveColdContextWindow, proactiveContextLimit)
	if !roomProactiveLimiter.allowJudge(last.FromWxID, s.proactiveCooldown(), time.Minute) {
		return "", false
	}
	s.logAITrigger("proactive.candidate.cold", "", last.Content)
	reply, err := s.judgeProactive(ctx, "", recent, "cold")
	if err != nil {
		log.Printf("[AITrigger] reason=proactive.error room=%s err=%v", last.FromWxID, err)
		return "", false
	}
	if reply == "" {
		s.logAITrigger("proactive.no", "", last.Content)
		return "", false
	}
	roomProactiveLimiter.markReply(last.FromWxID)
	s.logAITrigger("proactive.yes", "", reply)
	return reply, true
}
