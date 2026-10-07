package plugins

import (
	"log"
	"strings"

	"wechat-robot-client/interface/plugin"
)

// sendChatRoomReply 群聊回复改为引用原消息。引用失败时发普通文本，不再艾特。
func sendChatRoomReply(ctx *plugin.MessageContext, text string) {
	if ctx == nil || ctx.Message == nil || strings.TrimSpace(text) == "" {
		return
	}
	if err := ctx.MessageService.SendQuoteReply(ctx.Message.FromWxID, text, ctx.Message); err != nil {
		log.Printf("引用回复失败: %v", err)
	}
}
