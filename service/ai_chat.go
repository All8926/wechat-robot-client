package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"wechat-robot-client/interface/settings"
	"wechat-robot-client/model"
	"wechat-robot-client/pkg/robot"
	"wechat-robot-client/pkg/robotctx"
	"wechat-robot-client/repository"
	"wechat-robot-client/vars"
)

type AIChatService struct {
	ctx    context.Context
	config settings.Settings
}

func NewAIChatService(ctx context.Context, config settings.Settings) *AIChatService {
	return &AIChatService{
		ctx:    ctx,
		config: config,
	}
}

func (s *AIChatService) Chat(robotCtx robotctx.RobotContext, aiMessages []openai.ChatCompletionMessageParamUnion) (openai.ChatCompletionMessage, error) {
	// 获取 AI 配置
	aiConfig := s.config.GetAIConfig()

	// 构建系统提示词
	var basePrompt strings.Builder
	basePrompt.WriteString(aiConfig.Prompt)

	// 注入当前世界时间
	now := time.Now()
	weekdayMap := map[time.Weekday]string{
		time.Sunday:    "星期日",
		time.Monday:    "星期一",
		time.Tuesday:   "星期二",
		time.Wednesday: "星期三",
		time.Thursday:  "星期四",
		time.Friday:    "星期五",
		time.Saturday:  "星期六",
	}
	basePrompt.WriteString("\n\n【当前世界时间】\n")
	fmt.Fprintf(&basePrompt, "%d 年 %d 月 %d 日，%s", now.Year(), int(now.Month()), now.Day(), weekdayMap[now.Weekday()])

	if aiConfig.MaxCompletionTokens > 0 {
		fmt.Fprintf(&basePrompt, "\n\n请注意，每次回答不能超过%d个汉字。", aiConfig.MaxCompletionTokens)
	}

	// 构建系统消息
	var systemMessages []openai.ChatCompletionMessageParamUnion
	// 系统提示词
	systemMessages = append(systemMessages, openai.SystemMessage(basePrompt.String()))
	if strings.Contains(robotCtx.FromWxID, "@chatroom") {
		start := time.Now()
		// 群聊上下文：当前用户元信息 + 最近其他群友消息
		if groupCtx := s.buildGroupChatContext(robotCtx.FromWxID, robotCtx.MessageID); groupCtx != "" {
			systemMessages = append(systemMessages, openai.SystemMessage(groupCtx))
		}
		log.Printf("[GroupContext] 构建群聊上下文耗时: %v", time.Since(start))
	}
	// 群友单独的对话记录
	aiMessages = append(systemMessages, aiMessages...)

	client := openai.NewClient(
		option.WithAPIKey(aiConfig.APIKey),
		option.WithBaseURL(aiConfig.BaseURL),
	)
	req := openai.ChatCompletionNewParams{
		Model:    aiConfig.Model,
		Messages: aiMessages,
	}
	// 思考走单独通道，正文才发给微信。未配置时保持关闭。
	thinkingType := "disabled"
	if s.chatThinkingEnabled() {
		thinkingType = "enabled"
	}
	req.SetExtraFields(map[string]any{
		"thinking": map[string]any{"type": thinkingType},
	})

	aiStart := time.Now()
	reply, err := vars.Agent.ChatWithTools(&robotCtx, &client, req)
	log.Printf("[AI] 接口调用耗时: %v", time.Since(aiStart))

	return reply, err
}

func (s *AIChatService) chatThinkingEnabled() bool {
	if room, ok := s.config.(*ChatRoomSettingsService); ok {
		return room.AIThinkingEnabled()
	}
	globalSettings, err := NewGlobalSettingsService(s.ctx).GetGlobalSettings()
	if err != nil || globalSettings == nil || globalSettings.ChatAIThinkingEnabled == nil {
		return false
	}
	return *globalSettings.ChatAIThinkingEnabled
}

func (s *AIChatService) latestChatMessageText(messages []openai.ChatCompletionMessageParamUnion) string {
	for i := len(messages) - 1; i >= 0; i-- {
		text := s.chatMessageParamText(messages[i])
		if strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func (s *AIChatService) chatMessageParamText(message openai.ChatCompletionMessageParamUnion) string {
	switch content := message.GetContent().AsAny().(type) {
	case *string:
		return *content
	case *[]openai.ChatCompletionContentPartTextParam:
		var builder strings.Builder
		for _, part := range *content {
			builder.WriteString(part.Text)
		}
		return builder.String()
	case *[]openai.ChatCompletionContentPartUnionParam:
		var builder strings.Builder
		for _, part := range *content {
			if text := part.GetText(); text != nil {
				builder.WriteString(*text)
			}
		}
		return builder.String()
	case *[]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion:
		var builder strings.Builder
		for _, part := range *content {
			if text := part.GetText(); text != nil {
				builder.WriteString(*text)
			}
		}
		return builder.String()
	default:
		return ""
	}
}

// buildGroupChatContext 把最近的群聊按发言人摊开，避免把旁边的人说的话当成当前要骂的人。
func (s *AIChatService) buildGroupChatContext(chatRoomID string, currentID int64) string {
	if vars.DB == nil {
		return ""
	}
	msgRepo := repository.NewMessageRepo(s.ctx, vars.DB)
	since, limit := s.groupContextWindow(time.Now())
	recentMsgs, err := msgRepo.GetChatRoomTranscript(chatRoomID, currentID, since, limit)
	if err != nil {
		log.Printf("[GroupContext] 获取群聊流水失败: %v", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("【最近群聊】\n")
	sb.WriteString("每条前面是说话的人。只回复标了「当前消息」的那一条。\n")
	sb.WriteString("当前消息里 @ 到的人，就是对方点名的对象，不要换成旁边刚说过话的别人。\n")
	wrote := false
	robotLabel := s.robotSpeakerLabel()
	for _, msg := range recentMsgs {
		text := transcriptLine(msg)
		if text == "" {
			continue
		}
		wrote = true
		speaker := transcriptSpeaker(msg, robotLabel)
		if msg.ID == currentID {
			fmt.Fprintf(&sb, "当前消息 %s: %s\n", speaker, text)
			continue
		}
		fmt.Fprintf(&sb, "%s: %s\n", speaker, text)
	}
	if !wrote {
		return ""
	}
	return sb.String()
}

const (
	defaultGroupContextHours = 6
	defaultGroupContextLimit = 300
)

// groupContextWindow 群配置优先，未设置时用全局。两边都没写则用 6 小时、300 条。
func (s *AIChatService) groupContextWindow(now time.Time) (int64, int) {
	hours := defaultGroupContextHours
	limit := defaultGroupContextLimit
	if room, ok := s.config.(*ChatRoomSettingsService); ok {
		hours, limit = room.groupContextHoursAndLimit()
	}
	return now.Add(-time.Duration(hours) * time.Hour).Unix(), limit
}

func (s *ChatRoomSettingsService) groupContextHoursAndLimit() (int, int) {
	hours := defaultGroupContextHours
	limit := defaultGroupContextLimit
	if s.globalSettings != nil {
		if s.globalSettings.ChatContextHours != nil && *s.globalSettings.ChatContextHours > 0 {
			hours = *s.globalSettings.ChatContextHours
		}
		if s.globalSettings.ChatContextMessageLimit != nil && *s.globalSettings.ChatContextMessageLimit > 0 {
			limit = *s.globalSettings.ChatContextMessageLimit
		}
	}
	if s.chatRoomSettings != nil {
		if s.chatRoomSettings.ChatContextHours != nil && *s.chatRoomSettings.ChatContextHours > 0 {
			hours = *s.chatRoomSettings.ChatContextHours
		}
		if s.chatRoomSettings.ChatContextMessageLimit != nil && *s.chatRoomSettings.ChatContextMessageLimit > 0 {
			limit = *s.chatRoomSettings.ChatContextMessageLimit
		}
	}
	return hours, limit
}

func (s *AIChatService) robotSpeakerLabel() string {
	if room, ok := s.config.(*ChatRoomSettingsService); ok {
		if name := strings.TrimSpace(room.primaryRobotName()); name != "" {
			return name
		}
	}
	if name := strings.TrimSpace(cachedRobotNickname(s.ctx)); name != "" {
		return name
	}
	return "我"
}

func transcriptSpeaker(msg *model.Message, robotLabel string) string {
	if wxid := vars.RobotRuntime.WxID; wxid != "" && msg.SenderWxID == wxid {
		if robotLabel == "" {
			return "我"
		}
		return robotLabel
	}
	if msg.SenderNickname != "" {
		return msg.SenderNickname
	}
	if msg.SenderWxID != "" {
		return msg.SenderWxID
	}
	return "群成员"
}

func transcriptLine(msg *model.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Type == model.MsgTypeText {
		return truncateTranscript(msg.Content, 400)
	}
	if msg.Type != model.MsgTypeApp || msg.AppMsgType != model.AppMsgTypequote {
		return ""
	}
	var xmlMessage robot.XmlMessage
	if err := vars.RobotRuntime.XmlDecoder(msg.Content, &xmlMessage); err != nil {
		return ""
	}
	title := truncateTranscript(xmlMessage.AppMsg.Title, 200)
	refer := truncateTranscript(xmlMessage.AppMsg.ReferMsg.Content, 80)
	switch {
	case title != "" && refer != "":
		return title + "（引用: " + refer + "）"
	case title != "":
		return title
	default:
		return refer
	}
}

func truncateTranscript(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}
