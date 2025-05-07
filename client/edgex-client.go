package mqttclient

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/edgexfoundry/go-mod-core-contracts/v3/clients/logger"
	"github.com/edgexfoundry/go-mod-core-contracts/v3/dtos"
	"github.com/edgexfoundry/go-mod-messaging/v3/messaging"
	"github.com/edgexfoundry/go-mod-messaging/v3/pkg/types"
	"github.com/google/uuid"
)

// Config 定义 MQTT 客户端的配置
type Config struct {
	Broker struct {
		Host     string `toml:"host"`
		Port     int    `toml:"port"`
		Protocol string `toml:"protocol"`
	} `toml:"broker"`
	Type      string `toml:"type"`
	Topic     string `toml:"topic"`
	ClientID  string `toml:"client_id"`
	Username  string `toml:"username"`
	Password  string `toml:"password"`
	Publish   bool   `toml:"publish"`
	Interval  int    `toml:"publish_interval"` // 发布间隔（秒）
	LogLevel  string `toml:"log_level"`
	EnvPrefix string // 环境变量前缀
}

// MessageHandler 定义消息处理回调函数
type MessageHandler func(topic string, event dtos.Event)

// MQTTClient 封装的 MQTT 客户端
type MQTTClient struct {
	config      Config
	logger      logger.LoggingClient
	messageBus  messaging.MessageClient
	messages    chan types.MessageEnvelope
	messageErrs chan error
	stopCh      chan struct{}
	wg          sync.WaitGroup
	handler     MessageHandler
}

// NewMQTTClient 创建新的 MQTT 客户端
func NewMQTTClient(configPath string, handler MessageHandler) (*MQTTClient, error) {
	// 默认配置
	cfg := Config{
		Broker: struct {
			Host     string
			Port     int
			Protocol string
		}{
			Host:     "localhost",
			Port:     1883,
			Protocol: "tcp",
		},
		Type:      "mqtt",
		Topic:     "edgex/events/#",
		ClientID:  "EdgeXClient-" + uuid.New().String(),
		LogLevel:  "INFO",
		Publish:   false,
		Interval:  10,
		EnvPrefix: "EDGEX_",
	}

	// 加载配置文件
	if configPath != "" {
		if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
			return nil, fmt.Errorf("读取配置文件失败: %v", err)
		}
	}

	// 加载环境变量（覆盖配置文件）
	loadEnvOverrides(&cfg)

	// 初始化日志
	lc := logger.NewClient("mqtt_client", cfg.LogLevel)

	// 创建消息总线配置
	busConfig := types.MessageBusConfig{
		Broker: types.HostInfo{
			Host:     cfg.Broker.Host,
			Port:     cfg.Broker.Port,
			Protocol: cfg.Broker.Protocol,
		},
		Type: cfg.Type,
		Optional: map[string]string{
			"ClientId": cfg.ClientID,
		},
	}
	if cfg.Username != "" {
		busConfig.Optional["Username"] = cfg.Username
	}
	if cfg.Password != "" {
		busConfig.Optional["Password"] = cfg.Password
	}

	// 创建消息客户端
	messageBus, err := messaging.NewMessageClient(busConfig)
	if err != nil {
		return nil, fmt.Errorf("创建消息客户端失败: %v", err)
	}

	client := &MQTTClient{
		config:      cfg,
		logger:      lc,
		messageBus:  messageBus,
		messages:    make(chan types.MessageEnvelope),
		messageErrs: make(chan error),
		stopCh:      make(chan struct{}),
		handler:     handler,
	}

	return client, nil
}

// loadEnvOverrides 从环境变量加载配置
func loadEnvOverrides(cfg *Config) {
	if v := os.Getenv(cfg.EnvPrefix + "BROKER_HOST"); v != "" {
		cfg.Broker.Host = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "BROKER_PORT"); v != "" {
		if port, err := fmt.Sscanf(v, "%d", &cfg.Broker.Port); err == nil && port == 1 {
			cfg.Broker.Port = cfg.Broker.Port
		}
	}
	if v := os.Getenv(cfg.EnvPrefix + "BROKER_PROTOCOL"); v != "" {
		cfg.Broker.Protocol = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "TYPE"); v != "" {
		cfg.Type = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "TOPIC"); v != "" {
		cfg.Topic = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "CLIENT_ID"); v != "" {
		cfg.ClientID = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "USERNAME"); v != "" {
		cfg.Username = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv(cfg.EnvPrefix + "PUBLISH"); v == "true" {
		cfg.Publish = true
	}
	if v := os.Getenv(cfg.EnvPrefix + "PUBLISH_INTERVAL"); v != "" {
		if interval, err := fmt.Sscanf(v, "%d", &cfg.Interval); err == nil && interval == 1 {
			cfg.Interval = cfg.Interval
		}
	}
	if v := os.Getenv(cfg.EnvPrefix + "LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
}

// Start 启动客户端
func (c *MQTTClient) Start() error {
	// 连接到消息总线
	if err := c.messageBus.Connect(); err != nil {
		return fmt.Errorf("连接消息总线失败: %v", err)
	}
	c.logger.Info("成功连接到MQTT消息总线")

	// 订阅主题
	topics := []types.TopicChannel{{Topic: c.config.Topic, Messages: c.messages}}
	if err := c.messageBus.Subscribe(topics, c.messageErrs); err != nil {
		return fmt.Errorf("订阅消息失败: %v", err)
	}
	c.logger.Info(fmt.Sprintf("成功订阅主题: %s", c.config.Topic))

	// 启动消息处理
	c.wg.Add(1)
	go c.handleMessages()

	// 启动发布（如果启用）
	if c.config.Publish {
		c.wg.Add(1)
		go c.startPublishing()
	}

	// 监听系统信号
	go c.handleSignals()

	return nil
}

// handleMessages 处理接收到的消息
func (c *MQTTClient) handleMessages() {
	defer c.wg.Done()
	for {
		select {
		case err := <-c.messageErrs:
			c.logger.Error(fmt.Sprintf("接收消息错误: %v", err))

		case msg := <-c.messages:
			c.logger.Info(fmt.Sprintf("收到消息 - 主题: %s, 关联ID: %s", msg.ReceivedTopic, msg.CorrelationID))

			if msg.ContentType != "application/json" {
				c.logger.Error(fmt.Sprintf("无效的内容类型: 收到 %s, 期望 application/json", msg.ContentType))
				continue
			}

			var event dtos.Event
			if err := json.Unmarshal(msg.Payload, &event); err != nil {
				c.logger.Error(fmt.Sprintf("解析事件失败: %v", err))
				continue
			}

			if c.handler != nil {
				c.handler(msg.ReceivedTopic, event)
			}

		case <-c.stopCh:
			return
		}
	}
}

// startPublishing 定时发布测试消息
func (c *MQTTClient) startPublishing() {
	defer c.wg.Done()
	ticker := time.NewTicker(time.Duration(c.config.Interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.PublishTestEvent("edgex/events/test"); err != nil {
				c.logger.Error(err.Error())
			}
		case <-c.stopCh:
			return
		}
	}
}

// PublishTestEvent 发布测试事件
func (c *MQTTClient) PublishTestEvent(topic string) error {
	event := dtos.Event{
		Id:          uuid.New().String(),
		DeviceName:  "TestDevice",
		ProfileName: "TestProfile",
		SourceName:  "TestSource",
		Origin:      time.Now().UnixNano(),
		Readings: []dtos.BaseReading{
			dtos.SimpleReading{
				BaseReading: dtos.BaseReading{
					Id:           uuid.New().String(),
					DeviceName:   "TestDevice",
					ResourceName: "TestResource",
					ProfileName:  "TestProfile",
					Origin:       time.Now().UnixNano(),
				},
				ValueType: "String",
				Value:     fmt.Sprintf("Test value at %s", time.Now().Format(time.RFC3339)),
			},
		},
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %v", err)
	}

	msgEnvelope := types.MessageEnvelope{
		CorrelationID: uuid.New().String(),
		Payload:       payload,
		ContentType:   "application/json",
	}

	err = c.messageBus.Publish(msgEnvelope, topic)
	if err != nil {
		return fmt.Errorf("发布消息失败: %v", err)
	}

	c.logger.Info(fmt.Sprintf("成功发布测试事件到主题 %s, 事件ID: %s", topic, event.Id))
	return nil
}

// PublishEvent 发布自定义事件
func (c *MQTTClient) PublishEvent(event dtos.Event, topic string) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %v", err)
	}

	msgEnvelope := types.MessageEnvelope{
		CorrelationID: uuid.New().String(),
		Payload:       payload,
		ContentType:   "application/json",
	}

	err = c.messageBus.Publish(msgEnvelope, topic)
	if err != nil {
		return fmt.Errorf("发布消息失败: %v", err)
	}

	c.logger.Info(fmt.Sprintf("成功发布事件到主题 %s, 事件ID: %s", topic, event.Id))
	return nil
}

// Stop 停止客户端
func (c *MQTTClient) Stop() {
	close(c.stopCh)
	if err := c.messageBus.Disconnect(); err != nil {
		c.logger.Error(fmt.Sprintf("断开消息总线失败: %v", err))
	}
	c.wg.Wait()
	c.logger.Info("MQTT客户端已停止")
}

// handleSignals 处理系统信号
func (c *MQTTClient) handleSignals() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	c.logger.Info("收到终止信号，正在停止...")
	c.Stop()
}
