## V3.0.0 Release Notes

**Full Changelog: V2.9.7 → V3.0.0**

---

### V2.9.8 变更

**申领码自定义参数、生命周期引擎及前端工作流面板**

- `internal/claimgen/lifecycle.go` — 新增申领码生命周期引擎
  - 支持按状态比例（已申领/已领取/已归还/已超时）批量处理申领码
  - CMD103 申领码验证、CMD104 U盘领取、CMD105 归还 协议实现
  - 并发控制、状态轮询、任务取消

- `internal/claimgen/claimgen.go` — 重构申领码生成逻辑
  - 支持自定义申领参数（申领人、工号、手机号、区域ID、容量、格式、时长）
  - 新增 `buildClaimBody()` 构建申领请求体
  - 登录模块分离（`login.go`）支持 CookieJar + Token 认证

- `internal/api/claim.go` / `router.go` — 申领 API 端点调整
  - 新增生命周期任务端点（`/claim/lifecycle/start|status|cancel`）
  - 申领码导出（`/claim/export`）

- `web/dist/index.html` — 批量申领码页面重构
  - 新增 SASOC 平台登录面板（用户名密码登录，获取 Token）
  - 生命周期配置面板（比例滑块、并发数、终端SN选择）
  - 申领码生成实时统计（进度、成功/失败数、速率、耗时）
  - 支持导出生成的申领码列表

---

### V2.9.9 变更

**新增安全U盘告警协议、操作日志协议对齐及申领参数校验**

**告警模块 (`internal/simulator/commands/alarm.go`)**
- 新增 10 种安全U盘告警类型（`SAFE_UDISK_*`），对齐《安检站安全U盘告警协议》
  - `SAFE_UDISK_DOOR_FAULT`（柜门故障）
  - `SAFE_UDISK_NO_AVAILABLE_RETURN_SLOT`（无可用归还柜位）
  - `SAFE_UDISK_ILLEGAL_DEVICE`（非法设备插入）
  - `SAFE_UDISK_UNEXPECTED_REMOVAL`（U盘异常拔出）
  - `SAFE_UDISK_CABINET_FATAL_FAULT`（整机故障）
  - `SAFE_UDISK_FORMAT_FAILED`（U盘格式化失败）
  - `SAFE_UDISK_METADATA_WRITE_FAILED`（业务信息操作失败）
  - `SAFE_UDISK_INIT_FAILED`（U盘初始化失败）
  - `SAFE_UDISK_DOOR_CLOSE_TIMEOUT`（柜门关闭超时）
  - `SAFE_UDISK_USAGE_VIOLATION`（U盘使用违规）
- 新增 30 种告警原因枚举（reason 0~29）及中文名映射表 `ReasonNames`
- 新增 `AlarmTypeReasons` 映射表，约束每种告警类型只能携带合法 reason 子集
- `BuildBody()` 按协议携带规则生成 `detail{doorNo, reason}`：
  - reason 5/6 → 清空 sn + doorNo
  - reason 29 → 清空 doorNo
- 新增 `AllSafeUdiskAlarmSamples()` 支持一键模拟全部告警
- 新增 `alarm-all` API 端点：`POST /station/:id/alarm-all`

**操作日志模块 (`internal/simulator/commands/operation_log.go`)**
- 新增 `quarantineExport`（隔离区导出）操作类型
- sn 改为条件字段：`copy` / `quarantineExport` 无 SN 介质操作不要求 sn
- `insert` / `remove` 结果固定为 `success`
- 新增 `buildOpLogMessage()` 按协议格式生成各操作类型的 message（scan 统计信息、kill 病毒信息等）

**申领模块 (`claim.go` + `claimgen.go`)**
- 新增 `validateClaimParams()`：校验 factoryIds（数字 ID）、phone（11 位或 1~3 位前缀）、workerNo（字母数字）
- `buildPhone()` 支持完整 11 位号码（保留前 3 位 + 序号）和前缀模式两种

**前端 (`web/dist/index.html`)**
- StationPage / PressurePage 新增安全U盘告警表单（reason 下拉选择器、doorNo、sn）
- 新增告警原因映射表 `alarmReasonMap`（30 条）
- 一键全部告警入口及调用逻辑
- 操作日志 sn 按操作类型条件显示
- 申领表单 placeholder 优化

**版本号更新**
- `main.go` / `internal/api/response.go` / `web/dist/index.html` → V2.9.9

---

### V2.9.9 之后的热修复

**fix: 申领任务未使用登录的平台地址导致全部失败**

- **根因**：`startClaim()` 未设置 `PlatformURL`，`claimgen.go` 回退到默认值 `https://192.168.123.24:8440`，导致前端登录了 `.124` 但申领发向 `.24`
- **修复**：
  - `claim.go`：从登录凭证 `cred.PlatformURL` 获取平台地址传给 `StartTask()`
  - `index.html`：`startClaim` 请求增加 `platformUrl` 字段

---

### V3.0.0 变更

**申领地址修复、压力测试统计保持、登录态持久化**

**申领地址修复**
- `claim.go`：`StartTask` 从登录凭证获取 `PlatformURL`，避免申领请求发向默认 `.24`
- `index.html`：`startClaim` API 调用增加 `platformUrl` 参数

**压力测试统计保持显示**
- `internal/api/pressure.go`：
  - 压测管理器增加 `endTime` 字段，任务结束时自动记录
  - `getStatsInternal()` 任务结束后返回完整最终统计数据（`alarmsSent`、`logsSent`、`errors`、`stationCount`、`startTime`、`elapsed`），不再返回空数据

**申领登录态持久化**
- `index.html`：`platformUrl` / `token` / `sessionId` 写入 `localStorage`
- 页面刷新或切换后自动恢复登录状态，无需重复登录
- `claim.go`：新增 `GET /api/v1/claim/login-status` API 查询登录状态
- `router.go`：注册 `/claim/login-status` 路由

**GUI 终端窗口**
- 重新编译去掉交叉编译标志（`GOOS=windows GOARCH=amd64`），确保 `-H windowsgui` 正确生效，GUI 模式不再显示终端窗口

**版本号更新**
- `main.go` / `internal/api/response.go` / `web/dist/index.html` → V3.0.0

---

### 变更文件汇总（V2.9.7 → V3.0.0）

| 文件 | 变更 |
|------|------|
| `cmd/insert_panel/main.go` | 新增生命周期面板插入脚本 |
| `internal/api/claim.go` | 申领 API 重构 + 生命周期端点 + 登录态持久化 + 参数校验 + 地址修复 |
| `internal/api/pressure.go` | quarantineExport 操作类型 + 压测统计保持 |
| `internal/api/response.go` | 版本号更新 |
| `internal/api/router.go` | 生命周期 / login-status / alarm-all 路由 |
| `internal/api/station.go` | alarm-all 端点 |
| `internal/claimgen/claimgen.go` | 申领重构 + buildPhone + 校验 |
| `internal/claimgen/lifecycle.go` | 新增申领码生命周期引擎 |
| `internal/simulator/commands/alarm.go` | 10 种安全U盘告警 + 30 reason |
| `internal/simulator/commands/operation_log.go` | quarantineExport + 协议对齐 |
| `main.go` | 版本号更新 |
| `web/dist/index.html` | 全流程前端重构（告警/日志/申领/生命周期） |

---

### 构建产物

- `usb-simulator.exe` — 控制台模式（带日志输出）
- `usb-simulator-gui.exe` — GUI 模式（无终端窗口，日志写文件）

### 使用注意

1. 关闭旧进程，启动新 `usb-simulator-gui.exe`
2. 浏览器 `Ctrl + F5` 强制刷新
3. 申领前确认登录状态为"已登录"，且平台地址正确

---

**Commits**: `c0eba9a` → `2fb7a67`
**发布日期**: 2026-09-01
