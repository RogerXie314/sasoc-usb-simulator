# 移动介质安检站模拟测试工具 — 使用与测试手册

---

## 1. 快速开始

### 1.1 运行方式

项目提供两种可执行文件：

| 文件 | 模式 | 说明 |
|:-----|:-----|:-----|
| `usb-simulator.exe` | 控制台 | 有黑窗口可看日志，适合调试 |
| `usb-simulator-gui.exe` | GUI | 无黑窗口，自动打开浏览器，适合日常使用 |

双击 `usb-simulator-gui.exe` 即可启动，浏览器会自动打开 Web 控制台。

启动后程序会**最小化到系统托盘**（右下角任务栏通知区域），图标为一个白色应用图标：
- **双击托盘图标**：重新打开浏览器控制台
- **右键托盘图标**：显示菜单（「打开控制台」/「退出」）
- **关闭浏览器页面不会退出程序**，需通过托盘右键「退出」或任务管理器结束进程

> **注意**：同一台电脑只能运行一个实例。如果重复双击 exe，会弹出提示「已在运行中」。

控制台模式启动：

```
usb-simulator.exe                # 启动并自动打开浏览器
usb-simulator.exe --no-browser   # 启动但不打开浏览器
usb-simulator.exe -n             # 同上（缩写）
```

### 1.2 访问地址

启动后通过以下地址访问：

- **Web 控制台**：http://localhost:8080
- **Echo Server（内置模拟 SASOC）**：localhost:4567（TCP）

### 1.3 端口冲突与重复启动

程序启动时会自动检测：
- **单实例互斥锁**：如已有实例运行，GUI 模式会弹窗提示，控制台模式打印错误信息后退出
- **端口占用检测**：如 8080 端口被其他程序占用，会弹窗/打印提示信息后退出

如 8080 或 4567 端口被占用，修改 `config.yaml`：

```yaml
server:
  port: 8080        # Web 控制台端口

sasoc:
  host: "127.0.0.1" # SASOC 地址（内置 Echo Server 时填 127.0.0.1）
  port: 4567        # SASOC TCP 端口
```

### 1.4 配置文件

`config.yaml` 必须放在 exe 同目录下，完整配置项：

```yaml
server:
  port: 8080
  debug: true              # true 时输出详细日志

sasoc:
  host: "127.0.0.1"        # SASOC 服务端地址
  port: 4567               # SASOC 服务端 TCP 端口

database:
  path: "usb-simulator.db" # SQLite 数据库文件名

simulator:
  heartbeat_interval: 30   # 心跳间隔（秒）
  reconnect_interval: 60   # 重连间隔（秒）
  read_timeout: 600        # 读超时（秒）
  offline_timeout: 100     # 离线判定超时（秒）
  max_stations: 100        # 最大模拟站数
  encrypt: false           # 是否启用 AES256 加密
  compress: false          # 是否启用 ZLib 压缩

pressure:
  collect_interval: 5      # 压测指标采集间隔（秒）
  report_format: "html"    # 压测报告格式
```

> **重要**：首次验证时建议将 `encrypt` 和 `compress` 设为 `false`（明文模式），待确认通信正常后再开启加密/压缩。

---

## 2. Web 控制台操作

### 2.1 界面概览

打开 http://localhost:8080 后，左侧菜单包含 6 个功能页：

| 菜单 | 功能 |
|:-----|:-----|
| 仪表盘 | 站点概览、在线统计、实时事件流 |
| 安检站管理 | 创建/删除/启停模拟安检站 |
| U盘插件 | 模拟U盘插入/拔出/读写 |
| 协议调试 | 手动编码/解码/发送协议帧 |
| 压力测试 | 批量创建站点并自动心跳 |
| 审计数据生成 | 批量生成审计日志（U盘审计 + 安检站策略审计） |

### 2.2 创建安检站

1. 点击左侧「安检站管理」
2. 点击「创建安检站」
3. 填写表单：
   - **SN**：设备序列号（必填，如 `SN-TEST-001`）
   - **型号**：如 `SASOC-M100`
   - **版本**：如 `v2.0.0`
   - **SASOC 地址**：默认 `127.0.0.1:4567`（指向内置 Echo Server）
4. 点击确认创建

### 2.3 启动安检站

1. 在安检站列表中，找到目标站点
2. 点击「启动」按钮
3. 站点状态从 `idle` 变为 `online`，开始自动心跳

启动后模拟站会自动执行：
- TCP 连接到 SASOC（Echo Server）
- 发送注册请求（CMDID 102）
- 接收注册响应（分配 deviceId）
- 自动信息上报（CMDID 101）
- 30 秒间隔心跳（CMDID 100）

### 2.4 模拟U盘操作

1. 点击左侧「U盘插件」
2. 点击「插入U盘」，填写 SN、型号等
3. 插入后可进行：
   - **读取**：获取U盘 SN/型号/版本/合格标记/区域名称
   - **写入**：设置合格标记（true/false）和区域名称
   - **拔出**：模拟U盘拔出

### 2.5 协议调试

1. 点击左侧「协议调试」
2. 可执行三种操作：
   - **编码**：选择 CMDID + 填写请求体 → 生成十六进制帧
   - **解码**：粘贴十六进制帧 → 解析包头 + 包体
   - **发送**：选择在线站点 + CMDID → 发送协议命令

### 2.6 压力测试

1. 点击左侧「压力测试」
2. 配置参数：
   - **站点数量**：同时创建的模拟站数（1-100）
   - **心跳间隔**：秒
   - **持续时间**：秒（0=无限）
3. 点击「启动压测」
4. 实时查看在线数、心跳成功率、吞吐量等指标

### 2.7 审计数据生成

用于向 SASOC 平台批量写入审计记录，支撑 **U盘审计（wl_usb_audit_log）与安检站策略审计（wl_usb_strategy_audit）各 100 万条容量规格**的验证。

支持两种生成模式：

#### 2.7.1 本地模式（默认，CMDID107 操作日志，审计类型 8/9/12/13）

> **原理**：工具通过 CMDID107 操作日志上报（operation=`insert/remove/scan/kill` 且必带 `sn`），
> 平台每收到一条即同时写入上述两张审计表各 1 条（U盘审计类型对应 8/9/12/13）。
> 工具无需预置或回读平台数据，SN 由工具本地生成池提供。

操作步骤：

1. 先在「安检站管理」中创建并启动至少 1 个模拟安检站，确认状态为 `online`
2. 点击左侧「审计数据生成」
3. 核对页面顶部**目标平台**提示为独立测试实例（指向生产平台时会拒绝启动）
4. 生成方式选择「本地SN池」，配置参数：
   - **目标总条数**：默认 `1000000`（对应规格 100 万）
   - **生成速率(条/秒)**：默认 `1000`，上限 `10000`，按平台处理能力调整
   - **操作类型**：插入/拔出/查毒/杀毒（对应审计类型 8/9/12/13），至少选 1 种
   - **SN 前缀 / SN 池大小**：本地生成的 U 盘 SN 池，循环使用
5. 点击「开始生成」，确认弹窗中的目标平台地址
6. 生成中可实时查看进度、速率与预计剩余时间；达到目标条数后自动停止，也可随时「停止生成」

#### 2.7.2 平台数据模式（CMDID103→104→105 申领-领取-归还，审计类型 4/5）

用于生成 **U盘申领（类型4）与 U盘回收/归还（类型5）** 两类生命周期审计。
平台侧每个周期结果：`wl_usb_audit_log` 类型4+类型5 各 1 条，`wl_usb_strategy_audit` 3 条。

> **原理**：工具直连测试平台的 openGauss 数据库，为**已收录（status=3）**的 U 盘每次插入一条
> 新的申领记录（`wl_usb_apply`，状态=已申请、时间窗覆盖当前），随后模拟安检站按
> CMDID103（申领码验证）→ CMDID104（U盘领取，写类型4）→ CMDID105（U盘归还，写类型5）
> 依次上报。平台侧 103 只校验不消费申领码，104 条件更新把申领记录置为已领取，
> 105 归还并把设备恢复为已收录状态，因此同一 SN 可循环复用换取新申领记录。

前提与步骤：

1. **在测试平台提前录入并收录至少 1 个测试 U 盘**（建议 5 个左右），记下各 SN
2. 确认能访问测试平台的 openGauss：地址、端口（默认 5432）、库名（默认 `sasoc`）、schema（默认 `soc`）、用户名、密码
3. 「审计数据生成」页生成方式选择「平台数据模式」：
   - 填写 **openGauss 连接**信息（只允许指向独立测试实例）
   - **SN 列表**：每行一个已收录 U 盘 SN；留空则自动从平台查询已收录 U 盘
   - **目标总条数**：一个完整周期（申领-领取-归还）计 1 条
   - **生成速率(条/秒)**：建议从 `50`~`100` 起步，观察平台处理后再调高
4. 点击「开始生成」：工具先连接 openGauss 校验设备，随后逐周期插入申领记录并发起 103/104/105
5. 统计栏实时显示「已领取(类型4)」与「已归还(类型5)」计数；完成后到平台「U盘审计」页面按类型 4/5 复核

> **注意**：openGauss 连接失败或无可收录设备时任务自动结束并计入错误，不会产生部分写入。
> 生产黑名单同样生效：`config.yaml` 指向生产平台（192.168.60.162）时拒绝启动。

> **安全红线**：本功能仅用于**独立测试实例**。程序内置生产平台黑名单
> （192.168.60.162），`config.yaml` 指向上述地址时启动会被拒绝。

---

## 3. API 接口参考

所有 API 基础路径：`/api/v1`

### 3.1 安检站管理

| 方法 | 路径 | 说明 |
|:-----|:-----|:-----|
| GET | `/stations` | 列出所有安检站 |
| POST | `/stations` | 创建安检站 |
| POST | `/stations/batch` | 批量创建安检站 |
| POST | `/stations/:sn/start` | 启动安检站 |
| POST | `/stations/:sn/stop` | 停止安检站 |
| POST | `/stations/:sn/restart` | 重启安检站 |
| POST | `/stations/:sn/command` | 发送协议命令 |
| DELETE | `/stations/:sn` | 删除安检站 |

**创建安检站请求体**：

```json
{
  "sn": "SN-TEST-001",
  "model": "SASOC-M100",
  "version": "v2.0.0",
  "name": "测试站点1",
  "sasocHost": "127.0.0.1",
  "sasocPort": 4567,
  "heartbeatEnabled": true,
  "heartbeatInterval": 30,
  "encryptEnabled": false,
  "compressEnabled": false
}
```

**发送命令请求体**：

```json
{
  "command": "heartbeat",
  "params": {}
}
```

支持的 command 值：`heartbeat` `info_report` `register` `claim_verify` `usb_claim` `usb_return` `alarm` `operation_log` `upgrade_issue` `upgrade_result`

### 3.2 U盘插件

| 方法 | 路径 | 说明 |
|:-----|:-----|:-----|
| GET | `/usbs` | 列出所有U盘 |
| POST | `/usbs` | 创建/插入U盘 |
| POST | `/usbs/:sn/remove` | 拔出U盘 |
| POST | `/usbs/:sn/insert` | 插入U盘到站点 |
| GET | `/usbs/:sn/data` | 读取U盘数据 |
| POST | `/usbs/:sn/write` | 写入U盘数据 |
| DELETE | `/usbs/:sn` | 删除U盘 |
| POST | `/usbs/fault-injection` | 故障注入 |

**写入U盘请求体**：

```json
{
  "qualified": true,
  "areaName": "内部区域"
}
```

**故障注入请求体**：

```json
{
  "usbId": "USB-001",
  "fault": "bad_sector",
  "duration": 30
}
```

### 3.3 压力测试

| 方法 | 路径 | 说明 |
|:-----|:-----|:-----|
| POST | `/pressure/start` | 启动压测 |
| POST | `/pressure/stop` | 停止压测 |
| GET | `/pressure/status` | 获取压测状态 |
| GET | `/pressure/report` | 获取压测报告 |

**启动压测请求体**：

```json
{
  "stationCount": 10,
  "heartbeatInterval": 30,
  "duration": 300,
  "sasocHost": "127.0.0.1",
  "sasocPort": 4567,
  "encrypt": false,
  "compress": false
}
```

### 3.4 审计数据生成

| 方法 | 路径 | 说明 |
|:-----|:-----|:-----|
| POST | `/auditgen/start` | 启动审计数据生成任务 |
| POST | `/auditgen/stop` | 停止生成任务 |
| GET | `/auditgen/stats` | 获取生成统计（轮询使用） |

**启动生成请求体（本地模式）**：

```json
{
  "mode": "local",
  "total": 1000000,
  "rate": 1000,
  "stationCount": 0,
  "opTypes": ["insert", "remove", "scan", "kill"],
  "snPrefix": "USB-",
  "snPoolSize": 1000
}
```

| 字段 | 说明 |
|:-----|:-----|
| `mode` | `local`（默认，CMDID107）/ `platform`（CMDID103/104/105） |
| `total` | 目标总条数，默认 1000000（对应 100 万规格） |
| `rate` | 生成速率（条/秒），默认 1000，上限 10000 |
| `stationCount` | 参与站点数，0=全部在线站点 |
| `opTypes` | 操作类型，映射审计类型：insert→8、remove→9、scan→12、kill→13 |
| `snPrefix` / `snPoolSize` | 本地生成 U 盘 SN 池（前缀 + 数量），循环使用 |

**启动生成请求体（平台数据模式）**：

```json
{
  "mode": "platform",
  "total": 1000000,
  "rate": 100,
  "stationCount": 0,
  "openGaussHost": "192.168.123.124",
  "openGaussPort": 5432,
  "openGaussDB": "sasoc",
  "openGaussSchema": "soc",
  "openGaussUser": "soc",
  "openGaussPassword": "******",
  "deviceSNs": ["SN000001", "SN000002"]
}
```

| 字段 | 说明 |
|:-----|:-----|
| `openGaussHost` / `openGaussPort` | 测试平台数据库地址/端口（端口默认 5432） |
| `openGaussDB` / `openGaussSchema` | 库名（默认 `sasoc`）/ schema（默认 `soc`） |
| `openGaussUser` / `openGaussPassword` | 数据库账号密码 |
| `deviceSNs` | 已收录 U 盘 SN 列表；空数组则自动查询平台已收录设备 |
| `total`（平台模式） | 完整周期数（每个周期=申领-领取-归还，平台侧类型4+5 各 +1，策略审计 +3） |

**响应/统计字段**：`total/sent/errors/rate/remainingS/elapsed/startTime/endTime/running/mode/claimSent/returnSent`。
平台模式下 `sent` = 完成周期数，`claimSent`/`returnSent` = 已发 CMDID104/105 数。

> 生成任务达到 `total` 后自动停止；运行中重复启动返回 HTTP 409。
> 指向生产平台（192.168.60.162）启动时返回 HTTP 400。
> 平台数据模式要求预置已收录 U 盘并正确配置 openGauss；连接失败或未找到设备时任务自动结束并计入错误。

### 3.5 协议调试

| 方法 | 路径 | 说明 |
|:-----|:-----|:-----|
| POST | `/debug/encode` | 编码协议帧 |
| POST | `/debug/decode` | 解码协议帧 |
| POST | `/debug/send` | 发送原始命令 |

### 3.6 消息日志

| 方法 | 路径 | 说明 |
|:-----|:-----|:-----|
| GET | `/logs?page=1&page_size=50` | 查询日志 |
| DELETE | `/logs` | 清空日志 |

---

## 4. 测试场景

### 4.1 基础功能验证

#### 场景 F-001：安检站注册

1. 创建安检站 `SN-REG-001`
2. 点击「启动」
3. 观察事件流，确认收到「注册成功」响应，deviceId 被分配

#### 场景 F-002：心跳维持

1. 启动安检站
2. 观察事件流，每 30 秒出现一次心跳
3. 在「协议调试」中发送手动心跳命令

#### 场景 F-003：信息上报

1. 启动安检站后，自动触发信息上报
2. 检查上报内容包含：型号、版本、管控柜状态、病毒库版本

#### 场景 F-004：申领码验证

1. 启动安检站
2. 在「协议调试」中发送 `claim_verify` 命令，参数 `{"claimCode": "TEST001"}`
3. 确认收到验证结果

#### 场景 F-005：U盘领取与归还

1. 启动安检站
2. 在「U盘插件」中插入U盘
3. 发送 `usb_claim` 命令领取
4. 发送 `usb_return` 命令归还

#### 场景 F-006：告警上报

1. 启动安检站
2. 发送 `alarm` 命令：

```json
{
  "command": "alarm",
  "params": {
    "alarmType": 1,
    "alarmLevel": 2,
    "alarmDetail": "发现病毒",
    "malwareName": "Test.Trojan",
    "filePath": "/usb/test.exe"
  }
}
```

#### 场景 F-007：操作日志

1. 启动安检站
2. 发送 `operation_log` 命令：

```json
{
  "command": "operation_log",
  "params": {
    "opType": "usb_insert",
    "result": "success",
    "usbSn": "USB-001"
  }
}
```

### 4.2 U盘插件测试

#### 场景 F-010：U盘读取

1. 插入U盘（SN: USB-READ-001）
2. 点击「读取」，确认返回 SN、型号、版本、合格标记、区域名称

#### 场景 F-011：U盘写入

1. 插入U盘
2. 点击「写入」，设置 qualified=false, areaName="外部区域"
3. 再次读取，确认值已变更

#### 场景 F-012：多U盘同时插入

1. 依次插入 3 个U盘
2. 确认列表显示 3 个设备
3. 批量写入不同合格标记

#### 场景 F-013：故障注入

1. 插入U盘
2. 在「协议调试」或 API 中调用故障注入
3. 读取U盘确认 qualified 变为 false

### 4.3 协议调试测试

#### 场景 F-020：编码心跳帧

1. 在「协议调试」页选择 CMDID=100（心跳）
2. 填写空 body
3. 点击「编码」，查看十六进制输出
4. 确认 48 字节包头 + 0 字节包体

#### 场景 F-021：解码协议帧

1. 将编码输出的十六进制粘贴到「解码」输入框
2. 点击「解码」
3. 确认解析出的 CMDID、deviceId 等字段与编码时一致

#### 场景 F-022：加密模式编解码

1. 修改 config.yaml 中 encrypt=true
2. 重启程序
3. 编码一个信息上报帧，确认 hex 比明文长（含填充和 AES 块对齐）
4. 解码确认能还原

### 4.4 压力测试

#### 场景 P-001：10 站并发注册

1. 进入「压力测试」页
2. 设置站点数量=10，心跳间隔=30s
3. 点击「启动」
4. 确认 10 站全部 online

#### 场景 P-002：100 站并发注册

1. 设置站点数量=100
2. 启动压测
3. 观察在线数、心跳成功率
4. 运行 2 分钟后停止

#### 场景 P-003：长时间心跳压测

1. 设置站点数量=50，持续时间=600
2. 运行 10 分钟
3. 检查心跳成功率 > 99%

### 4.5 审计数据生成

#### 场景 A-001：小批量全类型生成

1. 创建并启动 1 个模拟安检站，确认 `online`
2. 进入「审计数据生成」，配置：目标条数=100、速率=20、操作类型全选
3. 点击「开始生成」，等待自动停止
4. 确认统计：已发送=100、错误=0、状态=已结束
5. 到平台侧查询 `wl_usb_audit_log`：新增 100 条，operation_type 覆盖 8/9/12/13；
   查询 `wl_usb_strategy_audit`：同样新增 100 条「操作日志上报」记录

#### 场景 A-002：100 万条容量规格验证

1. 使用独立测试实例，配置：目标条数=1000000、速率=1000（约 17 分钟）或按需调大
2. 生成完成后，到平台侧确认：
   - `wl_usb_audit_log` 总量达到 100 万（触发留存上限清理的验证数据）
   - `wl_usb_strategy_audit` 同步增长约 100 万
   - 抽查 detail 长度均 < 4096B，SN 在配置的 SN 池范围内
3. 若需观察平台清理逻辑，可再追加生成，确认仅在 operation_type ∈ (8,9,12,13) 上清理

#### 场景 A-003：生产保护拦截

1. 将 config.yaml 的 sasoc.host 改为 `192.168.60.162`
2. 重启程序，进入「审计数据生成」点击开始
3. 确认被拒绝（HTTP 400），提示目标为生产平台

#### 场景 A-004：平台数据模式生成类型4/5

1. 在测试平台提前录入并收录测试 U 盘（建议 5 个），记下 SN
2. 配置「审计数据生成」→ 平台数据模式，填写 openGauss 连接与 SN 列表
3. 配置目标条数=100、速率=50，点击开始，等待自动停止
4. 确认统计：已发送=100、已领取(类型4)=100、已归还(类型5)=100、错误=0
5. 到平台侧查询 `wl_usb_audit_log`：新增 200 条，operation_type 覆盖 4（申领）与 5（回收/归还）；
   查询 `wl_usb_strategy_audit`：新增约 300 条（验证/领取/归还各 1）

#### 场景 A-005：100 万条规格验证（平台模式，可选）

1. 使用独立测试实例：配置平台数据模式、目标条数=1000000（平台侧类型4+5 各 100 万 + 策略审计 300 万）
2. 速率建议从 100~200 起步，观察平台处理与 openGauss 入库无积压后逐步调高
3. 生成完成后到平台侧确认 `wl_usb_audit_log` 中类型 4/5 各自达到目标量级

---

## 5. 日志与排查

### 5.1 控制台模式日志

直接在黑窗口查看输出。

### 5.2 GUI 模式日志

日志文件位于 exe 同目录下 `logs/usb-simulator-YYYY-MM-DD.log`，JSON 格式。

### 5.3 常见问题

| 问题 | 可能原因 | 解决方法 |
|:-----|:---------|:---------|
| 页面 404 | 前端 API 路径不匹配 | 确保使用最新版 exe（已修复 API 对齐） |
| 双击 exe 没反应 | 已有实例在运行 | 检查系统托盘图标，或在任务管理器中结束进程 |
| 弹窗提示端口冲突 | 8080 被其他程序占用 | 修改 config.yaml 中的 server.port |
| 站点无法启动 | SASOC 端口被占用 | 修改 config.yaml 中的 sasoc.port |
| 心跳不回复 | Echo Server 未启动 | 检查 4567 端口是否被防火墙阻断 |
| 加密模式通信失败 | 密钥派生未实现 | key_derivation.go 为占位实现，暂用 encrypt=false |
| 浏览器未自动打开 | rundll32 异常 | 手动访问 http://localhost:8080 |
| 关闭浏览器后找不到程序 | GUI 模式在系统托盘 | 右下角通知区域找托盘图标，右键退出 |
| 数据库锁定 | 多实例运行 | 确保只运行一个实例（程序已自动互斥） |

### 5.4 数据存储

- **SQLite 数据库**：`usb-simulator.db`（exe 同目录）
- 存储内容：安检站配置、消息日志
- 删除此文件可重置所有数据

---

## 6. 对接真实 SASOC

### 6.1 修改连接地址

将 config.yaml 中的 SASOC 配置指向真实服务：

```yaml
sasoc:
  host: "192.168.1.100"  # 真实 SASOC IP
  port: 4567
```

### 6.2 开启加密/压缩

真实环境通常要求加密和压缩：

```yaml
simulator:
  encrypt: true
  compress: true
```

> **注意**：加密模式需要正确的密钥派生实现。当前 `key_derivation.go` 为占位版本，需要根据主机卫士 Java 源码的 `initRootKey` 方法替换真实实现后才能与真实 SASOC 通信。

### 6.3 关闭内置 Echo Server

当连接真实 SASOC 时，内置 Echo Server 会因端口冲突无法启动（这是正常的），日志中会出现：

```
echo server start failed (port may be in use)
```

不影响正常使用，忽略即可。如需彻底关闭，可将 `sasoc.port` 设为 0。

---

## 7. 从源码构建

### 7.1 前置条件

- Go 1.22+（https://go.dev/dl/）
- 配置国内代理：`go env -w GOPROXY=https://goproxy.cn,direct`

### 7.2 构建

```bash
# 控制台模式
go build -o usb-simulator.exe .

# GUI 模式（无黑窗口）
GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H windowsgui" -o usb-simulator-gui.exe .

# Linux
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o usb-simulator .
```

### 7.3 运行测试

```bash
go test -v -race ./...
```
