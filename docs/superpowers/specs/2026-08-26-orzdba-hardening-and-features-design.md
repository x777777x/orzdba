# orzdba 加固与功能扩展设计

日期：2026-08-26
状态：已确认（用户逐项拍板）

## 1. 背景与目标

在既有 orzdba Go 重写版基础上：
1. 修复代码复核发现的 P0–P3 缺陷；
2. 新增多磁盘监控（`-d sda,sdb`）；
3. 新增单位转换开关 `--unit`（默认输出原始数值，便于未来转存 Elasticsearch）；
4. 新增内存使用率模块（`-m`）；
5. 新增全局 `--full` 参数（已指定的 host 侧模块输出全部字段）。

设计约束（用户强调）：
- 进程**每秒**采样，查询数据量大，绝不能造成服务器性能波动；
- 连接数据库必须有**超时控制**，不能造成连接拥堵/阻塞；
- 面向 ES 转存：默认数值不带单位后缀。

## 2. 设计原则

1. **改动集中于表示层**：`metric.Cell` 同时携带显示文本与原始数值；采集器不关心单位策略。
2. **单位转换是横切关注点**：`UnitMode`（Raw/Human）贯穿所有字节类与百分比类指标。
3. **多磁盘向后兼容**：`-d sda` 仍可用，`-d sda,sdb` 为扩展。
4. **先修缺陷再叠功能**：P0（崩溃/热循环）最先修。
5. **不加 scope 参数**：host/db 采集域由现有 `-sys`/`-mysql` 表达；仅文档标注归属。

## 3. 数据模型（internal/metric/types.go）

```go
// UnitMode 控制字节/百分比类指标的数值呈现。
type UnitMode uint8

const (
    UnitRaw   UnitMode = iota // 默认：原始数值（字节/秒、字节、百分比浮点），ES 友好
    UnitHuman                 // 人类可读：k/m/g 后缀，沿用 Perl 兼容格式
)

// Cell 增加原始数值字段（Raw）。Text 仍为显示文本（渲染器按 mode 决定）。
type Cell struct {
    Text  string
    Raw   float64 // 原始数值（bytes/s、bytes、百分比）；非数值类为 0
    Color Color
}
```

## 4. 修复清单

### P0（崩溃/热循环）
- **P0-1 `--header-period 0` panic**：parseArgs 校验 `headerPeriod >= 1`；runLoop 使用经 `renderer.Period()` 规范化后的值（消除 `mycount % cfg.headerPeriod` 除零）。
- **P0-2 `-i 0`/负 interval**：parseArgs 校验 `interval >= 1`；`time.Sleep(interval)` 不再可能空转热循环，swap/net/StatusSource 不再除以 0 产生 `+Inf`。

### P1
- **P1-1 rt 锁按 PID 命名防不住双实例**：锁文件改按**端口**命名 `/tmp/orzdba_tcprstat.p<port>.lck`，内容写入 PID；Start 时若锁存在则校验持有者存活（`kill(pid,0)`），存活则报错、死亡则清理重建。日志文件保持按 PID（多实例隔离），锁与日志分离。
- **P1-2 tcprstat 日志无限增长 + 每 tick 全量读**：`lastSample` 改为只读文件**尾部**（`os.Open` + `Seek(-tailBytes)`），并在读取后若文件超过阈值（如 1 MiB）`Truncate(0)`+`Seek(0)` 重置，防止无界增长。
- **P1-3 O_TRUNC 重启清日志**：`openFile` 改 `O_CREATE|O_WRONLY|O_APPEND`（不 truncate）；日切文件同样追加。标题块只在**文件为空（新文件）**时打印，避免重启后重复标题。
- **P1-4 sink 失败路径孤儿 tcprstat**：main 中先 `logsink.New` 再 `rtCol.Start()`；并加 `defer rtCol.Stop()` 兜底任何提前 return。
- **P1-5 信号 vs 主循环 data race**：`Collector` 增加 `sync.Mutex`，`Stop/Collect/restart/Start` 中共享字段（`cmd/exited/started/restarts`）加锁；`exited` 的 atomic 保留用于快速路径。
- **P1-6 Fetch 恢复后速率翻倍**：`StatusSource` 记录 `lastFetchTime`；`Rate()` 以**真实经过时间** `(now - lastFetchTime)` 为分母（下限为 interval，上限封顶），避免断连恢复后瞬时速率虚高。

### P2
- **P2-1 `-hit` 首行 100.00**：`collectOne` 增加 `HasPrev` guard，首行输出 0。
- **P2-2 `-i5` 连写被破坏**：`normalizeArgs` 改为**已知长名白名单**匹配（`sys/mysql/lazy/innodb/nocolor/...`），不再对任意 2+ 字符 token 做 `-x`→`--x` 改写；短选项连写（`-i5`、`-C5`）恢复 pflag 原生支持。
- **P2-3 `--tps-mode commit` 静默忽略**：实现 commit 模式，TPS = `Com_commit + Com_rollback` 差值（`Com_commit`/`Com_rollback` 已在 statusVars 中）。
- **P2-4 `-n` 网卡无启动校验**：启动时校验 `/proc/net/dev` 存在该设备（与 disk 对称），缺失报错退出。
- **P2-5 `-logfile_by_day` 无 `-L` 静默忽略**：parseArgs 校验报错。
- **P2-6 位置参数静默忽略**：`fs.Args()` 非空 → 报错。
- **P2-7 `-rt` 在 IPv6-only/`?` 时静默失败**：`primaryIP()=="?"` 且 `-rt` → 启动报错。

### P3
- **P3-1 死代码**：`Renderer.period/since` 只在 runLoop 用到 `since`；统一由 `r.Period()` 提供周期，`since` 保留用于日切重置。删除未读字段。
- **P3-2 `Swap.Collect` 冗余分支**：简化为单分支。

## 5. 功能 1：多磁盘（-d sda,sdb）

- **CLI**：`-d`/`--disk` 逗号分隔设备列表。parseArgs 拆分；`checkDiskDevice` 逐设备校验。
- **采集层**（syscol/disk.go）：
  - `Disk` 持 `devices []string`，`prev map[string]diskStat`。
  - `parseDiskStats(data []byte) map[string]diskStat`（一次解析整个 `/proc/diskstats`）。
  - `Collect()` 逐设备生成 7 列 cells，顺序与列头一致。
- **渲染层**：`Headline()` 为每设备生成一段列头（`sda:...|sdb:...`）。
- 单设备行为与现状完全一致。

## 6. 功能 2：单位转换（--unit）

- **CLI**：`--unit`（bool）。默认 `UnitRaw`（原始数值）；传参 → `UnitHuman`。
- **render/format.go** 新增：
  - `FormatBytesValue(b float64, mode UnitMode, mWidth, sWidth int) string`
  - `FormatPercentValue(p float64, mode UnitMode) string`
- **受影响字节类**（Raw 填 `bytes/s` 或 `bytes`）：
  - `net.go` recv/send（bytes/s）
  - `bytes.go` recv/send（bytes/s）
  - `innodb_data.go` read/written（bytes/s）
  - `innodb_log.go` written（bytes/s）
  - `innodb_status.go` unflushed/ucheckpointed（bytes 绝对值）
  - `disk.go` rkbs/wkbs（Raw = `sectors×512/interval` bytes/s；Human 沿用 KiB/s 显示）
  - 标题块 `mysqlVarsLines` 字节变量（Raw 用字节整数，Human 用 G/M）
- **百分比类**（cpu/hit/threads 命中率）：Raw 模式输出原始浮点（如 99.6），Human 模式整数（现状）。

## 7. 功能 3：内存模块（-m）

- **CLI**：`-m`/`--mem`（bool）。默认仅显示使用率。
- **采集层**（syscol/mem.go）读 `/proc/meminfo`：
  - 使用率 = `(MemTotal - MemAvailable) / MemTotal × 100`（MemAvailable 3.14+；缺失回退 `free + buff + cache`）。
  - `--full` 时输出 `total used free available buff cached` 全字段。
- **渲染**：加入 sys 组；列头 `---memory----`。

## 8. 功能 4：全局 --full

- **CLI**：`--full`（bool），作用于所有已指定的 host 侧模块。
- **矩阵**：
  - 内存 `-m`：仅 `usage%` → `total used free available buff cached`
  - CPU `-c`：`usr sys idl iow`（4 列）→ 9 列（`usr nice sys idle iowait irq softirq steal guest`，`/proc/stat` 解析扩到 10 字段）
  - 网卡 `-n`：`recv send`（2 列）→ 8 列（rx/tx bytes+packets+errs+drop）
  - 磁盘 `-d`：7 列 → 追加 `avgqu-sz avgrq-sz %util`（iostat 完整列）
- `--full` 与 `--unit` 正交（列数 vs 数值单位）。不影响 MySQL 模块。`-hit full` 的 `full` 是值，与 `--full` flag 不冲突。

## 9. 性能与安全（用户强调）

- **每 tick 一次 `/proc` 全量读取**：所有采集器复用同一次 `os.ReadFile` 结果（多磁盘/多网卡不重复读文件）；每 tick 至多 1 条 `SHOW GLOBAL STATUS` + 可选 1 条 `SHOW ENGINE INNODB STATUS`/`SHOW SLAVE STATUS`。
- **MySQL 连接**：单连接 `MaxOpenConns=1`；所有查询 `context.WithTimeout(mysqlTimeout)`（默认 1s），杜绝连接堆积；`SetConnMaxLifetime(0)` 保持长连接，断连由驱动自动重建。
- **tcprstat 日志**：尾部读 + 阈值 truncate，防无界增长与每 tick O(n) 全量读。
- **凭证安全**：保持不落 argv（`--mysql-pass` 仍为调试专用）；日志 0600；锁文件 0600。
- **数值安全**：所有差值用 int64 防溢出；`/proc` 解析失败降级 0 不 panic；`--unit` 下 Raw 为纯数值无后缀。

## 10. 测试

- 单元测试覆盖每个修复与新功能；`go test -race ./...` 全绿。
- 黄金样本：`meminfo_tick*.txt`、多设备 `diskstats` 样本。
- 集成断言：`--header-period 0`/`-i 0` 不再 panic；`-d sda,sdb` 列对齐；`--unit` 输出无 k/m 后缀。

## 11. 文档

- README 参数表、`--unit`/`--full`/`-m`/多磁盘语义、host/db 归属标注。
- usage 文本同步。
