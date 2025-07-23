package main

import (
	"context"
	"encoding/json"
	"github.com/lerity-yao/go-mq/rabbitmq"
	"github.com/zeromicro/go-zero/core/logx"
)

type MqMsg struct {
	Name string
}

func main() {

	ctx := context.Background()
	mqConf := rabbitmq.RabbitSenderConf{
		RabbitConf: rabbitmq.RabbitConf{
			Username: "*",
			Password: "*",
			Host:     "*",
			Port:     10008,
		},
		ContentType: "",
	}
	mqSender, err := rabbitmq.MustNewSenderClient(ctx, mqConf)

	if err != nil {
		panic(err)
	}

	msg := MqMsg{
		Name: "test",
	}
	msgBody, _ := json.Marshal(msg)

	err = mqSender.Send(ctx, "go-mq-test", "go-mq-test.topic.test", msgBody)

	if err != nil {
		panic(err)
	}

	logx.Infof("success!")

}
