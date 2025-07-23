package rabbitmq

import (
	"context"
	"encoding/json"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/zeromicro/go-zero/core/logc"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/queue"
	"github.com/zeromicro/go-zero/core/trace"
	"go.opentelemetry.io/otel"
	oteltrace "go.opentelemetry.io/otel/trace"
	"time"
)

// MustNewListener rabbitmq消费者服务端
func MustNewListener(ctx context.Context, rabbitListenerConf RabbitListenerConf, handler ConsumeHandler) queue.MessageQueue {
	listener := RabbitListener{
		queues:   rabbitListenerConf,
		handler:  handler,
		forever:  make(chan bool),
		ctx:      ctx,
		maxRetry: 10,
	}
	err := listener.connect()
	logx.Must(err)
	return &listener
}

func (q *RabbitListener) connect() error {
	var err error
	maxRetry := 0
	for maxRetry < q.maxRetry {
		q.conn, err = amqp.DialConfig(getRabbitURL(q.queues.RabbitConf), amqp.Config{
			Heartbeat: 30 * time.Second, // 设置心跳时间
		})
		if err == nil {
			logx.Infof("Connected to RabbitMQ")
			break
		}
		maxRetry++
		logx.Errorf("Failed to connect to RabbitMQ: %v. Retrying(%d/%d)", err, maxRetry, q.maxRetry)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		logx.Errorf("Failed to connect to RabbitMQ, Reconnect...")
		if q.conn != nil {
			q.conn.Close()
			q.conn = nil
		}
		return err
	}

	q.handleConnectionClose()

	// 创建通道
	maxRetry = 0
	for maxRetry < q.maxRetry {
		q.channel, err = q.conn.Channel()
		if err == nil {
			logx.Infof("Channel created successfully")

			// 设置QoS（关键修改点）
			err = q.channel.Qos(
				q.queues.ChannelQos.PrefetchCount, // prefetchCount
				q.queues.ChannelQos.PrefetchSize,  // prefetchSize
				q.queues.ChannelQos.Global,        // global
			)
			if err != nil {
				logx.Errorf("Failed to set QoS: %v. Retrying(%d/%d)", err, maxRetry, q.maxRetry)
				maxRetry++
				continue
			}
			logx.Infof("Successfully to set QoS prefetchCount: %d, prefetchSize: %d, global: %t",
				q.queues.ChannelQos.PrefetchCount, q.queues.ChannelQos.PrefetchSize, q.queues.ChannelQos.Global)
			break
		}
		maxRetry++
		logx.Errorf("Failed to open a channel: %v. Retrying(%d/%d)", err, maxRetry, q.maxRetry)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		logx.Errorf("Failed to open a channel, Reconnect...")
		if q.conn != nil {
			q.conn.Close()
			q.conn = nil
		}
		return err
	}
	return nil
}

func (q *RabbitListener) handleConnectionClose() {
	connCloseChan := q.conn.NotifyClose(make(chan *amqp.Error))

	go func() {
		for err := range connCloseChan {
			logx.Errorf("Connection closed: %v", err)
			q.reconnect()
		}
	}()
}

func (q *RabbitListener) reconnect() {
	logx.Info("Attempting to reconnect...")
	q.reconnectMutex.Lock()
	defer q.reconnectMutex.Unlock()
	if q.channel != nil {
		q.channel.Close()
		q.channel = nil
	}
	if q.conn != nil {
		q.conn.Close()
		q.conn = nil
	}
	if errX := q.connect(); errX != nil {
		logx.Errorf("Reconnect failed: %v", errX)
		// 延迟后重试
		time.AfterFunc(5*time.Second, func() {
			q.reconnect() // 再次尝试
		})
		return
	}

	go q.Start() // 重新启动消费

}

func (q *RabbitListener) parseMessage(listenerConsumer ConsumerConf, message amqp.Delivery) (*RabbitMsgBody, error) {

	var msgBody = new(RabbitMsgBody)
	err := json.Unmarshal(message.Body, msgBody)
	if err != nil {
		logx.Errorf("Failed to parse RabbitMQ message payload, delivery: %v, error: %v", message, err)
		if listenerConsumer.AutoAck == false {
			// 紧确认消费当前消息，因为此消息当前消费者无法解析，同队列的其他消费者肯定也无法解析，需要确认消费掉，不然一直循环消费
			message.Ack(false)
		}
		return nil, err
	}
	return msgBody, nil
}

func (q *RabbitListener) requeueMessage(ctx context.Context, listenerConsumer ConsumerConf, message amqp.Delivery, retryCount int32) {
	if message.Headers == nil {
		message.Headers = make(amqp.Table)
	}
	message.Headers["x-retry-count"] = retryCount + 1
	// 手动重新发布消息（确保 headers 被保留）
	err := q.channel.Publish(
		"",
		listenerConsumer.Name,
		false,
		false,
		amqp.Publishing{
			Headers:     message.Headers,
			ContentType: q.queues.ContentType,
			Body:        message.Body,
		},
	)
	if err != nil {
		logc.Errorf(ctx, "Failed requeue message: %v,  err: %v", string(message.Body), err)
	}

	if err == nil {
		logc.Errorf(ctx, "Successfully requeue message : %v", string(message.Body))
	}

	// 确认原消息（避免重复消费）
	if !listenerConsumer.AutoAck {
		message.Ack(false)
	}
}

func (q *RabbitListener) processMessage(listenerConsumer ConsumerConf, message amqp.Delivery) {

	msgBody, err := q.parseMessage(listenerConsumer, message)
	if err != nil {
		return
	}

	ctx := q.ctx
	var traceSpan oteltrace.Span
	carrier := msgBody.Carrier
	carrierKeysLength := len(carrier.Keys())
	if carrierKeysLength > 0 {
		wireContext := otel.GetTextMapPropagator().Extract(ctx, msgBody.Carrier)
		tracer := otel.GetTracerProvider().Tracer(trace.TraceName)
		spanCtx, span := tracer.Start(wireContext,
			listenerConsumer.Name,
			oteltrace.WithSpanKind(oteltrace.SpanKindConsumer),
		)
		ctx = spanCtx
		traceSpan = span
	}

	retryCount, i := message.Headers["x-retry-count"].(int32)
	logc.Infof(ctx, "Received a message: %v, retryCount: %d, i: %t", string(message.Body), retryCount, i)

	if retryCount > listenerConsumer.MaxRetryCount {
		logc.Errorf(q.ctx, "Message retry count exceeded maximum limit %d/%d, message: %v",
			retryCount, listenerConsumer.MaxRetryCount, string(message.Body),
		)
		// 消息重试次数过多，将其手动确认
		if listenerConsumer.AutoAck == false {
			message.Ack(false)
		}
		return
	}

	err = q.handler.Consume(ctx, msgBody.Msg)

	if err != nil {
		q.requeueMessage(ctx, listenerConsumer, message, retryCount)
	}

	if err == nil {
		logc.Infof(ctx, "Successfully processed message: %v", string(msgBody.Msg))
		message.Ack(false)
	}

	if traceSpan != nil {
		traceSpan.End()
	}
}

func (q *RabbitListener) Start() {

	for listenerQueueIdx := range q.queues.ListenerQueues {
		listenerQueue := q.queues.ListenerQueues[listenerQueueIdx]

		go func(listenerConsumer ConsumerConf) {
			queueMessages, err := q.channel.Consume(
				listenerConsumer.Name,
				"",
				listenerConsumer.AutoAck,
				listenerConsumer.Exclusive,
				listenerConsumer.NoLocal,
				listenerConsumer.NoWait,
				nil,
			)

			if err != nil {
				logx.Errorf("Failed to get message from queue: %s consumer: %v", listenerConsumer.Name, err)
				return
			}

			for message := range queueMessages {
				q.processMessage(listenerConsumer, message)
			}

		}(listenerQueue)
	}
	<-q.forever
}

func (q *RabbitListener) Stop() {
	if q.channel != nil {
		q.channel.Close()
		q.channel = nil
	}
	if q.conn != nil {
		q.conn.Close()
		q.conn = nil
	}
	close(q.forever)
}
