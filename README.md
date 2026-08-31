# orzdba

一个用 Go 重写的 MySQL & Linux/macOS 主机实时监控工具，基于淘宝 DBA 团队的 Perl 原版重新实现。

| | |
|---|---|
| **状态** | 活跃开发中 — M0–M8 已完成，M9 待完成 |
| **测试** | 全量单元测试通过（`go test -race ./...`），覆盖 7 个包 |
| **路线图** | [`go-rewrite-plan.md`](go-rewrite-plan.md)（v2.0 里程碑） |
| **仓库** | [x777777x/orzdba](https://github.com/x777777x/orzdba) |

## 版本说明

- **当前版本**：`0.1.0-dev`（`cmd/orzdba/main.go` 中 `version` 变量，正式发版时通过 `-ldflags "-X main.version=..."` 注入发布版本）
- **代码基线**：2026-08-31，提交 `5c2f1e8`（首次推送到远端仓库前的本地基线）
- **里程碑状态**：v2.0 计划 M0–M8 已完成，M9（与 Perl 原版黄金样本对齐）待完成
- **已验证**：
  - 单元测试：`go test ./...`（含 `-race`）7 个包全部通过
  - 交叉编译：linux/{amd64,arm64,386,arm}、darwin/{amd64,arm64}、windows/{amd64,arm64}、freebsd、openbsd
  - 指标正确性：真实 Linux aarch64 + MySQL 8.0.45 验证 load/cpu/mem/qps
  - 功能验证（2026-08-31，MySQL 8.0.45 @ 127.0.0.1:3306）：`-mysql`（QPS/TPS/线程/字节，Com_select 差值随负载增长）、`-innodb`（buffer pool 页面解析）
  - macOS 系统指标（2026-08-31，本机 darwin/arm64）：`-sys` 的 load/cpu/mem/swap/net/disk 全部采集真实值（sysctl/host_statistics/getifaddrs/IOKit）；centos:7 与 ubuntu:22.04（aarch64 容器）Linux 回归 7 包单元测试全绿

## 指标域

| 域 | 指标 | 触发参数 |
|----|------|---------|
| **主机级 (host)** | 内存使用率/全字段 | `-m`/`--mem` |
| | CPU 使用率 | `-c`/`--cpu` |
| | CPU 负载 | `-l`/`--load` |
| | 磁盘 IO（单盘或多盘） | `-d`/`--disk sda,sdb` |
| | 网卡收发 | `-n`/`--net eth0` |
| | swap | `-s`/`--swap` |
| **数据库级 (db)** | QPS/TPS、命中率、InnoDB、线程、字节、主从、半同步 | `-mysql`/`-innodb`/`-slave`/`-semi` 等 |

用 `-sys`（host 域合集 `-t -l -c -s -m`）或 `-mysql`（db 域合集）组合即可按域采集，无需额外的 scope 参数。

## 全局展示参数

| 参数 | 说明 |
|------|------|
| `--unit` | 默认**原始数值**（bytes/s、字节、百分比浮点，无 k/m/g 后缀，ES 友好）；传 `--unit` 切换为人类可读单位 |
| `--full` | 对已指定的 host 模块输出**全字段**（内存 total/used/free/avail/buff/cached、CPU 9 列、网卡 rx/tx 8 列、磁盘扩展列） |

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

# 主机级全字段监控（内存/CPU/网卡/磁盘），原始数值（ES 友好）
./bin/orzdba -m -c -n eth0 -d sda,sdb --full -i 1

# 人类可读单位（k/m/g）
./bin/orzdba -lazy --unit -i 1
```

运行 `orzdba -h` 可查看完整参数列表。

## 项目结构

```
cmd/orzdba/           CLI 入口：参数解析、主循环、标题块
internal/metric/      共享类型：Cell、Group、Color
internal/syscol/      系统采集器：load、cpu、swap、net、disk（Linux /proc + macOS 原生 API，build tag 隔离）
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
| M7 | `-rt` tcprstat 响应时间采集器：子进程生命周期（SIGTERM→200ms→SIGKILL）、崩溃重试一次、0600 日志、端口锁（P1-1） | ✅ 已完成 |
| M8 | `logsink`：stdout / 单文件（0600，追加不截断）/ 按天轮转文件（标题重打 + 计数器重置） | ✅ 已完成 |
| M10 | macOS 系统指标：load/cpu/mem/swap/net/disk 原生 API 采集（sysctl/host_statistics/getifaddrs/IOKit），build tag 隔离，Linux 零改动 | ✅ 已完成 |
| **M9** | **与 Perl 原版的黄金样本行为对齐测试** | **待完成** |

## 与 Perl 原版的偏差

以下为 **有意为之** 的修改，用于修复 bug 或提升日志可用性：

1. **时间列**：打印 `YYYY-MM-DD HH:MM:SS` 而非 `HH:MM:SS`，每行带完整日期（多日日志文件更方便）。
2. **网络解析**：使用 `strings.Fields` 分割（`recv` = field[1]、`send` = field[9]）；Perl 的 `split(/\s+|:/)` 存在 off-by-one 错误，会读到空字段。
3. **磁盘设备检查**：在 Linux 上读取 `/proc/diskstats` 校验设备存在，遇到无效设备名会报错；macOS 通过 IOKit 枚举块设备校验。Windows/freebsd/openbsd 无 `/proc` 且未实现原生采集，跳过检查并退化为零值输出。网卡同理（Linux `/proc/net/dev`，macOS `getifaddrs`）。
4. **单位**：默认输出**原始数值**（bytes/s、字节、百分比浮点），不带 k/m/g 后缀——便于转存 Elasticsearch 做趋势分析；用 `--unit` 切换为人类可读单位（Perl 兼容格式）。
5. **日志追加**：`-L` 与 `-logfile_by_day` 使用追加模式（`O_APPEND`），重启不再清空已有日志；标题块仅在文件为空（新文件）时打印，避免重复标题。
6. **macOS 系统指标（M10 新增）**：macOS 无 `/proc`，改用原生 API 采集——load 用 `sysctl vm.loadavg`（fixpt_t 定点数）、cpu 用 `host_cpu_load_info`、mem 用 `hw.memsize` + `host_statistics64`、swap 用 `vm.swapusage`、net 用 `getifaddrs`、disk 用 IOKit。语义差异：macOS 无 iowait/steal（恒 0）；swap 显示当前用量（`si`=已用、`so`=可用字节）而非 Linux 的 si/so 速率；disk 无队列/服务时间统计，`queue/await/svctm/%util` 恒 0，仅 `r/s w/s rkB/s wkB/s` 有真实值。Linux 行为完全不变。

## 设计约束

| 约束 | 实现方式 |
|------|---------|
| **性能** | 每 tick 0 次 fork，≤ 1 条 SQL（`-innodb_status`/`-slave` 时 ≤ 3 条），RSS ≤ 30 MB；tcprstat 日志尾部读 + 超阈值截断 |
| **安全** | MySQL 密码不出现在进程命令行；日志文件权限 0600；tcprstat 子进程通过 PID 跟踪（无 `killall`） |
| **无 shell 调用** | `cat`/`grep`/`sed`/`awk`/`mysql`/`ifconfig` 全部由 Go 标准库或原生驱动替代 |

详见路线图文档中的完整性能与安全预算。

## 测试

```bash
make test            # 运行全部单元测试
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
