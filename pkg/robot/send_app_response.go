package robot

import "encoding/json"

// UnmarshalJSON 同时兼容协议返回的 NewMsgId 和 newMsgId。
func (r *SendAppResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.FromUserName = firstJSONString(raw, "FromUserName", "fromUserName")
	r.ToUserName = firstJSONString(raw, "ToUserName", "toUserName")
	r.MsgSource = firstJSONString(raw, "MsgSource", "msgSource")
	r.Content = firstJSONString(raw, "Content", "content")
	r.ClientMsgId = firstJSONString(raw, "ClientMsgId", "clientMsgId")
	r.Type = int(firstJSONInt(raw, "Type", "type"))
	r.ActionFlag = int(firstJSONInt(raw, "ActionFlag", "actionFlag"))
	r.MsgId = firstJSONInt(raw, "MsgId", "msgId")
	r.CreateTime = firstJSONInt(raw, "CreateTime", "createTime", "Createtime")
	r.NewMsgId = firstJSONInt(raw, "NewMsgId", "newMsgId")
	if r.NewMsgId == 0 {
		r.NewMsgId = r.MsgId
	}
	if baseRaw, ok := raw["BaseResponse"]; ok {
		var base struct {
			Ret int `json:"ret"`
		}
		if err := json.Unmarshal(baseRaw, &base); err == nil {
			r.BaseRet = base.Ret
		}
	}
	return nil
}

func firstJSONString(raw map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || string(value) == "null" {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return text
		}
	}
	return ""
}

func firstJSONInt(raw map[string]json.RawMessage, keys ...string) int64 {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || string(value) == "null" {
			continue
		}
		var number int64
		if err := json.Unmarshal(value, &number); err == nil && number != 0 {
			return number
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil && text != "" {
			var parsed int64
			if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}
