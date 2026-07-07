# orzdba

一个用 Go 重写的 MySQL & Linux 主机实时监控工具，基于淘宝 DBA 团队的 Perl 原版重新实现。

| | |
|---|---|
| **状态** | 活跃开发中 — M0–M8 已完成，M9 待完成 |
| **测试** | 84 项通过，覆盖 7 个包 |
| **路线图** | [`go-rewrite-plan.md`](go-rewrite-plan.md)（v2.0 里程碑） |
| **仓库** | [x777777x/orzdba](https://github.com/x777777x/orzdba) |

## 安装

```bash
make build
# 或直接编译
go build -o bin/orzdba ./cmd/orzdba
```

## 快速开始

```bash
# 仅查看系统信息（无需 MySQL）
./bin/orzdba -sys -i 1 -C 5

# 完整 MySQL 监控，输出到日志文件并按天轮转
./bin/orzdba -lazy -d sda -C 100 -i 2 -L /tmp/orzdba.log -logfile_by_day
```

运行 `orzdba -h` 可查看完整参数列表。

## 项目结构

```
cmd/orzdba/           CLI 入口：参数解析、主循环、标题块
internal/metric/      共享类型：Cell、Group、Color
internal/syscol/      /proc 采集器：load、cpu、swap、net、disk
internal/mycol/       MySQL 采集器：com、hit、innodb_*、threads、bytes、slave、semi
internal/rtcol/       tcprstat 响应时间采集器（仅 Linux）
internal/mysqlc/      MySQL 连接与凭证解析（my.cnf、环境变量、CLI）
internal/render/      ANSI 颜色渲染与列格式化
internal/logsink/     输出：stdout / 单文件 / 按天轮转文件
testdata/             单元测试用的 /proc 黄金样本
```

### 架构

```
┌──────────────────────────────────────────────────┐
│                    main.go                       │
│  ┌─────────┐  ┌─────────┐  ┌──────────────────┐ │
│  │  syscol │  │  mycol  │  │  rtcol (Linux)   │ │
│  │ (load,  │  │(com,    │  │ (tcprstat)       │ │
│  │  cpu,   │  │ hit,    │  └──────────────────┘ │
│  │  swap,  │  │innodb_, │                        │
│  │  net,   │  │threads..)│  ┌──────────────────┐ │
│  │  disk)  │  └────┬─────┘  │   render/        │ │
│  └─────────┘       │        │  (ANSI, 列对齐)  │ │
│            ┌───────┘        └──────────────────┘ │
│            │  ┌─────────────────────────────┐    │
│            │  │  StatusSource (每 tick 一次) │    │
│            │  └─────────────────────────────┘    │
│            │                                     │
│            ▼                                     │
│  ┌──────────────────────────────────────────┐   │
│  │              轮询主循环                   │   │
│  │  采集 → BuildRow → sleep(interval)      │   │
│  └──────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

- **每 tick 一条 SQL**。`StatusSource` 每个采样间隔只发一次 `SHOW GLOBAL STATUS`，然后将结果分发给所有 MySQL 子模块。
- **单连接**。`MaxOpenConns=1`，不使用连接池，避免连接抖动。
- **零 fork**。所有 `/proc` 读取使用 Go 标准库；MySQL 通过 `database/sql` + `go-sql-driver/mysql` 驱动走原生协议。唯一用到 `exec` 的是 `tcprstat`，通过 PID 跟踪实现精准清理。

## 里程碑

| ID | 功能 | 状态 |
|----|------|------|
| M0 | 仓库骨架、CI、包占位 | ✅ 已完成 |
| M1 | CLI：pflag 解析、单杠长选项（`-sys`/`-mysql`）、组合参数展开、SIGINT/SIGTERM 信号清理 | ✅ 已完成 |
| M2 | `/proc` 采集器（load、cpu、swap、net、disk）；ANSI 渲染器、15 行表头重打、`nocolor` 模式 | ✅ 已完成 |
| M3 | MySQL 连接与凭证解析（自实现 my.cnf 解析器，5 源优先级合并） | ✅ 已完成 |
| M4 | `-mysql` 子模块：com（QPS/TPS）、hit、threads、bytes；首次采样零值、逐 tick 差值 | ✅ 已完成 |
| M5 | `SHOW ENGINE INNODB STATUS` 文本解析（历史列表、日志、读视图、活动/排队查询） | ✅ 已完成 |
| M6 | `-slave`（SHOW SLAVE STATUS）、`-semi`（Rpl_semi_sync_*）、`-hit full`（5 列扩展命中率） | ✅ 已完成 |
| M7 | `-rt` tcprstat 响应时间采集器：子进程生命周期（SIGTERM→200ms→SIGKILL）、崩溃重试一次、0600 日志、O_EXCL 锁 | ✅ 已完成 |
| M8 | `logsink`：stdout / 单文件（0600）/ 按天轮转文件（标题重打 + 计数器重置） | ✅ 已完成 |
| **M9** | **与 Perl 原版的黄金样本行为对齐测试** | **待完成** |

## 与 Perl 原版的偏差

以下为 **有意为之** 的修改，用于修复 bug 或提升日志可用性：

1. **时间列**：打印 `YYYY-MM-DD HH:MM:SS` 而非 `HH:MM:SS`，每行带完整日期（多日日志文件更方便）。
2. **网络解析**：使用 `strings.Fields` 分割（`recv` = field[1]、`send` = field[9]）；Perl 的 `split(/\s+|:/)` 存在 off-by-one 错误，会读到空字段。
3. **磁盘设备检查**：当 `/proc/diskstats` 不存在时（非 Linux 开发机），跳过设备存在性检查，工具退化为零值输出；在 Linux 上遇到真正的无效设备名仍会报错。

## 设计约束

| 约束 | 实现方式 |
|------|---------|
| **性能** | 每 tick 0 次 fork，≤ 1 条 SQL，RSS ≤ 30 MB |
| **安全** | MySQL 密码不出现在进程命令行；日志文件权限 0600；tcprstat 子进程通过 PID 跟踪（无 `killall`） |
| **无 shell 调用** | `cat`/`grep`/`sed`/`awk`/`mysql`/`ifconfig` 全部由 Go 标准库或原生驱动替代 |

详见路线图文档中的完整性能与安全预算。

## 测试

```bash
make test            # 运行全部 84 项测试
go test ./... -v     # 详细输出 — 列出每个测试用例
go test ./... -cover # 按包显示覆盖率
```

各包覆盖率：

| 包 | 覆盖率 |
|----|--------|
| `render` | 92.3% |
| `mysqlc` | 69.9% |
| `syscol` | 68.1% |
| `logsink` | 72.7% |
| `mycol` | 47.6% |
| `cmd/orzdba` | 29.3% |
| `rtcol` | 16.7% *(仅 Linux 二进制，macOS 跳过)* |
