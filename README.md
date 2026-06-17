# 移动介质安检站模拟测试工具

面向 SASOC V200R026C01 USB 综合管控方案的功能验证与性能压测工具。

## 功能特性

- **U盘插件模拟器**：模拟 U 盘读写、多盘插入、故障注入
- **安检站模拟器**：覆盖全部 10 条协议命令（CMDID 100-109），支持注册、心跳、信息上报、申领码验证、U盘领取/归还、告警上报、操作日志上报、病毒库升级
- **性能压测引擎**：100 台模拟站并发注册 + 持续心跳
- **Web 控制台**：可视化操作，实时监控

## 快速开始

### 编译

```bash
# 安装依赖
go mod tidy

# 编译
make build

# 或交叉编译
make build-win    # Windows
make build-linux  # Linux
```

### 运行

```bash
# 默认配置启动
./usb-simulator

# 指定配置文件
./usb-simulator -config configs/single-station.yaml

# 访问 Web 控制台
# http://localhost:8080
```

### 命令行压测

```bash
# 100 台并发注册 + 持续心跳
./usb-simulator -pressure configs/100-stations-pressure.yaml
```

## 配置说明

编辑 `config.yaml`：

```yaml
server:
  port: 8080          # Web 服务端口
  debug: true         # 调试模式

sasoc:
  host: "192.168.1.1" # SASOC 服务端地址
  port: 4567          # SASOC 服务端端口

simulator:
  heartbeat_interval: 30   # 心跳间隔（秒）
  reconnect_interval: 60   # 重连间隔（秒）
  encrypt: true             # AES256 加密
  compress: true            # ZLib 压缩
  max_stations: 100         # 最大模拟站数
```

## 项目结构

```
usb-simulator/
├── cmd/server/main.go          # 入口
├── internal/
│   ├── protocol/               # 协议编解码引擎
│   ├── simulator/              # 模拟器核心（安检站+U盘+命令）
│   ├── pressure/               # 压测引擎
│   ├── api/                    # REST API + WebSocket
│   ├── hub/                    # 实例管理中心 + EventBus
│   ├── model/                  # 数据模型 + SQLite
│   └── config/                 # 配置管理
├── configs/                    # 预设场景配置
└── web/                        # Vue3 前端源码
```

## API 接口

### 安检站管理

| 接口 | 方法 | 说明 |
|:---|:---|:---|
| `/api/v1/station/create` | POST | 创建模拟安检站 |
| `/api/v1/station/list` | GET | 列出所有安检站 |
| `/api/v1/station/:id/start` | POST | 启动安检站 |
| `/api/v1/station/:id/stop` | POST | 停止安检站 |
| `/api/v1/station/:id/heartbeat` | POST | 手动触发心跳 |
| `/api/v1/station/:id/report` | POST | 触发信息上报 |
| `/api/v1/station/:id/verify-claim` | POST | 申领码验证 |
| `/api/v1/station/:id/claim-usb` | POST | U盘领取上报 |
| `/api/v1/station/:id/return-usb` | POST | U盘归还上报 |
| `/api/v1/station/:id/alarm` | POST | 告警上报 |
| `/api/v1/station/:id/operation-log` | POST | 操作日志上报 |
| `/api/v1/station/:id/trigger-upgrade` | POST | 触发升级 |

### U盘插件

| 接口 | 方法 | 说明 |
|:---|:---|:---|
| `/api/v1/usb-plug/list` | GET | 列出已插入U盘 |
| `/api/v1/usb-plug/insert` | POST | 插入U盘 |
| `/api/v1/usb-plug/remove/:usbId` | POST | 拔出U盘 |
| `/api/v1/usb-plug/read/:usbId` | GET | 读取U盘信息 |
| `/api/v1/usb-plug/write/:usbId` | POST | 写入U盘信息 |

### 压力测试

| 接口 | 方法 | 说明 |
|:---|:---|:---|
| `/api/v1/pressure/start` | POST | 启动压测 |
| `/api/v1/pressure/stop` | POST | 停止压测 |
| `/api/v1/pressure/status` | GET | 压测状态 |
| `/api/v1/pressure/report` | GET | 压测报告 |

### WebSocket

| 路径 | 说明 |
|:---|:---|
| `/ws` | 实时消息推送（状态变更、消息流、压测指标） |

## 协议说明

工具实现了与 SASOC 完全一致的 TCP 通信协议：

- 传输层：TCP，端口 4567
- 帧结构：48 字节包头 + 变长 JSON 包体
- 加密：AES256-CBC + PKCS7 填充
- 压缩：ZLib
- 校验：CRC32
- 字节序：大端（Big-Endian）

## 许可证

内部工具，仅供测试使用。
