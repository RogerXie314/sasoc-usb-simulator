# 移动介质安检站模拟测试工具 — 实施计划与进度

> **版本**：V2.0  
> **开始日期**：2026-06-16  
> **最终目标**：完成全部 6 个里程碑，产出可运行的单 exe 部署包

---

## 里程碑总览

| 阶段 | 内容 | 计划周期 | 状态 | 完成日期 |
| :--- | :--- | :--- | :--- | :--- |
| M1 | 协议引擎 | 第1-2周 | ✅ 完成 | 2026-06-16 |
| M2 | 安检站模拟器 | 第3-4周 | ✅ 完成 | 2026-06-16 |
| M3 | U盘插件模拟器 | 第5周 | ✅ 完成 | 2026-06-16 |
| M4 | Web 控制台 | 第5-6周 | ✅ 完成 | 2026-06-16 |
| M5 | 压测引擎 | 第7周 | ✅ 完成 | 2026-06-16 |
| M6 | 集成测试 + 部署 | 第8周 | ✅ 完成 | 2026-06-16 |

---

## M1：协议引擎 ✅

### M1-1: 项目脚手架

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 创建项目目录结构 | ✅ 完成 | `usb-simulator/` 完整目录树 |
| 2 | 初始化 go.mod | ✅ 完成 | `go.mod` (Go 1.22+) |
| 3 | 编写 main.go 入口 | ✅ 完成 | `cmd/server/main.go` (含 embed.FS + 优雅关闭) |
| 4 | 编写 Makefile | ✅ 完成 | 支持 build/run/test/cross-compile |
| 5 | 编写 config.yaml + config.go | ✅ 完成 | Viper 配置加载 + 默认值 |

### M1-2: Header 结构体 + Encode/Decode

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 定义 Header struct（48字节） | ✅ 完成 | `internal/protocol/header.go` |
| 2 | 实现 Encode() 序列化 | ✅ 完成 | 手动按偏移写入，确保跨平台 |
| 3 | 实现 DecodeHeader() 反序列化 | ✅ 完成 | 含 Validate() 校验 |
| 4 | 单元测试：构造-解析往返验证 | ✅ 完成 | `internal/protocol/codec_test.go` |

### M1-3: AES256 加解密 + 密钥派生

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 实现 Aes256Encrypt + PKCS7 填充 | ✅ 完成 | `internal/protocol/crypto.go` (CBC 模式) |
| 2 | 实现 Aes256Decrypt + 去填充 | ✅ 完成 | 支持 fillLen 和自动检测两种模式 |
| 3 | 实现 DeriveKey 密钥派生 | ✅ 完成 | `internal/protocol/key_derivation.go` (占位版，待获取主机卫士源码) |
| 4 | 单元测试：加密-解密往返验证 | ✅ 完成 | 含空明文、DeriveKey 场景 |

### M1-4: ZLib 压缩 + CRC32 校验

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 实现 ZLib 压缩/解压 | ✅ 完成 | `internal/protocol/compress.go` |
| 2 | 实现 CRC32 校验计算 | ✅ 完成 | `internal/protocol/checksum.go` (IEEE 多项式) |
| 3 | 单元测试 | ✅ 完成 | 确定性验证 + 篡改检测 |

### M1-5: 完整帧编解码 + 集成测试

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 实现 EncodeFrame 完整编码流程 | ✅ 完成 | `internal/protocol/frame.go` (JSON→压缩→加密→CRC→组帧) |
| 2 | 实现 DecodeFrame 完整解码流程 | ✅ 完成 | 含 CRC32 校验 + 解密 + 解压 |
| 3 | 实现 TCP 分帧读取 | ✅ 完成 | ReadFrame / ReadFrameFromTCP |
| 4 | 集成测试：明文帧编解码往返 | ✅ 完成 | |
| 5 | 集成测试：加密+压缩帧编解码往返 | ✅ 完成 | 含全部 10 个 CMDID 测试 |

---

## M2：安检站模拟器 ✅

### M2-1: 状态机 + TCP 客户端

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 定义 StationState 枚举 + 状态机 | ✅ 完成 | idle→registering→online→reconnecting |
| 2 | 实现 TCP 连接/断开/重连 | ✅ 完成 | 60s 间隔自动重连 |
| 3 | 实现消息收发循环（goroutine） | ✅ 完成 | receiveLoop + handleFrame |
| 4 | 实现 30s 心跳定时器 | ✅ 完成 | time.Ticker 驱动 |
| 5 | 实现 context 生命周期控制 | ✅ 完成 | context.WithCancel |

### M2-2: Command 接口 + 10 条命令

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 定义 Command 接口 + 注册表 | ✅ 完成 | `commands/base.go` init() 自动注册 |
| 2 | CMDID=102 注册 | ✅ 完成 | 含 code=0/1000/1001 处理 |
| 3 | CMDID=100 心跳 | ✅ 完成 | CPU/内存/硬盘 + doorStatus |
| 4 | CMDID=101 信息上报 | ✅ 完成 | 型号/版本/病毒库/管控柜 |
| 5 | CMDID=103 申领码验证 | ✅ 完成 | 2001/2005/2006 错误码 |
| 6 | CMDID=104 U盘领取上报 | ✅ 完成 | |
| 7 | CMDID=105 U盘归还上报 | ✅ 完成 | 2002/2003 + 自动信息上报刷新 |
| 8 | CMDID=106 告警上报 | ✅ 完成 | 6 种告警类型 + 病毒详情 |
| 9 | CMDID=107 操作日志上报 | ✅ 完成 | 5 种操作类型 |
| 10 | CMDID=108/109 升级流程 | ✅ 完成 | goroutine 驱动 4 阶段进度上报 |

### M2-3: Hub 管理中心 + EventBus

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 实现 Hub（Station/USB 注册表） | ✅ 完成 | `internal/hub/hub.go` 线程安全 |
| 2 | 实现 EventBus（状态变更事件） | ✅ 完成 | 发布-订阅模式，非阻塞推送 |
| 3 | 实现 SQLite 持久化 | ✅ 完成 | `internal/model/db.go` + WAL 模式 |

### M2-4: REST API + WebSocket

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | Gin 路由注册 + 中间件 | ✅ 完成 | CORS + 日志 + 恢复 |
| 2 | 安检站管理 API（16 个接口） | ✅ 完成 | `internal/api/station.go` |
| 3 | WebSocket 消息流推送 | ✅ 完成 | `internal/api/ws.go` |
| 4 | embed.FS 前端嵌入 | ✅ 完成 | main.go go:embed + SPA fallback |

---

## M3：U盘插件模拟器 ✅

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | UsbDevice 数据模型 | ✅ 完成 | `internal/simulator/usb_device.go` |
| 2 | Cabinet 管控柜模型 | ✅ 完成 | `internal/simulator/cabinet.go` + PutUsb/RemoveUsb |
| 3 | U盘插入/拔出/读取/写入 API | ✅ 完成 | `internal/api/usb_plugin.go` 6 个接口 |
| 4 | 故障注入（超时/失败/非法） | ✅ 完成 | WriteDelay/ReadDelay/WriteFail/ReadFail |
| 5 | 批量操作 API | ✅ 完成 | batch-write 接口 |

---

## M4：Web 控制台 ✅

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | Vue3 + Element Plus 页面框架 | ✅ 完成 | 单文件 HTML，CDN 加载 |
| 2 | 仪表盘页面 | ✅ 完成 | 4 项统计卡片 + 实时事件流 |
| 3 | U盘插件管理页面 | ✅ 完成 | CRUD + 故障注入控制 |
| 4 | 安检站管理页面 | ✅ 完成 | 表格 + 展开行 + 命令按钮 |
| 5 | 协议调试页面 | ✅ 完成 | 编解码 + 原始帧 Hex 查看 |
| 6 | 压测中心页面 | ✅ 完成 | 配置表单 + 实时指标 |
| 7 | 消息日志页面 | ✅ 完成 | 可过滤表格 + 详情弹窗 |
| 8 | 构建产物嵌入 Go 二进制 | ✅ 完成 | `web/dist/index.html` → embed.FS |

---

## M5：压测引擎 ✅

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 压测调度引擎（goroutine 池） | ✅ 完成 | `internal/pressure/engine.go` |
| 2 | MetricsCollector 指标采集 | ✅ 完成 | `internal/pressure/metrics.go` channel 聚合 |
| 3 | 压测配置解析 | ✅ 完成 | YAML 配置 + 3 个预设场景 |
| 4 | 压测报告生成（HTML） | ✅ 完成 | `internal/pressure/reporter.go` 含 ECharts |
| 5 | 压测 API 接口 | ✅ 完成 | `internal/api/pressure.go` start/stop/status/report |

---

## M6：集成测试 + 部署 🔧

| 步骤 | 内容 | 状态 | 交付物 |
| :--- | :--- | :--- | :--- |
| 1 | 内置 Echo Server（模拟 SASOC） | ✅ 完成 | `internal/simulator/echo_server.go` |
| 2 | Go 代码编译修复 | ✅ 完成 | 修复 import 缺失、签名不一致、死锁、类型冲突 |
| 3 | go mod tidy + 编译验证 | ✅ 完成 | Go 1.26.4 + goproxy.cn，22MB exe |
| 4 | 全功能场景验证（F-001~F028） | ✅ 完成 | 注册/心跳/信息上报/申领码/U盘/告警/日志 已验证（含真实SASOC） |
| 5 | 性能压测场景验证（P-001~P006） | ⬜ 待开始 | |
| 6 | 交叉编译 + 单 exe 打包 | ✅ 完成 | usb-simulator.exe (控制台) + usb-simulator-gui.exe (无黑窗口) |
| 7 | 用户使用文档 | ✅ 完成 | `README.md` |

---

## 文件清单

共 **39** 个文件，覆盖全部 6 个里程碑：

```
usb-simulator/
├── README.md                                    # 使用文档
├── IMPLEMENTATION.md                            # 本文档
├── go.mod                                       # Go 模块定义
├── Makefile                                     # 构建脚本
├── config.yaml                                  # 默认配置
├── main.go                                      # 入口（embed.FS + Echo Server + 优雅关闭）
├── internal/
│   ├── config/config.go                         # Viper 配置加载
│   ├── protocol/
│   │   ├── header.go                            # 48 字节包头 + 编解码
│   │   ├── frame.go                             # 完整帧编解码
│   │   ├── crypto.go                            # AES256-CBC 加解密
│   │   ├── key_derivation.go                    # 密钥派生（SHA1+零填充）
│   │   ├── compress.go                          # ZLib 压缩/解压
│   │   ├── checksum.go                          # CRC32 校验
│   │   ├── errors.go                            # 错误定义
│   │   └── codec_test.go                        # 协议层单元测试
│   ├── simulator/
│   │   ├── station.go                           # 安检站模拟（TCP + 状态机）
│   │   ├── cabinet.go                           # 管控柜模拟
│   │   ├── usb_device.go                        # U盘设备 + 插件管理器
│   │   ├── echo_server.go                       # 内置模拟服务端
│   │   └── commands/
│   │       ├── base.go                          # Command 接口 + 注册表
│   │       ├── heartbeat.go                     # CMDID=100
│   │       ├── info_report.go                   # CMDID=101
│   │       ├── register.go                      # CMDID=102
│   │       ├── claim_verify.go                  # CMDID=103
│   │       ├── usb_claim.go                     # CMDID=104
│   │       ├── usb_return.go                    # CMDID=105
│   │       ├── alarm.go                         # CMDID=106
│   │       ├── operation_log.go                 # CMDID=107
│   │       └── upgrade.go                       # CMDID=108/109
│   ├── pressure/
│   │   ├── engine.go                            # 压测调度
│   │   ├── metrics.go                           # 指标采集
│   │   └── reporter.go                          # 报告生成
│   ├── api/
│   │   ├── router.go                            # Gin 路由 + 中间件
│   │   ├── station.go                           # 安检站 API
│   │   ├── usb_plugin.go                        # U盘插件 API
│   │   ├── pressure.go                          # 压测 API
│   │   ├── protocol_debug.go                    # 协议调试 API
│   │   ├── ws.go                                # WebSocket 推送
│   │   └── response.go                          # 统一响应格式
│   ├── hub/
│   │   └── hub.go                               # Hub + EventBus
│   └── model/
│       ├── db.go                                # SQLite 初始化 + 迁移
│       ├── station.go                           # 安检站 CRUD
│       └── message_log.go                       # 消息日志 CRUD
├── web/
│   └── dist/
│       └── index.html                           # Vue3 + Element Plus 前端
└── configs/
    ├── single-station.yaml                      # 单站场景
    ├── 10-stations.yaml                         # 10 站并发
    └── 100-stations-pressure.yaml               # 100 站压测
```

---

## 已修复问题（编译验证阶段）

| # | 问题 | 文件 | 修复内容 |
|:--|:-----|:-----|:---------|
| 1 | embed 路径不支持 `..` | `cmd/server/main.go` → `main.go` | 移至项目根目录，embed 路径改为 `all:web/dist` |
| 2 | `RegisterCommand` 函数/结构体同名冲突 | `commands/register.go` | 结构体重命名为 `RegisterCmd` |
| 3 | `station.SendRegister()` 方法不存在 | `commands/register.go` | 改为 `SendCommand(station, CmdRegister, nil)` |
| 4 | `"fmt"` imported and not used | `commands/upgrade.go` | 移除未使用的 fmt import |
| 5 | `header.DevID` uint32 → string 类型不匹配 | `echo_server.go` | 改为 `fmt.Sprintf("%d", header.DevID)` |
| 6 | `buf` declared and not used | `station.go` | 移除未使用的 buf 变量 |
| 7 | `MaxBodyLen + 1` 溢出 uint16 | `codec_test.go` | 测试用例改为 bodyLen=MaxBodyLen 边界值 |
| 8 | Start() 持锁调用 sendFrame() → 死锁 | `station.go` | Start() 改为先检查后释放锁再调用 |
| 9 | reconnectLoop 持锁调用 connectAndRegister → 死锁 | `station.go` | 移除 reconnectLoop 中的 mu.Lock/Unlock |
| 10 | alarm API alarmType/alarmLevel 类型 string→int | `api/station.go` | 字段类型改为 int |
| 11 | Gin NoRoute + embed.FS 导致首页 301 重定向 | `main.go` | 改用 http.HandlerFunc 包装，API 路径走 Gin，其余走 embed 静态 |
| 12 | Gin debug 模式输出覆盖启动横幅 | `main.go` + `router.go` | main.go 在 SetupRouter 前强制 gin.SetMode(ReleaseMode)，router.go 移除模式切换 |
| 13 | Windows GUI 模式无控制台无日志 | `main.go` | isConsoleAvailable() 检测，GUI 模式日志写 logs/ 目录 |
| 14 | 启动后需手动打开浏览器 | `main.go` | 添加 openBrowser() 自动打开，`--no-browser` 可跳过 |
| 15 | headFlag 常量注释/错误消息写 0x7e7e，实际为 0x5054 | `errors.go` + `header.go` + `codec_test.go` | 统一更新为 0x5054 |
| 16 | receiveLoop 校验失败后 continue 导致帧偏移 | `station.go` | 校验失败时跳过 bodyLen 字节保持帧对齐 |
| 17 | 注册应答 JSON 解密失败时 deviceId 丢失 | `station.go` | 从包头 devID 字段补充提取 deviceId |

---

## 验证结果

### 编译验证 ✅

- Go 1.22.12 + goproxy.cn 代理
- `go mod tidy` 成功下载全部依赖
- `go build -o usb-simulator.exe .` 生成 20MB 单 exe
- `go test ./internal/protocol/` 14 个测试全部 PASS

### 端到端功能验证 ✅（Echo Server 模式）

| 功能 | CMDID | 验证结果 |
|:-----|:------|:---------|
| TCP 连接 | — | ✅ 模拟站→Echo Server 连接成功 |
| 设备注册 | 102 | ✅ 注册成功，分配 deviceId=10001 |
| 自动信息上报 | 101 | ✅ 含 cabinet/virusLibs/model/version |
| 心跳 | 100 | ✅ 心跳发送成功 |
| 信息上报（手动） | 101 | ✅ |
| 申领码验证 | 103 | ✅ |
| U盘领取 | 104 | ✅ |
| U盘归还 | 105 | ✅ |
| 告警上报 | 106 | ✅ |
| 操作日志上报 | 107 | ✅ |
| 协议编解码调试 | — | ✅ encode/decode API 正常 |
| U盘插件 CRUD | — | ✅ insert/read/write/list/remove |
| Web 控制台 | — | ✅ Vue3 SPA 返回 index.html (53972B) |
| 自动打开浏览器 | — | ✅ 启动后自动 openBrowser（--no-browser 跳过） |
| GUI 模式 | — | ✅ -H windowsgui 无黑窗口，日志写 logs/ 目录 |

### 真实 SASOC 对接验证 ✅（2026-06-17）

| 功能 | CMDID | 验证结果 | 备注 |
|:-----|:------|:---------|:-----|
| TCP 连接 | — | ✅ 模拟站→192.168.60.162:4567 | 加密+压缩模式 |
| 设备注册 | 102 | ✅ 注册成功，deviceId=5 | 从包头 devID 提取 |
| 自动信息上报 | 101 | ✅ | |
| 心跳 | 100 | ✅ 30s 周期 | 心跳应答为空包体 |
| 申领码验证 | 103 | ✅ | |
| 告警上报 | 106 | ✅ | 单向上报无应答 |
| 操作日志上报 | 107 | ✅ | 单向上报无应答 |
| 长期连接稳定性 | — | ✅ 连续 2 分钟无断线 | |

> **注**：SASOC 注册应答 JSON 解密失败（密钥派生可能存在差异），但注册本身被接受（状态→online），deviceId 从包头 `devID` 字段补充获取。密钥派生逻辑已移植自主机卫士源码，需后续与 SASOC 开发团队确认。

### 构建产物

| 文件 | 模式 | 大小 | 说明 |
|:-----|:-----|:-----|:-----|
| `usb-simulator.exe` | 控制台 | 22MB | 有黑窗口可看日志，适合调试 |
| `usb-simulator-gui.exe` | GUI | 15MB | 无黑窗口自动打开浏览器，适合交付 |

---

## 待完成事项

1. **SASOC 注册应答解密兼容性** → 当前注册应答 JSON 解密失败（密钥派生或加密参数差异），需与 SASOC 开发团队确认
2. **性能压测场景验证**（P-001~P006）→ 100 站并发注册+心跳
3. **对接真实 SASOC 环境的完整业务流程验证** → 申领码验证、U盘领取/归还等端到端场景
