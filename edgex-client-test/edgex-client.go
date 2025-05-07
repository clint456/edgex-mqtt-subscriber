package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edgexfoundry/go-mod-core-contracts/v3/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v3/dtos"
	"github.com/edgexfoundry/go-mod-messaging/v3/messaging"
	"github.com/edgexfoundry/go-mod-messaging/v3/pkg/types"
	"github.com/google/uuid"
)

// 日志客户端
var lc logger.LoggingClient

// 发布测试事件
func publishTestEvent(messageBus messaging.MessageClient, topic string) error {
	// 创建一个简单的测试事件
	value := fmt.Sprintf("Test value at %s", time.Now().Format(time.RFC3339))
	reading, err := dtos.NewSimpleReading(
		"TestProfile",
		"TestDevice",
		"TestResource",
		"String",
		value,
	)
	if err != nil {
		// 处理错误
		panic(err)
	}

	event := dtos.Event{
		Id:          uuid.New().String(),
		DeviceName:  "TestDevice",
		ProfileName: "TestProfile",
		SourceName:  "TestSource",
		Origin:      time.Now().UnixNano(),
		Readings:    []dtos.BaseReading{reading},
	}

	// 序列化为 JSON
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %v", err)
	}

	// 创建消息包
	msgEnvelope := types.MessageEnvelope{
		CorrelationID: uuid.New().String(),
		Payload:       payload,
		ContentType:   "application/json",
	}

	// 发布消息
	err = messageBus.Publish(msgEnvelope, topic)
	if err != nil {
		return fmt.Errorf("发布消息失败: %v", err)
	}

	lc.Info(fmt.Sprintf("成功发布测试事件到主题 %s, 事件ID: %s", topic, event.Id))
	return nil
}

func main() {
	// 初始化日志客户端
	lc = logger.NewClient("edgex_subscriber", "INFO")

	// MQTT消息总线配置
	config := types.MessageBusConfig{
		Broker: types.HostInfo{
			Host:     "localhost", // MQTT代理主机
			Port:     1883,        // MQTT默认端口
			Protocol: "tcp",       // MQTT协议
		},
		Type: "mqtt",
		Optional: map[string]string{
			"ClientId": "EdgeXSubscriber", // MQTT客户端ID
			// 可选：添加认证信息
			// "Username": "your-username",
			// "Password": "your-password",
		},
	}

	// 创建消息客户端
	messageBus, err := messaging.NewMessageClient(config)
	if err != nil {
		lc.Error(fmt.Sprintf("创建消息客户端失败: %v", err))
		return
	}

	// 连接到消息总线
	if err = messageBus.Connect(); err != nil {
		lc.Error(fmt.Sprintf("连接消息总线失败: %v", err))
		return
	}
	lc.Info("成功连接到MQTT消息总线")

	// 创建消息和错误通道
	messages := make(chan types.MessageEnvelope)
	messageErrors := make(chan error)

	// 订阅的主题
	topics := []types.TopicChannel{
		{
			Topic:    "edgex/events/#", // 使用通配符订阅所有事件
			Messages: messages,
		},
	}

	// 订阅消息
	if err = messageBus.Subscribe(topics, messageErrors); err != nil {
		lc.Error(fmt.Sprintf("订阅消息失败: %v", err))
		return
	}
	lc.Info("成功订阅edgex/events/#主题")

	// 定时发布测试消息
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			if err := publishTestEvent(messageBus, "edgex/events/test"); err != nil {
				lc.Error(err.Error())
			}
		}
	}()

	// 处理系统信号以优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 消息处理循环
	for {
		select {
		case err := <-messageErrors:
			lc.Error(fmt.Sprintf("接收消息错误: %v", err))

		case msg := <-messages:
			lc.Info(fmt.Sprintf("收到消息 - 主题: %s, 关联ID: %s", msg.ReceivedTopic, msg.CorrelationID))

			// 检查内容类型
			if msg.ContentType != "application/json" {
				lc.Error(fmt.Sprintf("无效的内容类型: 收到 %s, 期望 application/json", msg.ContentType))
				continue
			}

			// 解析事件
			var event dtos.Event
			if err := json.Unmarshal(msg.Payload, &event); err != nil {
				lc.Error(fmt.Sprintf("解析事件失败: %v", err))
				continue
			}

			lc.Info(fmt.Sprintf("处理事件: 设备: %s, 事件ID: %s", event.DeviceName, event.Id))

		case <-sigChan:
			lc.Info("收到终止信号，正在断开连接...")
			if err := messageBus.Disconnect(); err != nil {
				lc.Error(fmt.Sprintf("断开消息总线失败: %v", err))
			}
			lc.Info("程序退出")
			return
		}
	}
}
