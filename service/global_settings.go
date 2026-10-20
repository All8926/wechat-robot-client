package service

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/datatypes"

	"wechat-robot-client/model"
	"wechat-robot-client/repository"
	"wechat-robot-client/vars"
)

type GlobalSettingsService struct {
	ctx    context.Context
	gsRepo *repository.GlobalSettings
}

func NewGlobalSettingsService(ctx context.Context) *GlobalSettingsService {
	return &GlobalSettingsService{
		ctx:    ctx,
		gsRepo: repository.NewGlobalSettingsRepo(ctx, vars.DB),
	}
}

func (s *GlobalSettingsService) GetGlobalSettings() (*model.GlobalSettings, error) {
	return s.gsRepo.GetGlobalSettings()
}

func (s *GlobalSettingsService) SaveGlobalSettings(data *model.GlobalSettings) error {
	data.FriendSyncCron = "" // 这个不允许用户修改
	if err := normalizeSettingsChatAINameAliases(&data.ChatAINameAliases); err != nil {
		return err
	}
	err := s.gsRepo.Update(data)
	if err != nil {
		return err
	}
	// 重新读取最新的完整配置，通知所有观察者
	newData, err := s.GetGlobalSettings()
	if err != nil {
		return err
	}
	vars.SettingsObserver.NotifyAll(newData)
	return nil
}

// PatchRuntimeConfig 只更新点名别名和主动插话。显式传入的空值会写成 NULL，用来恢复默认。
func (s *GlobalSettingsService) PatchRuntimeConfig(aliases *[]string, enabled *bool, enabledSet bool, cooldown *int, cooldownSet bool, thinking *bool, thinkingSet bool, contextHours *int, contextHoursSet bool, contextLimit *int, contextLimitSet bool) error {
	current, err := s.GetGlobalSettings()
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("全局配置不存在")
	}
	updates := map[string]any{}
	if aliases != nil {
		normalized := normalizeStringList(*aliases)
		if len(normalized) > 20 {
			normalized = normalized[:20]
		}
		if normalized == nil {
			normalized = []string{}
		}
		payload, err := json.Marshal(normalized)
		if err != nil {
			return err
		}
		updates["chat_ai_name_aliases"] = datatypes.JSON(payload)
	}
	if enabledSet {
		updates["chat_ai_proactive_enabled"] = nullableBool(enabled)
	}
	if cooldownSet {
		if cooldown != nil && (*cooldown < 30 || *cooldown > 3600) {
			return fmt.Errorf("主动插话冷却需要在 30 到 3600 秒之间")
		}
		updates["chat_ai_proactive_cooldown"] = nullableInt(cooldown)
	}
	if thinkingSet {
		updates["chat_ai_thinking_enabled"] = nullableBool(thinking)
	}
	if contextHoursSet {
		if contextHours != nil && (*contextHours < 1 || *contextHours > 168) {
			return fmt.Errorf("群聊上下文小时数需要在 1 到 168 之间")
		}
		updates["chat_context_hours"] = nullableInt(contextHours)
	}
	if contextLimitSet {
		if contextLimit != nil && (*contextLimit < 1 || *contextLimit > 2000) {
			return fmt.Errorf("群聊上下文条数需要在 1 到 2000 之间")
		}
		updates["chat_context_message_limit"] = nullableInt(contextLimit)
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.gsRepo.DB.WithContext(s.ctx).Model(&model.GlobalSettings{}).Where("id = ?", current.ID).Updates(updates).Error; err != nil {
		return err
	}
	fresh, err := s.GetGlobalSettings()
	if err != nil {
		return err
	}
	vars.SettingsObserver.NotifyAll(fresh)
	return nil
}
