package controller

import (
	"errors"
	"wechat-robot-client/model"
	"wechat-robot-client/pkg/appx"
	"wechat-robot-client/pkg/utils"
	"wechat-robot-client/service"

	"github.com/gin-gonic/gin"
)

type GlobalSettings struct {
}

func NewGlobalSettingsController() *GlobalSettings {
	return &GlobalSettings{}
}

func (ct *GlobalSettings) GetGlobalSettings(c *gin.Context) {
	resp := appx.NewResponse(c)
	globalSettings, err := service.NewGlobalSettingsService(c).GetGlobalSettings()
	if err != nil {
		resp.ToErrorResponse(err)
		return
	}
	if globalSettings == nil {
		resp.ToErrorResponse(errors.New("获取全局设置失败"))
		return
	}
	resp.ToResponse(globalSettings)
}

func (ct *GlobalSettings) SaveGlobalSettings(c *gin.Context) {
	var req model.GlobalSettings
	resp := appx.NewResponse(c)
	if ok, err := appx.BindAndValid(c, &req); !ok || err != nil {
		resp.ToErrorResponse(errors.New("参数错误"))
		return
	}
	if req.ChatAIEnabled != nil && *req.ChatAIEnabled {
		if req.ChatAPIKey == "" || req.ChatBaseURL == "" || req.ChatModel == "" || req.ImageRecognitionModel == "" || req.ChatPrompt == "" {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
	}
	if req.ImageAIEnabled != nil && *req.ImageAIEnabled {
		if req.ImageAISettings == nil {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
	}
	if req.WelcomeEnabled != nil && *req.WelcomeEnabled {
		if req.WelcomeType == "" {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
		if req.WelcomeType == model.WelcomeTypeText {
			if req.WelcomeText == "" {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
		if req.WelcomeType == model.WelcomeTypeEmoji {
			if req.WelcomeEmojiMD5 == "" || req.WelcomeEmojiLen == 0 {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
		if req.WelcomeType == model.WelcomeTypeImage {
			if req.WelcomeImageURL == "" {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
		if req.WelcomeType == model.WelcomeTypeURL {
			if req.WelcomeText == "" || req.WelcomeURL == "" {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
	}
	if req.ChatRoomRankingEnabled != nil && *req.ChatRoomRankingEnabled {
		if req.ChatRoomRankingDailyCron == "" {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
		if req.ChatRoomRankingDailyCron != "" {
			if !utils.IsDailyAtHourMinute(req.ChatRoomRankingDailyCron) {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
		if req.ChatRoomRankingWeeklyCron != nil && *req.ChatRoomRankingWeeklyCron != "" {
			if !utils.IsWeeklyMondayAtHourMinute(*req.ChatRoomRankingWeeklyCron) {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
		if req.ChatRoomRankingMonthCron != nil && *req.ChatRoomRankingMonthCron != "" {
			if !utils.IsMonthly1stAtHourMinute(*req.ChatRoomRankingMonthCron) {
				resp.ToErrorResponse(errors.New("参数错误"))
				return
			}
		}
	}
	if req.ChatRoomSummaryEnabled != nil && *req.ChatRoomSummaryEnabled {
		if req.ChatRoomSummaryModel == "" || req.ChatRoomSummaryCron == "" {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
		if !utils.IsDailyAtHourMinute(req.ChatRoomSummaryCron) {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
	}
	if req.NewsEnabled != nil && *req.NewsEnabled {
		if req.NewsType == "" || req.NewsCron == "" {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
		if !utils.IsDailyAtHourMinute(req.NewsCron) {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
	}
	if req.MorningEnabled != nil && *req.MorningEnabled {
		if req.MorningCron == "" {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
		if !utils.IsDailyAtHourMinute(req.MorningCron) {
			resp.ToErrorResponse(errors.New("参数错误"))
			return
		}
	}
	err := service.NewGlobalSettingsService(c).SaveGlobalSettings(&req)
	if err != nil {
		resp.ToErrorResponse(err)
		return
	}
	resp.ToResponse(nil)
}

type runtimeConfigRequest struct {
	ChatRoomID       string    `json:"chat_room_id"`
	NameAliases      *[]string `json:"chat_ai_name_aliases"`
	ProactiveEnabled *bool     `json:"chat_ai_proactive_enabled"`
	EnabledSet       bool      `json:"proactive_enabled_set"`
	Cooldown         *int      `json:"chat_ai_proactive_cooldown"`
	CooldownSet      bool      `json:"cooldown_set"`
	ThinkingEnabled  *bool     `json:"chat_ai_thinking_enabled"`
	ThinkingSet      bool      `json:"thinking_set"`
	ContextHours     *int      `json:"chat_context_hours"`
	ContextHoursSet  bool      `json:"context_hours_set"`
	ContextLimit     *int      `json:"chat_context_message_limit"`
	ContextLimitSet  bool      `json:"context_limit_set"`
}

func (ct *GlobalSettings) SaveRuntimeConfig(c *gin.Context) {
	var req runtimeConfigRequest
	resp := appx.NewResponse(c)
	if ok, err := appx.BindAndValid(c, &req); !ok || err != nil {
		resp.ToErrorResponse(errors.New("参数错误"))
		return
	}
	var err error
	if req.ChatRoomID != "" {
		err = service.NewChatRoomSettingsService(c).PatchRuntimeConfig(req.ChatRoomID, req.NameAliases, req.ProactiveEnabled, req.EnabledSet, req.Cooldown, req.CooldownSet, req.ThinkingEnabled, req.ThinkingSet, req.ContextHours, req.ContextHoursSet, req.ContextLimit, req.ContextLimitSet)
	} else {
		err = service.NewGlobalSettingsService(c).PatchRuntimeConfig(req.NameAliases, req.ProactiveEnabled, req.EnabledSet, req.Cooldown, req.CooldownSet, req.ThinkingEnabled, req.ThinkingSet, req.ContextHours, req.ContextHoursSet, req.ContextLimit, req.ContextLimitSet)
	}
	if err != nil {
		resp.ToErrorResponse(err)
		return
	}
	resp.ToResponse(nil)
}
