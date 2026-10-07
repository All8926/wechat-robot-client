package service

import (
	"encoding/xml"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"wechat-robot-client/model"
	"wechat-robot-client/pkg/robot"
	"wechat-robot-client/vars"
)

type quoteReplyApp struct {
	XMLName   xml.Name         `xml:"appmsg"`
	AppID     string           `xml:"appid,attr"`
	SDKVer    string           `xml:"sdkver,attr"`
	Title     string           `xml:"title"`
	Type      int              `xml:"type"`
	ShowType  int              `xml:"showtype"`
	AppAttach quoteReplyAttach `xml:"appattach"`
	ReferMsg  quoteRefer       `xml:"refermsg"`
}

type quoteReplyAttach struct {
	TotalLen int `xml:"totallen"`
}

type quoteRefer struct {
	Type        int    `xml:"type"`
	SvrID       string `xml:"svrid"`
	FromUsr     string `xml:"fromusr"`
	ChatUsr     string `xml:"chatusr"`
	DisplayName string `xml:"displayname"`
	Content     string `xml:"content"`
	MsgSource   string `xml:"msgsource"`
	CreateTime  int64  `xml:"createtime"`
}

// SendQuoteReply 在群里引用原消息回复，不再艾特对方。
func (s *MessageService) SendQuoteReply(toWxID, content string, quoted *model.Message) error {
	if quoted == nil || quoted.MsgId == 0 {
		return fmt.Errorf("被引用的消息不存在")
	}
	xmlText, err := buildQuoteReplyXML(toWxID, content, quoted, s.quoteDisplayName(toWxID, quoted.SenderWxID))
	if err != nil {
		return err
	}
	message, err := vars.RobotRuntime.SendAppMessage(toWxID, int(model.AppMsgTypequote), xmlText)
	if err != nil {
		return err
	}
	if message.BaseRet != 0 || message.NewMsgId == 0 {
		return fmt.Errorf("引用回复被微信拒绝 ret=%d", message.BaseRet)
	}
	stored := model.Message{
		MsgId:         message.NewMsgId,
		ClientMsgId:   message.MsgId,
		Type:          model.MsgTypeApp,
		AppMsgType:    model.AppMsgTypequote,
		Content:       message.Content,
		MessageSource: message.MsgSource,
		FromWxID:      toWxID,
		ToWxID:        vars.RobotRuntime.WxID,
		SenderWxID:    vars.RobotRuntime.WxID,
		ReplyWxID:     quoted.SenderWxID,
		IsChatRoom:    strings.HasSuffix(toWxID, "@chatroom"),
		CreatedAt:     message.CreateTime,
		UpdatedAt:     time.Now().Unix(),
	}
	if err := s.msgRepo.Create(&stored); err != nil {
		log.Printf("引用回复入库失败: %v", err)
	}
	return nil
}

func (s *MessageService) quoteDisplayName(chatRoomID, senderWxID string) string {
	if senderWxID == "" {
		return ""
	}
	if strings.HasSuffix(chatRoomID, "@chatroom") && s.crmRepo != nil {
		member, err := s.crmRepo.GetChatRoomMember(chatRoomID, senderWxID)
		if err == nil && member != nil {
			if member.Remark != "" {
				return member.Remark
			}
			if member.Nickname != "" {
				return member.Nickname
			}
		}
	}
	return senderWxID
}

func buildQuoteReplyXML(toWxID, content string, quoted *model.Message, displayName string) (string, error) {
	fromUsr := quoted.FromWxID
	chatUsr := quoted.SenderWxID
	if fromUsr == "" {
		fromUsr = toWxID
	}
	// 协议只接受 appmsg 本身。外面包一层 msg 时，微信会返回 ret=-2，消息不会出现在群里。
	payload := quoteReplyApp{
		SDKVer:   "0",
		Title:    strings.TrimSpace(content),
		Type:     int(model.AppMsgTypequote),
		ShowType: 0,
		ReferMsg: quoteRefer{
			Type:        int(quoted.Type),
			SvrID:       strconv.FormatInt(quoted.MsgId, 10),
			FromUsr:     fromUsr,
			ChatUsr:     chatUsr,
			DisplayName: displayName,
			Content:     quotedMessagePreview(quoted),
			CreateTime:  quoted.CreatedAt,
		},
	}
	body, err := xml.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func quotedMessagePreview(message *model.Message) string {
	switch message.Type {
	case model.MsgTypeImage:
		return "[图片]"
	case model.MsgTypeVoice:
		return "[语音]"
	case model.MsgTypeVideo, model.MsgTypeMicroVideo:
		return "[视频]"
	case model.MsgTypeEmoticon:
		return "[表情]"
	case model.MsgTypeApp:
		if message.AppMsgType == model.AppMsgTypequote {
			var xmlMessage robot.XmlMessage
			if err := vars.RobotRuntime.XmlDecoder(message.Content, &xmlMessage); err == nil && strings.TrimSpace(xmlMessage.AppMsg.Title) != "" {
				return limitRunes(xmlMessage.AppMsg.Title, 120)
			}
		}
	}
	text := strings.TrimSpace(message.Content)
	if text == "" {
		if message.DisplayFullContent != "" {
			text = message.DisplayFullContent
		} else {
			return "[消息]"
		}
	}
	return limitRunes(text, 120)
}
