# orzdba 全项目单测覆盖差距报告

> 审计日期：2026-07-07
> 审计范围：全部 37 个 Go 源文件（22 个源码 + 15 个单测）
> 审计结论：84 项测试通过，7 个包有覆盖率报告，部分包存在**严重缺失**。

---

## 统计总览

| 包 | 覆盖率 | 源码函数/方法数 | 有单测覆盖的函数数 | 缺失覆盖的函数数 |
|---|---|---|---|---|
| `internal/metric` | — | 0（纯常量/类型定义） | 0 | 0 |
| `internal/syscol` | 68% | ~25 个 | ~17 | **~8** |
| `internal/mycol` | 47% | ~45 个 | ~21 | **~24** |
| `internal/mysqlc` | 70% | ~15 个 | ~10 | **~5** |
| `internal/render` | 92% | ~12 个 | ~11 | ~1 |
| `internal/logsink` | 73% | ~8 个 | ~6 | **~2** |
| `internal/rtcol` | 17% | ~12 个 | ~4 | **~8** |
| `cmd/orzdba` | 29% | ~15 个 | ~4 | **~11** |

---

## 一、`internal/syscol`（/proc 系统采集器）— 覆盖率 68%

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
| `CountCPU()` | `collector.go` | ✅ |
| `CountCPUEmpty()` (error path) | `collector.go` | ✅ |
| `parseCPUStat()` | `cpu.go` | ✅ |
| `CPU.Sample()` | `cpu.go:51` | ✅（间接通过 TestCPUSecondTick） |
| `CPU.consume()` | `cpu.go:61` | ✅ |
| `CPU.Collect()` | `cpu.go:95` | ✅ |
| `cpuUsrColor()` | `cpu.go:109` | ✅ |
| `cpuSysColor()` | `cpu.go:117` | ✅ |
| `cpuIowColor()` | `cpu.go:125` | ✅ |
| `Load.Collect()` | `load.go:29` | ✅（通过 golden test） |
| `loadColor()` | `load.go:50` | ✅ |
| `zeroLoad()` | `load.go:57` | ✅ |
| `parseVMStatSwap()` | `swap.go:81` | ✅ |
| `Swap.Collect()` (first-tick) | `swap.go:35` | ✅ |
| `Swap.Collect()` (second-tick) | `swap.go:35` | ✅ |
| `swapColor()` | `swap.go:72` | ✅ |
| `parseNetDev()` | `net.go:85` | ✅ |
| `netColor()` | `net.go:74` | ✅ |
| `Net.Collect()` | `net.go:42` | ✅ |
| `parseDiskStat()` | `disk.go:159` | ✅ |
| `Disk.deltams()` | `disk.go:111` | ✅ |
| `diskBytesColor()` | `disk.go:118` | ✅ |
- `diskWaitColor()` | `disk.go:124` | ✅ |
- `diskSvcColor()` | `disk.go:130` | ✅ |
- `diskBusyColor()` | `disk.go:136` | ✅ |
- `zeroDisk()` | `disk.go:143` | ✅ |

### 缺失覆盖

| 函数 | 文件 | 缺失内容 |
|---|---|---|
- `Load.Headline()` | `load.go:23` | 纯返回值，未测 |
- `Load.Name()` | `load.go:20` | 纯返回值，未测 |
- `Swap.Headline()` | `swap.go:30` | 纯返回值，未测 |
- `Swap.Name()` | `swap.go:28` | 纯返回值，未测 |
- `Net.Headline()` | `net.go:37` | 纯返回值，未测 |
- `Net.Name()` | `net.go:35` | 纯返回值，未测 |
- `Disk.Headline()` | `disk.go:43` | 纯返回值，未测 |
- `Disk.Name()` | `disk.go:41` | 纯返回值，未测 |

> **风险评估**：全部为纯 `return` 语句，无分支逻辑。缺失不影响运行正确性，但**不符合项目"每个 collector 必须测试"的约定**。

---

## 二、`internal/mycol`（MySQL 采集器）— 覆盖率 47% ⚠️

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
- `StatusSource.Delta()` | `collector.go:125` | ✅ |
- `StatusSource.Rate()` | `collector.go:133` | ✅ |
- `StatusSource.Cur()` | `collector.go:117` | ✅ |
- `StatusSource.HasPrev()` | `collector.go:114` | ✅ |
- `StatusSource.CurRaw()` | `collector.go:139` | ✅ |
- `Com.Collect()` (有 prev) | `com.go:25` | ✅ |
- `Com.Collect()` (first-tick) | `com.go:25` | ✅ |
- `Hit.collectOne()` (正常) | `hit.go:40` | ✅ |
- `Hit.collectOne()` (低 hit) | `hit.go:40` | ✅ |
- `Hit.collectOne()` (零读取) | `hit.go:40` | ✅ |
- `Hit.collectFull()` (7 列) | `hit.go:54` | ✅ |
- `threadCacheHit()` (有连接) | `threads.go:40` | ✅ |
- `threadCacheHit()` (无连接) | `threads.go:40` | ✅ |
- `Threads.Collect()` (有 prev) | `threads.go:24` | ✅ |
- `Threads.Collect()` (first-tick) | `threads.go:24` | ✅ |
- `InnodbRows.Collect()` | `innodb_rows.go:17` | ✅ |
- `InnodbPages.Collect()` | `innodb_pages.go:18` | ✅ |
- `InnodbData.Collect()` | `innodb_data.go:20` | ✅ |
- `InnodbLog.Collect()` | `innodb_log.go:20` | ✅ |
- `parseInnodbStatus()` | `innodb_status.go:66` | ✅ |
- `parseInnodbStatus` (缺失字段) | `innodb_status.go:66` | ✅ |
- `formatSlave()` (正常) | `slave.go:33` | ✅ |
- `formatSlave()` (lag 红色) | `slave.go:33` | ✅ |
- `formatSlave()` (NULL lag) | `slave.go:33` | ✅ |
- `Semi.Collect()` (ON) | `semi.go:22` | ✅ |
- `Semi.Collect()` (OFF) | `semi.go:22` | ✅ |
- `Semi.Collect()` (未加载) | `semi.go:22` | ✅ |
- `pct()` (各分支) | `hit.go:94` | ✅ |
- `hitColor()` | `hit.go:107` | ✅ |

### 缺失覆盖

#### 2.1 StatusSource 关键方法 — 全部未覆盖 ⚠️

| 函数 | 文件 | 说明 |
|---|---|---|
- `StatusSource.Fetch()` | `collector.go:81` | **核心方法**——执行 SQL 并更新 cur/prev 映射，无单测覆盖 |
- `StatusSource.SlaveStatus()` | `collector.go:150` | 执行 `SHOW SLAVE STATUS` 解析，无单测覆盖 |
- `StatusSource.Databases()` | `collector.go:189` | 执行 `SHOW DATABASES` 过滤系统库，无单测覆盖 |
- `StatusSource.ShowVariables()` | `collector.go:217` | 执行 `SHOW VARIABLES IN (...)` 获取变量值，无单测覆盖 |
- `StatusSource.InnodbStatus()` | `collector.go:28` (mycol) | 执行 `SHOW ENGINE INNODB STATUS` 解析，无单测覆盖 |
- `StatusSource.NewStatusSource()` | `collector.go:73` | 构造函数，未单独验证字段初始化 |
- `isSystemDB()` | `collector.go:242` | 系统数据库过滤判断，未测 |
- `inList()` | `collector.go:251` | SQL IN 子句构建，未测 |
- `parseInt64()` | `collector.go:261` | 数值解析降级，未测 |

#### 2.2 各 Collector 的 First-Tick 分支 — 仅部分覆盖

| 函数 | 文件 | 缺失场景 |
|---|---|---|
| `InnodbData.Collect()` | `innodb_data.go:20` | **first-tick 零值分支**未测 |
| `InnodbLog.Collect()` | `innodb_log.go:20` | **first-tick 零值分支**未测 |
- `InnodbRows.Collect()` | `innodb_rows.go:17` | **HasPrev=false 分支**有覆盖 ✅ |
- `InnodbPages.Collect()` | `innodb_pages.go:18` | **HasPrev=false 分支**有覆盖 ✅ |
- `Bytes.Collect()` | `bytes.go:25` | **HasPrev=false 分支**有覆盖 ✅ |
- `Threads.Collect()` | `threads.go:24` | **HasPrev=false 分支**有覆盖 ✅ |
- `InnodbStatus.Collect()` | `innodb_status.go:102` | **HasPrev=false 分支**、**InnodbStatus() 返回 OK=false 的降级分支**均未测 |

#### 2.3 纯返回值缺失

| 函数 | 文件 |
|---|---|
| `Com.Name()` | `com.go:19` |
| `Com.Headline()` | `com.go:21` |
- `Hit.Name()` | `hit.go:22` |
- `Hit.Headline()` (full) | `hit.go:24` |
- `Hit.Headline()` (one) | `hit.go:24` |
- `InnodbRows.Name()` | `innodb_rows.go:13` |
- `InnodbRows.Headline()` | `innodb_rows.go:14` |
- `InnodbPages.Name()` | `innodb_pages.go:13` |
- `InnodbPages.Headline()` | `innodb_pages.go:15` |
- `InnodbData.Name()` | `innodb_data.go:16` |
- `InnodbData.Headline()` | `innodb_data.go:17` |
- `InnodbLog.Name()` | `innodb_log.go:16` |
- `InnodbLog.Headline()` | `innodb_log.go:17` |
- `InnodbStatus.Name()` | `innodb_status.go:97` |
- `InnodbStatus.Headline()` | `innodb_status.go:98` |
- `Threads.Name()` | `threads.go:19` |
- `Threads.Headline()` | `threads.go:20` |
- `Bytes.Name()` | `bytes.go:19` |
- `Bytes.Headline()` | `bytes.go:21` |
- `Slave.Name()` | `slave.go:16` |
- `Slave.Headline()` | `slave.go:17` |
- `Slave.zeroSlave()` | `slave.go:48` |
- `Semi.Name()` | `semi.go:17` |
- `Semi.Headline()` | `semi.go:18` |
- `zeroOverZero()` (测试辅助) | `collector_test.go:410` |

> **风险评估**：**StatusSource 的 Fetch/Databases/ShowVariables/InnodbStatus/SlaveStatus 是全项目的核心**——所有 MySQL 采集器都依赖 StatusSource 提供数据。这些方法在单测中完全未覆盖，意味着如果这些方法有 bug，**47% 的 mycol 覆盖率也测不出来**。

---

## 三、`internal/mysqlc`（MySQL 连接与凭证）— 覆盖率 70%

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
- `Config.DSN()` (TCP) | `conn.go:154` | ✅ |
- `Config.DSN()` (Unix socket) | `conn.go:154` | ✅ |
- `Config.DSN()` (socket 优先于 host) | `conn.go:154` | ✅ |
- `Config.SafeDSN()` | `conn.go:168` | ✅ |
- `ResolveCredentials` (CLI 覆盖 CNF) | `conn.go:56` | ✅ |
- `ResolveCredentials` (env 覆盖) | `conn.go:56` | ✅ |
- `ResolveCredentials` (编译期注入) | `conn.go:56` | ✅ |
- `ResolveCredentials` (默认值) | `conn.go:56` | ✅ |
- `ParseMySQLDefaults` (client 段) | `defaults.go:parseMySQLDefaults` | ✅ |
- `ParseMySQLDefaults` (mysql 段) | `defaults.go:parseMySQLDefaults` | ✅ |
- `ParseMySQLDefaults` (缺失段) | `defaults.go:parseMySQLDefaults` | ✅ |
- `ParseMySQLDefaults` (布尔键/注释跳过) | `defaults.go:parseMySQLDefaults` | ✅ |
- `CheckFileMode()` | `defaults.go:checkFileMode` | ✅ |
- `expandHome()` | `conn.go:195` | 间接通过 DefaultCNFSearch 覆盖 |

### 缺失覆盖

| 函数 | 文件 | 缺失内容 |
|---|---|---|
- `Open()` | `conn.go:177` | **建立连接的方法**——需要真实 MySQL 服务才能测试，但可以用 mock 驱动 |
- `Config.DSN()` TLS 分支 | `conn.go:156` | 未测 `TLS=true` 时 `?tls=true` 是否正确拼接 |
- `DefaultCNFSearch` 全路径探测 | `conn.go:51` | 仅测了指定文件，未测"遍历默认路径"场景 |
- `ParseMySQLDefaults` `!include`/`!includedir` 边界 | `defaults.go` | 文档提到支持，但测试未覆盖 |
- `ParseMySQLDefaults` 引号值处理 | `defaults.go` | 仅测了一个引号值案例 |
- `loadCNF()` 默认搜索路径 | `conn.go:121` | 未测无显式 defaults-file 时的默认搜索逻辑 |

---

## 四、`internal/render`（渲染层）— 覆盖率 92%

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
- `NewANSI()` (color=true) | `ansi.go:36` | ✅ |
- `NewANSI()` (color=false) | `ansi.go:36` | ✅ |
- `ANSI.Escape()` | `ansi.go:39` | ✅ |
- `ANSI.Reset()` | `ansi.go:47` | ✅ |
- `ANSI.Colorize()` | `ansi.go:56` | ✅ |
- `NewRenderer()` | `render.go:44` | ✅ |
- `(*Renderer) Header()` | `render.go:60` | ✅ |
- `(*Renderer) BuildRow()` | `render.go:90` | ✅ |
- `(*Renderer) sep()` | `render.go:120` | ✅ (内部) |
- `FormatBytesRate()` (各分支) | `format.go:20` | ✅ |
- `FormatBytesKM()` (各分支) | `format.go:25` | ✅ |
- `FormatBytesAutoG()` (各分支) | `format.go:42` | ✅ |
- `RoundInt()` | `format.go:54` | ✅ |

### 缺失覆盖

| 函数 | 文件 | 缺失内容 |
|---|---|---|
- `(*Renderer) ResetHeaderCounter()` | `render.go:135` | 仅测试了 Header/BuildRow，**未验证 ResetHeaderCounter 将 since 归零**的效果 |
- `(*Renderer) ANSI()` | `render.go:131` | 仅一个 `return r.ansi` 行，理论上可忽略 |

---

## 五、`internal/logsink`（输出层）— 覆盖率 73%

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
- `Stdout.Write()` | `stdout.go:8` | ✅ |
- `Stdout.Close()` | `stdout.go:9` | 部分覆盖 |
- `New()` (stdout) | `sink.go:35` | ✅ |
- `New()` (file) | `sink.go:35` | ✅ |
- `New()` (daily file) | `sink.go:35` | ✅ |
- `openFile()` (成功) | `sink.go:46` | ✅ |
- `openFile()` (失败) | `sink.go:46` | ✅ (TestFileNotWritableErrors) |
- `newFile()` (成功) | `file.go:15` | ✅ |
- `(*File).Write()` | `file.go:23` | ✅ |
- `(*File).Close()` | `file.go:24` | ✅ |
- `newDailyFile()` (成功) | `dailyfile.go:18` | ✅ |
- `(*DailyFile).Write()` | `dailyfile.go:28` | ✅ |
- `(*DailyFile).Close()` | `dailyfile.go:29` | ✅ |
- `(*DailyFile).MaybeRotate()` (同一天) | `dailyfile.go:34` | ✅ |
- `(*DailyFile).MaybeRotate()` (跨天) | `dailyfile.go:34` | ✅ |

### 缺失覆盖

| 函数 | 文件 | 缺失内容 |
|---|---|---|
- `(*File).reopen()` | `file.go:27` | **唯一未测的方法**——DailyFile 通过它切换文件，但单独调用未覆盖 |
- `(*Stdout) Write()` 异常场景 | `stdout.go:8` | 未测 stdout 被重定向或关闭时的行为 |
- `(*Stdout) Close()` 返回值验证 | `stdout.go:9` | 只调用了 Write，Close 返回 nil 未单独验证 |

---

## 六、`internal/rtcol`（tcprstat 采集器）— 覆盖率 17% ⚠️ 最低

这是本项目 **最薄弱的包**。所有功能涉及子进程生命周期管理，在 macOS 上 tcprstat 二进制不存在，所有测试被跳过。

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
- `parseRTLine()` (正常) | `tcprstat.go:169` | ✅ |
- `parseRTLine()` (字段不足) | `tcperstat.go:169` | ✅ |
- `parseRTLine()` (空) | `tcperstat.go:169` | ✅ |
- `rtColor()` (阈值) | `tcperstat.go:182` | ✅ |

### 缺失覆盖

| 函数 | 文件 | 缺失场景 |
|---|---|---|
- `(*Collector).Name()` | `tcperstat.go:47` | 纯返回值，未测 |
- `(*Collector).Headline()` | `tcperstat.go:49` | 纯返回值，未测 |
- `(*Collector).Start()` (二进制不存在) | `tcperstat.go:56` | **启动前提检查，完全未测** |
- `(*Collector).Start()` (锁定失败) | `tcperstat.go:61` | 另一个实例已持有锁，**完全未测** |
- `(*Collector).Start()` (日志文件创建失败) | `tcperstat.go:67` | 文件权限错误等，**完全未测** |
- `(*Collector).Collect()` (进程未启动) | `tcperstat.go:89` | `c.started=false` 时返回零值，**完全未测** |
- `(*Collector).Collect()` (信号探测失败→重启一次) | `tcperstat.go:93` | 子进程崩溃后重启一次，**完全未测** |
- `(*Collector).Collect()` (重启失败→降级零值) | `tcperstat.go:93` | 重启后仍然崩溃，**完全未测** |
- `(*Collector).lastSample()` (日志文件读取失败) | `tcperstat.go:127` | **完全未测** |
- `(*Collector).lastSample()` (文件内容为空) | `tcperstat.go:132` | **完全未测** |
- `(*Collector).lastSample()` (最后一行解析失败) | `tcperstat.go:138` | **完全未测** |
- `(*Collector).Stop()` (正常退出) | `tcperstat.go:147` | SIGTERM 后等待退出，**完全未测** |
- `(*Collector).Stop()` (超时→SIGKILL) | `tcperstat.go:153` | 200ms 超时后 SIGKILL，**完全未测** |
- `(*Collector).Stop()` (清理文件) | `tcperstat.go:158` | 删除 log/lck 文件，**完全未测** |
- `(*Collector).restart()` (成功) | `tcperstat.go:114` | **完全未测** |
- `(*Collector).restart()` (失败) | `tcperstat.go:114` | 重启失败，**完全未测** |

> **风险评估**：`rtcol` 包负责生产环境中 tcprstat 子进程的完整生命周期管理。虽然 macOS CI 会跳过这些测试，但**在 Linux 生产环境中这些路径一旦出错，影响是直接的业务故障**（tcprstat 崩溃后不再响应、残留文件不清理）。建议在 Linux 测试环境中补充集成测试。

---

## 七、`cmd/orzdba`（CLI 入口）— 覆盖率 29%

### 已有单测

| 函数 | 文件 | 覆盖情况 |
|---|---|---|
- `normalizeArgs()` (单杠长选项) | `args.go:199` | ✅ |
- `normalizeArgs()` (短选项不变) | `args.go:199` | ✅ |
- `normalizeArgs()` (值不变) | `args.go:199` | ✅ |
- `normalizeArgs()` (-hit full 合并) | `args.go:199` | ✅ |
- `normalizeArgs()` (bare -hit) | `args.go:199` | ✅ |
- `config.expand()` (-sys) | `args.go:142` | ✅ |
- `config.expand()` (-mysql) | `args.go:142` | ✅ |
- `config.expand()` (-mysql 不覆盖 -hit full) | `args.go:142` | ✅ |
- `config.expand()` (leaf 不触发 composite) | `args.go:142` | ✅ |
- `friendlyParseErr()` (缺参数) | `args.go:239` | ✅ |
- `friendlyParseErr()` (未知标志) | `args.go:239` | ✅ |
- `friendlyParseErr()` (透传) | `args.go:239` | ✅ |
- `validateDevFlag()` (错误) | `args.go:370` | ✅ |
- `parseArgs()` (-hit full) | `args.go:65` | ✅ |
- `parseArgs()` (-mysql -hit full) | `args.go:65` | ✅ |
- `parseArgs()` (-sys 展开) | `args.go:65` | ✅ |
- `parseArgs()` (无参数) | `args.go:65` | ✅ |
- `parseArgs()` (-d 缺值) | `args.go:65` | ✅ |

### 缺失覆盖

| 函数/逻辑 | 文件 | 缺失场景 |
|---|---|---|
- `parseArgs()` (-innodb 展开) | `args.go:180` | 未测 |
- `parseArgs()` (-slave/-semi 展开) | `args.go:152-155` | 未测 |
- `parseArgs()` (--tps-mode) | `args.go:116` | 未测 |
- `parseArgs()` (-rt 组合) | `args.go:162` | 未测 |
- `parseArgs()` (所有组合参数交叉) | `args.go:142` | 未测 |
- `main()` (启动流程) | `main.go:37` | **完全无法在单测中运行**（需要 /proc + MySQL + tcprstat） |
- `runLoop()` (day-rollover) | `main.go:218` | 按天轮转检测，**完全未测** |
- `runLoop()` (count 退出) | `main.go:230` | -C 到达退出，**完全未测** |
- `runLoop()` (header 重打) | `main.go:233` | headerPeriod 重打表头，**完全未测** |
- `mysqlTitleLine()` | `main.go:251` | 需要真实 StatusSource，**完全未测** |
- `mysqlVarsLines()` | `main.go:294` | 需要真实 StatusSource，**完全未测** |
- `buildTitle()` | `main.go:460` | 需 os.Hostname + 网络接口，**完全未测** |
- `primaryIP()` (有非 loopback) | `main.go:495` | **完全未测** |
- `primaryIP()` (无非 loopback) | `main.go:495` | 全 loopback 返回 "?"，**完全未测** |
- `detectCPU()` (/proc 存在) | `main.go:354` | **完全未测** |
- `detectCPU()` (/proc 不存在) | `main.go:354` | fallback 到 runtime，**完全未测** |
- `checkDiskDevice()` (设备存在) | `main.go:383` | **完全未测** |
- `checkDiskDevice()` (/proc 不存在) | `main.go:383` | 非 Linux 降级，**完全未测** |
- `checkDiskDevice()` (设备不存在) | `main.go:383` | 报错退出，**完全未测** |
- `usage()` (help 输出) | `main.go:399` | 纯字符串输出，**完全未测** |

> **风险评估**：`main()` 和 `runLoop()` 是**整个程序的入口和主循环**，目前没有任何单测覆盖。这意味着如果启动流程或轮询逻辑出现回归错误，**只能通过手动运行验证**。建议至少编写 `runLoop` 的 mock 集成测试（注入 MockSink 和 MockStatusSource）。

---

## 八、`internal/metric`（公共类型）— 无测试文件

| 内容 | 说明 |
|---|---|
| `Color` 枚举 | 纯常量/枚举，无业务逻辑 |
- `Cell` 结构体 | 纯数据载体 |
- `Group` 枚举 | 纯常量 |

> **结论：此包无需单测**——没有可执行逻辑，仅为其他包提供类型定义。

---

## 优先级建议

### P0 — 核心逻辑缺失（必须补充）

| 缺失数量 | 关键缺失 | 包 |
|---|---|---|
| ~8 个 | **StatusSource.Fetch()**——执行 SQL 并更新 cur/prev 映射 | `mycol` |
| ~4 个 | **StatusSource.SlaveStatus/Databases/ShowVariables/InnodbStatus**——所有查询路径 | `mycol` |
| ~1 个 | **Open()**——建立连接的完整路径（可用 mock 驱动） | `mysqlc` |

> 这 8 个方法是**所有 MySQL 采集器的数据源**。如果它们有 bug，即使 47% 的 mycol 覆盖率也测不出来，因为所有子 collector 都依赖 StatusSource 提供数据。

### P1 — 边界分支缺失（应当补充）

| 缺失数量 | 关键缺失 | 包 |
|---|---|---|
| ~5 个 | 各 Collector 的 **HasPrev=false (first-tick) 零值分支**——InnodbData/InnodbLog/InnodbStatus 等 | `mycol` |
| ~4 个 | `StatusSource` 的 `ok=false` 降级分支（Fetch 失败时各 collector 退化为零值） | `mycol` |
| ~3 个 | `Config.DSN()` TLS 分支、`DefaultCNFSearch` 全路径探测 | `mysqlc` |

### P2 — 纯返回值缺失（建议补充）

| 缺失数量 | 关键缺失 | 包 |
|---|---|---|
| ~30 个 | 所有 `Name()` / `Headline()` 纯返回函数——虽然无分支逻辑，但项目约定"每个 collector 必须有单测" | `syscol`, `mycol` |
| ~1 个 | `(*Renderer).ResetHeaderCounter()` 效果验证 | `render` |
| ~1 个 | `(*File).reopen()` 单独测试 | `logsink` |

### P3 — 集成场景缺失（建议补充）

| 缺失数量 | 关键缺失 | 包 |
|---|---|---|
| ~15 个 | `main()` 启动流程、`runLoop()` 完整逻辑（day-rollover/count 退出/header 重打） | `cmd/orzdba` |

### P4 — 安全路径缺失（Linux 环境补充）

| 缺失数量 | 关键缺失 | 包 |
|---|---|---|
| ~15 个 | `Start()` 三种错误路径、`Collect()` 崩溃探测与重启、`Stop()` 精准清理 | `rtcol` |

---

## 附录：各包单测文件清单

| 包 | 源码文件数 | 单测文件数 | 单测用例数 |
|---|---|---|---|
| `cmd/orzdba` | 2 | 1 | 18 |
| `internal/logsink` | 4 | 1 | 7 |
| `internal/mycol` | 11 | 1 | 29 |
| `internal/mysqlc` | 3 | 2 | 15 |
| `internal/render` | 3 | 1 | 6 |
| `internal/rtcol` | 1 | 1 | 4 |
| `internal/syscol` | 6 | 1 | 16 |
| **合计** | **31** | **8** | **96** |

---

（完）
