package service

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	"wechat-robot-client/repository"
	"wechat-robot-client/vars"
)

type robotName struct {
	value    string
	explicit bool
}

type nicknameCache struct {
	mu       sync.Mutex
	nickname string
	expireAt time.Time
	ok       bool
}

var robotNicknameCache nicknameCache

// matchesRobotName 判断消息是否点到了机器人。显式别名按配置匹配，昵称过短时不匹配，避免误触发。
func (s *ChatRoomSettingsService) matchesRobotName(content string) bool {
	content = strings.TrimSpace(content)
	if len([]rune(content)) < 2 {
		return false
	}
	folded := strings.ToLower(content)
	for _, name := range s.collectRobotNames() {
		if !usableRobotName(name.value, name.explicit) {
			continue
		}
		if contentContainsName(content, folded, name.value) {
			return true
		}
	}
	return false
}

func (s *ChatRoomSettingsService) robotDisplayNames() []string {
	names := s.collectRobotNames()
	result := make([]string, 0, len(names))
	for _, name := range names {
		if usableRobotName(name.value, name.explicit) {
			result = append(result, name.value)
		}
	}
	return result
}

func (s *ChatRoomSettingsService) collectRobotNames() []robotName {
	names := make([]robotName, 0, 8)
	seen := make(map[string]struct{})
	add := func(value string, explicit bool) {
		value = strings.TrimSpace(value)
		if value == "" || value == "机器人" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		names = append(names, robotName{value: value, explicit: explicit})
	}

	for _, alias := range append([]string{"小机"}, s.chatAINameAliases()...) {
		add(alias, true)
	}
	add(cachedRobotNickname(s.ctx), false)
	if s.Message != nil && vars.RobotRuntime.WxID != "" && vars.DB != nil {
		member, err := repository.NewChatRoomMemberRepo(s.ctx, vars.DB).GetChatRoomMember(s.Message.FromWxID, vars.RobotRuntime.WxID)
		if err != nil {
			log.Printf("[AITrigger] 读取机器人群昵称失败: %v", err)
		} else if member != nil {
			add(member.Nickname, false)
			add(member.Remark, false)
		}
	}
	return names
}

func (s *ChatRoomSettingsService) chatAINameAliases() []string {
	aliases := make([]string, 0)
	if s.chatRoomSettings != nil {
		roomAliases, err := s.chatRoomSettings.GetChatAINameAliases()
		if err != nil {
			log.Printf("[AITrigger] 解析群聊点名别名失败: %v", err)
		} else {
			aliases = append(aliases, roomAliases...)
		}
	}
	if s.globalSettings != nil {
		globalAliases, err := s.globalSettings.GetChatAINameAliases()
		if err != nil {
			log.Printf("[AITrigger] 解析全局点名别名失败: %v", err)
		} else {
			aliases = append(aliases, globalAliases...)
		}
	}
	return aliases
}

func cachedRobotNickname(ctx context.Context) string {
	now := time.Now()
	robotNicknameCache.mu.Lock()
	if robotNicknameCache.ok && now.Before(robotNicknameCache.expireAt) {
		name := robotNicknameCache.nickname
		robotNicknameCache.mu.Unlock()
		return name
	}
	robotNicknameCache.mu.Unlock()

	name, found := queryRobotNickname(ctx)
	if !found {
		return ""
	}
	robotNicknameCache.mu.Lock()
	robotNicknameCache.nickname = name
	robotNicknameCache.expireAt = time.Now().Add(5 * time.Minute)
	robotNicknameCache.ok = true
	robotNicknameCache.mu.Unlock()
	return name
}

func queryRobotNickname(ctx context.Context) (string, bool) {
	if vars.AdminDB == nil || vars.RobotRuntime.RobotID == 0 {
		return "", false
	}
	robotAdmin, err := repository.NewRobotAdminRepo(ctx, vars.AdminDB).GetByRobotID(vars.RobotRuntime.RobotID)
	if err != nil {
		log.Printf("[AITrigger] 读取机器人昵称失败: %v", err)
		return "", false
	}
	if robotAdmin == nil || robotAdmin.Nickname == nil {
		return "", true
	}
	return strings.TrimSpace(*robotAdmin.Nickname), true
}

// usableRobotName 过滤过短的自动昵称。后台手写的别名放宽到两个字符。
func usableRobotName(name string, explicit bool) bool {
	name = strings.TrimSpace(name)
	length := len([]rune(name))
	if length < 2 {
		return false
	}
	if !explicit && isASCIIText(name) && length < 3 {
		return false
	}
	return true
}

func contentContainsName(content, foldedContent, name string) bool {
	if isASCIIText(name) {
		return containsASCIIName(foldedContent, strings.ToLower(name))
	}
	return strings.Contains(content, name)
}

func containsASCIIName(content, name string) bool {
	if name == "" {
		return false
	}
	start := 0
	for {
		index := strings.Index(content[start:], name)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !isASCIINameByte(content[index-1])
		end := index + len(name)
		afterOK := end >= len(content) || !isASCIINameByte(content[end])
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
}

func isASCIIText(text string) bool {
	for _, r := range text {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return text != ""
}

func isASCIINameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
