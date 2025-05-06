
# EdgeX MQTT消息总线订阅程序

### 使用说明

1. **确保依赖版本兼容**：
   - 确保使用最新版本的依赖库，运行以下命令：
     ```bash
     go get github.com/edgexfoundry/go-mod-messaging/v3@latest
     go get github.com/edgexfoundry/go-mod-core-contracts/v3@latest
     go mod tidy
     ```

2. **重新编译**：
   - 重新编译程序：
     ```bash
     go build -o edgex-mqtt-subscriber edgex_subscriber.go
     ```

3. **运行程序**：
   - 确保 MQTT 代理（如 Mosquitto）在 `localhost:1883` 上运行（如果使用不同的主机或端口，请在代码中更新 `Host` 和 `Port`）。
   - 运行程序：
     ```bash
     ./edgex-mqtt-subscriber
     ```

4. **验证日志**：
   - 程序应成功连接到 MQTT 代理，订阅 `edgex/events/#` 主题，并记录接收到的事件，使用正确的字段访问（`event.Id`）。

5. **认证配置**：
   - 如果 MQTT 代理需要认证，请在 `config` 变量的 `Optional` 映射中取消注释并设置 `Username` 和 `Password` 字段。

