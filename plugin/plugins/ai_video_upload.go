package plugins

import (
	"fmt"
	"log"
	"wechat-robot-client/interface/plugin"
	"wechat-robot-client/service"
)

type AIVideoUploadPlugin struct{}

func NewAIVideoUploadPlugin() plugin.MessageHandler {
	return &AIVideoUploadPlugin{}
}

func (p *AIVideoUploadPlugin) GetName() string {
	return "AIVideoUpload"
}

func (p *AIVideoUploadPlugin) GetLabels() []string {
	return []string{"text", "internal", "chat"}
}

func (p *AIVideoUploadPlugin) Match(ctx *plugin.MessageContext) bool {
	return ctx.ReferMessage != nil
}

func (p *AIVideoUploadPlugin) PreAction(ctx *plugin.MessageContext) bool {
	return true
}

func (p *AIVideoUploadPlugin) PostAction(ctx *plugin.MessageContext) {

}

func (p *AIVideoUploadPlugin) GetOSSFileURL(ctx *plugin.MessageContext) (string, error) {
	ossSettingService := service.NewOSSSettingService(ctx.Context)
	ossSettings, err := ossSettingService.GetOSSSettingService()
	if err != nil {
		return "", fmt.Errorf("获取OSS设置失败: %w", err)
	}
	if ossSettings.AutoUploadVideo != nil && *ossSettings.AutoUploadVideo {
		err := ossSettingService.UploadVideoToOSS(ossSettings, ctx.ReferMessage)
		if err != nil {
			return "", fmt.Errorf("上传视频到OSS失败: %w", err)
		}
		return ctx.ReferMessage.AttachmentUrl, nil
	}
	return "", nil
}

func (p *AIVideoUploadPlugin) SendMessage(ctx *plugin.MessageContext, aiReplyText string) {
	if ctx.Message.IsChatRoom {
		sendChatRoomReply(ctx, aiReplyText)
		return
	}
	if err := ctx.MessageService.SendTextMessage(ctx.Message.FromWxID, aiReplyText); err != nil {
		log.Printf("发送AI回复消息失败: %v", err)
	}
}

func (p *AIVideoUploadPlugin) Run(ctx *plugin.MessageContext) {
	if ctx.ReferMessage.AttachmentUrl == "" {
		videoURL, err := p.GetOSSFileURL(ctx)
		if err != nil {
			log.Printf("视频上传失败: %v", err)
			p.SendMessage(ctx, fmt.Sprintf("视频上传失败: %v，你可能没开启自动上传视频，请前往机器人详情 -> OSS 设置手动开启", err))
			return
		}
		if videoURL == "" {
			p.SendMessage(ctx, "视频上传失败: 视频URL为空，你可能没开启自动上传视频，请前往机器人详情 -> OSS 设置手动开启")
			return
		}
	}
}
