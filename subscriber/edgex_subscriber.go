package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/edgexfoundry/go-mod-core-contracts/v3/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v3/dtos"
	"github.com/edgexfoundry/go-mod-messaging/v3/messaging"
	"github.com/edgexfoundry/go-mod-messaging/v3/pkg/types"
)

// 日志客户端
var lc logger.LoggingClient

// 顶层结构体，匹配整个 Payload
type Payload struct {
	APIVersion string     `json:"apiVersion"`
	RequestID  string     `json:"requestId"`
	Event      dtos.Event `json:"event"`
}

// dtos.Event 结构体，匹配 event 子对象
type Event struct {
	APIVersion  string    `json:"apiVersion"`
	Id          string    `json:"id"`
	DeviceName  string    `json:"deviceName"`
	ProfileName string    `json:"profileName"`
	SourceName  string    `json:"sourceName"`
	Origin      int64     `json:"origin"`
	Readings    []Reading `json:"readings"`
}

// Reading 结构体，匹配 readings 数组中的对象
type Reading struct {
	Id           string `json:"id"`
	Origin       int64  `json:"origin"`
	DeviceName   string `json:"deviceName"`
	ResourceName string `json:"resourceName"`
	ProfileName  string `json:"profileName"`
	ValueType    string `json:"valueType"`
	Value        string `json:"value"`
}

func processMessage(msg types.MessageEnvelope) {
	lc.Info(fmt.Sprintf("收到 Payload: %s", string(msg.Payload)))
	var payload Payload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		lc.Error(fmt.Sprintf("解析 Payload 失败: %v", err))
		return
	}
	event := payload.Event
	if event.Id == "" || event.DeviceName == "" {
		lc.Warn("事件缺少 id 或 deviceName，跳过处理")
		return
	}
	lc.Info(fmt.Sprintf("解析后事件: %+v", event))
	lc.Info(fmt.Sprintf("处理事件: 设备: %s, 事件ID: %s", event.DeviceName, event.Id))
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
			Topic:    "edgex/events/device/device-virtual/Random-Integer-Device/Random-Integer-Device/#", // 使用通配符订阅所有事件
			Messages: messages,
		},
	}

	// 订阅消息
	if err = messageBus.Subscribe(topics, messageErrors); err != nil {
		lc.Error(fmt.Sprintf("订阅消息失败: %v", err))
		return
	}
	lc.Info("成功订阅edgex/events/#主题")

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
			// 解析数据
			processMessage(msg)
			// 打印原始 Payload
			// lc.Info(fmt.Sprintf("收到 Payload: %s", string(msg.Payload)))

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
