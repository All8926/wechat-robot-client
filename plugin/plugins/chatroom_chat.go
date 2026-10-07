package plugins

import (
	"log"
	"strings"
	"sync"

	"wechat-robot-client/interface/plugin"
	"wechat-robot-client/pkg/robot"
	"wechat-robot-client/service"
	"wechat-robot-client/vars"
)

type ChatRoomAIChatPlugin struct{}

func NewChatRoomAIChatPlugin() plugin.MessageHandler {
	return &ChatRoomAIChatPlugin{}
}

func (p *ChatRoomAIChatPlugin) GetName() string {
	return "ChatRoomAIChat"
}

func (p *ChatRoomAIChatPlugin) GetLabels() []string {
	return []string{"text", "chat"}
}

func (p *ChatRoomAIChatPlugin) Match(ctx *plugin.MessageContext) bool {
	return NewChatRoomCommonPlugin().Match(ctx)
}

func (p *ChatRoomAIChatPlugin) PreAction(ctx *plugin.MessageContext) bool {
	return NewChatRoomCommonPlugin().PreAction(ctx)
}

func (p *ChatRoomAIChatPlugin) PostAction(ctx *plugin.MessageContext) {

}

func (p *ChatRoomAIChatPlugin) Run(ctx *plugin.MessageContext) {
	// 不响应自己从其它设备发出的消息
	if ctx.Message != nil && ctx.Message.SenderWxID == vars.RobotRuntime.WxID {
		return
	}
	if !p.PreAction(ctx) {
		return
	}
	if !ctx.Settings.IsAIChatEnabled() {
		return
	}
	roomID := ctx.Message.FromWxID
	triggered := p.triggered(ctx)
	if triggered && !ctx.Settings.IsAITrigger() && quotedRobot(ctx) {
		log.Printf("[AITrigger] reason=quote_reply msg_id=%d from=%s sender=%s refer_msg_id=%d",
			ctx.Message.MsgId, ctx.Message.FromWxID, ctx.Message.SenderWxID, ctx.ReferMessage.MsgId)
	}
	// 同一个群同时只生成一条。生成期间新的触发先挂起，结束后只回最新的那条。
	if !chatRoomReplyGate.enter(roomID, ctx, triggered) {
		return
	}
	owned := true
	defer func() {
		if owned {
			chatRoomReplyGate.forceStop(roomID)
		}
	}()
	current := ctx
	for owned {
		p.replyOne(current)
		next, still := chatRoomReplyGate.next(roomID)
		owned = still
		current = next
	}
}

func (p *ChatRoomAIChatPlugin) triggered(ctx *plugin.MessageContext) bool {
	if ctx.Settings.IsAITrigger() {
		return true
	}
	// 引用机器人自己的消息回复，和艾特一样直接触发，不用再点名
	return quotedRobot(ctx)
}

func (p *ChatRoomAIChatPlugin) replyOne(ctx *plugin.MessageContext) {
	if !p.triggered(ctx) {
		if roomSettings, ok := ctx.Settings.(*service.ChatRoomSettingsService); ok {
			reply, ok := roomSettings.ProactiveShortReply(ctx.Context)
			if ok && strings.TrimSpace(reply) != "" {
				if err := ctx.MessageService.SendTextMessage(ctx.Message.FromWxID, reply); err != nil {
					log.Printf("主动插话发送失败: %v", err)
				}
			}
		}
		return
	}
	defer func() {
		err := ctx.MessageService.SetMessageIsInContext(ctx.Message)
		if err != nil {
			log.Printf("更新消息上下文失败: %v", err)
		}
	}()
	aiChat := NewAIChatPlugin()
	if !aiChat.Match(ctx) {
		return
	}
	aiChat.Run(ctx)
}

type roomReplySlot struct {
	running bool
	pending *plugin.MessageContext
}

type roomReplyGate struct {
	mu    sync.Mutex
	rooms map[string]*roomReplySlot
}

var chatRoomReplyGate = &roomReplyGate{
	rooms: map[string]*roomReplySlot{},
}

// enter 拿到本群的回复权。已经有回复在生成时，触发消息覆盖成待回复的最新一条，插话直接丢掉。
func (g *roomReplyGate) enter(roomID string, ctx *plugin.MessageContext, queue bool) bool {
	if roomID == "" || ctx == nil || ctx.Message == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	slot := g.rooms[roomID]
	if slot == nil {
		slot = &roomReplySlot{}
		g.rooms[roomID] = slot
	}
	if slot.running {
		if queue {
			slot.pending = ctx
			log.Printf("[AITrigger] reason=room_busy_queue room=%s msg_id=%d", roomID, ctx.Message.MsgId)
		}
		return false
	}
	slot.running = true
	return true
}

// next 取走挂起的最新一条。没有挂起时释放回复权。
func (g *roomReplyGate) next(roomID string) (*plugin.MessageContext, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	slot := g.rooms[roomID]
	if slot == nil {
		return nil, false
	}
	if slot.pending != nil {
		next := slot.pending
		slot.pending = nil
		return next, true
	}
	slot.running = false
	return nil, false
}

func (g *roomReplyGate) forceStop(roomID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	slot := g.rooms[roomID]
	if slot == nil {
		return
	}
	slot.running = false
	slot.pending = nil
}

// quotedRobot 判断这条消息是不是在引用机器人说过的话。
func quotedRobot(ctx *plugin.MessageContext) bool {
	robotWxID := vars.RobotRuntime.WxID
	if robotWxID == "" || ctx == nil || ctx.ReferMessage == nil {
		return false
	}
	if ctx.ReferMessage.SenderWxID == robotWxID {
		return true
	}
	if ctx.Message == nil {
		return false
	}
	var xmlMessage robot.XmlMessage
	if err := vars.RobotRuntime.XmlDecoder(ctx.Message.Content, &xmlMessage); err != nil {
		return false
	}
	refer := xmlMessage.AppMsg.ReferMsg
	if refer.ChatUsr == robotWxID {
		return true
	}
	return refer.ChatUsr == "" && refer.FromUsr == robotWxID
}
