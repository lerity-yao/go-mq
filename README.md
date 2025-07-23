# go-mq

## rabbitmq

一个rabbitmq客户端和消费者sdk

客户端和消费者两者之间通过链路跟踪关联起来，按照标准的 OpenTelemetry 集成链路跟踪

- 客户端可以在 go-zero 框架或者单独的脚本中使用，如果在单独脚本中使用，链路跟踪不会上报，注意保存日志

- 消费者需要配合 go-zero 使用

### 消费者

#### 消费者配置

```go
// RabbitListenerConf 消费者配置
// RabbitListenerConf RabbitConf
// RabbitListenerConf ListenerQueues
// RabbitListenerConf ChannelQos
// RabbitListenerConf ContentType 如果需要重新推送消息，比如消费失败，发送报文类型
type RabbitListenerConf struct {
    RabbitConf
    ListenerQueues []ConsumerConf
    ChannelQos     ChannelQosConf
    ContentType    string `json:",default=text/plain"` // MIME content type
}


// RabbitConf rabbitmq基础配置信息
// RabbitConf Username 账号
// RabbitConf Password 密码
// RabbitConf Host 地址
// RabbitConf Port 端口
// RabbitConf VHost 命名空间
type RabbitConf struct {
    Username string
    Password string
    Host     string
    Port     int
    VHost    string `json:",optional"`
}

// ConsumerConf 队列消费配置参数
// ConsumerConf Name  消息队列名称
// ConsumerConf MaxRetryCount 消费消息重试的最大次数
// ConsumerConf AutoAck 控制消息确认机制,设置为true,则在消息被消费者拿到之后，队列会立马删除消息，通常要设置为false，在消费成功之后调用Ack(false)来通知队列删除消息，需要配合channel.Qos使用
// ConsumerConf Exclusive 队列访问控制模式，当前消费者是否唯一模式。设置true,则只允许当前消费者连接此队列，不允许其他消费者连接，设置false，则允许多个消费者连接队列。
// ConsumerConf NoLocal 禁止本地消费，即禁止消费者消费自己推送的消息；rabbitmq不支持此模式
// ConsumerConf NoWait 控制服务器响应机制，设置true为非阻塞模式，连接服务的时候，不会等待服务反馈成功的响应，就执行消费者，无法感知消费者是否创建成功，设置false，阻塞模式，会等待服务响应，成功才进行消费
type ConsumerConf struct {
    Name          string
    MaxRetryCount int32 `json:",default=3"`
    AutoAck       bool  `json:",default=false"`
    Exclusive     bool  `json:",default=false"`
    NoLocal       bool  `json:",default=false"`
    NoWait        bool  `json:",default=false"`
}

// ChannelQosConf 通道qos设置
// ChannelQosConf 此处为消费者通到，非队列
// ChannelQosConf 如果一个消费者监控的多个队列，再此消费者中，所有队列的qos都是一样的
// ChannelQosConf PrefetchCount 可以预取的消息数量，当消费者未确认消息达到此数量上限，RabbitMQ 会停止向该消费者投递新消息，默认只为5，一次性最多处理5条消息
// ChannelQosConf PrefetchSize 可以预取的消息总数的字节总大小。设置为0，则不限制消息字节大小
// ChannelQosConf Global 此qos的生效范围，设置false，qos只在此消费者生效，如果设置true，会影响其他消费者，建议设置false
type ChannelQosConf struct {
    PrefetchCount int  `json:",default=5"`
    PrefetchSize  int  `json:",default=0"`
    Global        bool `json:",default=false"`
}

```

#### 消费者使用

```go

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
)

// 构建go-zero服务启动配置
type Config struct {
	rest.RestConf
	RabbitMqConf rabbitmq.RabbitListenerConf
}

// 构建消费者逻辑函数
type MqLogic struct {
	c Config
}

// 构建消费者逻辑函数
type MqMsg struct {
	Name string
}

// 构建消费者逻辑函数
func NewMqLogic(c Config) *MqLogic {
	return &MqLogic{
		c: c,
	}
}

// 构建消费者逻辑函数
func (l *MqLogic) Consume(mqCtx context.Context, message []byte) error {

	logc.Infof(mqCtx, "接受消息为: %s\n", message)

	var msg MqMsg

	_ = json.Unmarshal(message, &msg)

	logc.Infof(mqCtx, "msgBody: %v", msg)

	return nil
}

// 构建消费者服务
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
    
	// 添加消费者服务
	serviceGroup.Add(RabbitMqs(c))

	serviceGroup.Start()
}

```

### 客户端

#### 客户端配置

```go
// RabbitSenderConf 客户端配置
// RabbitSenderConf RabbitConf
// RabbitSenderConf ContentType 发送报文类型
type RabbitSenderConf struct {
	RabbitConf
	ContentType string `json:",default=text/plain"` // MIME content type
}

// RabbitConf rabbitmq基础配置信息
// RabbitConf Username 账号
// RabbitConf Password 密码
// RabbitConf Host 地址
// RabbitConf Port 端口
// RabbitConf VHost 命名空间
type RabbitConf struct {
    Username string
    Password string
    Host     string
    Port     int
    VHost    string `json:",optional"`
}

```

#### 客户端使用

```go
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

```