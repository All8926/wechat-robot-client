package common_cron

import (
	"context"
	"log"

	"wechat-robot-client/service"
	"wechat-robot-client/vars"
)

type ChatRoomProactiveCron struct {
	CronManager *CronManager
}

func NewChatRoomProactiveCron(cronManager *CronManager) vars.CommonCronInstance {
	return &ChatRoomProactiveCron{
		CronManager: cronManager,
	}
}

func (cron *ChatRoomProactiveCron) IsActive() bool {
	return true
}

func (cron *ChatRoomProactiveCron) Cron() error {
	service.RunColdChatRoomProactive(context.Background())
	return nil
}

func (cron *ChatRoomProactiveCron) Register() {
	if !cron.IsActive() {
		return
	}
	err := cron.CronManager.AddJob(vars.ChatRoomProactiveCron, "* * * * *", func() {
		if err := cron.Cron(); err != nil {
			log.Printf("群聊冷场检查失败: %v", err)
		}
	})
	if err != nil {
		log.Printf("群聊冷场检查任务注册失败: %v", err)
	}
}
