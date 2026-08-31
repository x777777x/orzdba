# orzdba

一个用 Go 重写的 MySQL & Linux/macOS 主机实时监控工具，基于淘宝 DBA 团队的 Perl 原版重新实现。

## 功能

监控两类指标，可任意组合：

| 域 | 指标 | 触发参数 |
|----|------|---------|
| **主机级 (host)** | CPU 负载 | `-l`/`--load` |
| | CPU 使用率 | `-c`/`--cpu` |
| | 内存使用率 / 全字段 | `-m`/`--mem` |
| | 磁盘 IO（单盘或多盘） | `-d`/`--disk sda,sdb` |
| | 网卡收发 | `-n`/`--net eth0` |
| | swap | `-s`/`--swap` |
| **数据库级 (db)** | QPS/TPS、命中率、InnoDB、线程、字节、主从、半同步 | `-mysql`/`-innodb`/`-slave`/`-semi` 等 |

提供三个组合参数，一条命令即可按域采集，无需逐个列参数：

- `-sys`：主机信息合集 `-t -l -c -s -m`（无需 MySQL）
- `-mysql`：MySQL 信息合集 `-t -com -hit -T -B`（QPS/TPS、命中率、线程、字节）
- `-lazy`：常用合集 `-t -l -c -s -com -hit`（主机 + MySQL 常用指标）

### 全局展示参数

| 参数 | 说明 |
|------|------|
| `--unit` | 默认输出**原始数值**（bytes/s、字节、百分比浮点，无 k/m/g 后缀，便于转存 ES 做趋势分析）；传 `--unit` 切换为人类可读单位（k/m/g） |
| `--full` | 对 host 模块输出**全字段**（内存 total/used/free/avail/buff/cached、CPU 9 列、网卡 rx/tx 8 列、磁盘扩展列） |

## 支持的操作系统

| OS | 系统指标数据源 | 说明 |
|----|--------------|------|
| **Linux** | `/proc`（loadavg/stat/meminfo/vmstat/net/dev/diskstats） | 完整支持全部指标，含磁盘队列/服务时间 |
| **macOS** | 原生 API（sysctl / host_statistics / getifaddrs / IOKit） | 支持 load/cpu/mem/swap/net/disk；无 iowait/steal；swap 显示当前用量；磁盘无队列/服务时间（`queue/await/svctm/%util` 恒 0） |
| Windows / BSD | — | 可编译，系统指标未实现（输出 0） |

## 安装

```bash
make build
# 或直接编译（不注入编译信息，--version 显示默认值）
go build -o bin/orzdba ./cmd/orzdba
```

`make build` 会自动注入**版本号**（git tag/提交号）、**Git 提交号**、**编译时间**（UTC），可用 `orzdba --version` 查看：

```
$ orzdba --version
orzdba e89276b-dirty
commit:    e89276b
built:     2026-08-31T09:14:41Z
```

## 快速开始

### Linux

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

Linux 磁盘设备名为 `/dev/` 下的块设备（如 `sda`、`vda`、`nvme0n1`），网卡名为 `eth0`/`ens33` 等。

### macOS

```bash
# 系统信息（macOS 原生 API 采集）
./bin/orzdba -sys -i 1 -C 5

# 内存/CPU/网卡全字段
./bin/orzdba -m -c -n en0 --full -i 1

# 磁盘 IO（macOS 用 disk0/disk1 等设备名）
./bin/orzdba -d disk0 -i 1 -C 5
```

macOS 磁盘设备名为 `disk0`/`disk1` 等（可用 `ls /dev/disk*` 查看），网卡名为 `en0`/`en1` 等。

### 连接 MySQL

默认连接本机 `127.0.0.1:3306`。远程或指定账号：

```bash
./bin/orzdba -H 192.168.1.10 -P 3306 --mysql-user root --mysql-pass 'xxx' -mysql -i 1 -C 5
```

凭证解析优先级：**命令行 > 环境变量（`ORZDBA_MYSQL_USER`/`ORZDBA_MYSQL_PASS`）> my.cnf（`~/.my.cnf` 等）**。密码不会出现在进程命令行中。

### MySQL 连接参数

| 参数 | 说明 |
|------|------|
| `-H, --host` | MySQL 主机（默认 127.0.0.1） |
| `-P, --port` | 端口（默认 3306） |
| `-S, --socket` | 使用 Unix socket 连接 |
| `--mysql-user` / `--mysql-pass` | 用户名 / 密码 |
| `--mysql-defaults-file` | 指定 my.cnf 文件 |
| `--mysql-timeout` | SQL/连接超时（默认 1s） |
| `--mysql-tls` | 启用 TLS |

### 运维参数

| 参数 | 说明 |
|------|------|
| `--daemon` | 后台运行（daemon 化，仅 Unix；Windows 不支持）。不指定 `-L` 时自动写 `/tmp/orzdba.log`（按天截转） |
| `-L <path> --also-stdout` | 写文件的同时也输出到 stdout（双写）；文件默认按天截转需加 `-logfile_by_day` |
| `-noheader` | 不输出表头（启动标题块 + 周期性表头都关闭） |
| `--sep <s>` | 自定义数据行列分隔符（默认 `\|`；`\t` 表示制表符；指定后**每个数值列**都用该符号分隔，时间列保持整体） |

示例：

```bash
# 后台运行 MySQL 监控，日志按天截转
./bin/orzdba --daemon -mysql -L /var/log/orzdba.log -logfile_by_day

# 前台运行，同时输出到屏幕和文件
./bin/orzdba -sys -L /tmp/orzdba.log --also-stdout -i 1

# 无表头、逗号分隔（便于导入表格/脚本处理）
./bin/orzdba -sys -noheader --sep , -i 1 -C 5
```

运行 `orzdba -h` 可查看完整参数列表。

## 设计要点

- **每 tick 一条 SQL**：`StatusSource` 每个采样间隔只发一次 `SHOW GLOBAL STATUS`，结果分发给所有 MySQL 子模块，避免 orzdba-go 的每模块各查一次。
- **单连接**：`MaxOpenConns=1`，无连接池，避免连接抖动；连接断开时自动重建。
- **零 fork**：`/proc` 读取、MySQL 协议均用 Go 标准库实现，唯一外部命令是 `tcprstat`（`-rt`，仅 Linux，需 `/usr/bin/tcprstat`）。
- **计数安全**：累计计数器回退（MySQL 重启）时输出 0 而非负速率；速率按真实采样窗口计算。

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

## 测试

```bash
make test   # 运行全部单元测试
```

设计文档与开发路线图见 [`go-rewrite-plan.md`](go-rewrite-plan.md) 与 `docs/`。
