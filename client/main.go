package main

import (
	"fmt"

	"github.com/edgexfoundry/go-mod-core-contracts/v3/dtos"
	"github.com/your-org/edgex-mqtt-subscriber/mqttclient"
)

func main() {
	// 定义消息处理回调
	handler := func(topic string, event dtos.Event) {
		fmt.Printf("处理事件 - 主题: %s, 设备: %s, 事件ID: %s\n", topic, event.DeviceName, event.Id)
	}

	// 创建客户端
	client, err := mqttclient.NewMQTTClient("config.toml", handler)
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}

	// 启动客户端
	if err := client.Start(); err != nil {
		fmt.Printf("启动客户端失败: %v\n", err)
		return
	}

	// 阻塞主线程
	select {}
}
