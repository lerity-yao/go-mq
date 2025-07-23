package main

import (
	"context"
	"encoding/json"
	"flag"
	"github.com/lerity-yao/go-mq/rabbitmq"
	"github.com/zeromicro/go-zero/core/logc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
	"net/http"
	_ "net/http/pprof"
)

type Config struct {
	rest.RestConf
	RabbitMqConf rabbitmq.RabbitListenerConf
}

type MqLogic struct {
	c Config
}

type MqMsg struct {
	Name string
}

func NewMqLogic(c Config) *MqLogic {
	return &MqLogic{
		c: c,
	}
}

func (l *MqLogic) Consume(mqCtx context.Context, message []byte) error {

	logc.Infof(mqCtx, "接受消息为: %s\n", message)

	var msg MqMsg

	_ = json.Unmarshal(message, &msg)

	logc.Infof(mqCtx, "msgBody: %v", msg)

	return nil
}

func RabbitMqs(c Config) service.Service {
	logx.Infof("链接mq %v, 监听队列 %v",
		c.RabbitMqConf,
		c.RabbitMqConf.ListenerQueues,
	)

	ctx := context.Background()

	return rabbitmq.MustNewListener(
		ctx, c.RabbitMqConf,
		NewMqLogic(c))
}

func main() {
	flag.Parse()

	q := rabbitmq.ConsumerConf{
		Name:          "go-mq-test.topic.test",
		MaxRetryCount: 3,
		AutoAck:       false,
		Exclusive:     false,
		NoLocal:       false,
		NoWait:        false,
	}
	qs := make([]rabbitmq.ConsumerConf, 0)
	qs = append(qs, q)
	c := Config{
		RestConf: rest.RestConf{
			Host: "0.0.0.0",
			Port: 8080,
		},
		RabbitMqConf: rabbitmq.RabbitListenerConf{
			RabbitConf: rabbitmq.RabbitConf{
				Username: "*",
				Password: "*",
				Host:     "*",
				Port:     0,
			},
			ListenerQueues: qs,
			ChannelQos: rabbitmq.ChannelQosConf{
				PrefetchCount: 10,
				PrefetchSize:  0,
				Global:        false,
			},
		},
	}

	if err := c.SetUp(); err != nil {
		panic(err)
	}

	serviceGroup := service.NewServiceGroup()
	defer serviceGroup.Stop()

	serviceGroup.Add(RabbitMqs(c))

	go func() {
		http.ListenAndServe(":9999", nil)
	}()

	serviceGroup.Start()
}
