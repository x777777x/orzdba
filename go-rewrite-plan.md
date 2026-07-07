# orzdba Go 重写开发计划

> 文档版本：v2.0
> 编写日期：2026-07-05
> 目标读者：参与 Go 重写的开发与评审人员
> **新项目路径**：[x777777x/orzdba](https://github.com/x777777x/orzdba)（独立 git 仓库，`git@github.com:x777777x/orzdba.git`）
> 参考实现：
> - [`zhangchunsheng/orzdba`](https://github.com/zhangchunsheng/orzdba)（Perl 原版，1146 行）
> - [`lflxp/orzdba`](https://github.com/lflxp/orzdba)（社区 Go 实现，1823 行，MIT，作者 lflxp，2017）
> 参考文档：`orzdba工具使用说明.pdf`（淘宝 DBA，2012；本地文件，存放于 Perl 原版仓库根目录）

---

## 1. 文档目的与范围

### 1.1 目的
在 [x777777x/orzdba](https://github.com/x777777x/orzdba) 仓库中用 Go 重写 orzdba，**对齐 Perl 原版的输出语义**，**吸收 orzdba-go 的功能扩展**（slave/semi/扩展命中率），**修复 orzdba-go 的严重性能与安全缺陷**。核心硬约束：**被监控机的负载不能因本工具上升可观察量，数据库不能因本工具受到额外压力**。

### 1.2 范围
- **纳入范围**：
  - Perl 原版的全部命令行参数、采集器、渲染规则、日志按天轮转、Ctrl-C 信号清理、tcprstat 调用与日志清理；
  - orzdba-go 的功能扩展：`-slave`、`-semi`、扩展命中率（Key Buffer / Index / Qcache / Innodb）、Thread cache hit%、MySQL 远程连接（`-H`）；
  - MySQL 凭证的 my.cnf 解析、编译期注入、CLI/环境变量覆盖；
  - 性能与安全预算的全部硬约束（§9）。
- **不纳入范围**（本期不做）：
  - 替换 tcprstat 的原生 TCP 抓包（保留外部 `/usr/bin/tcprstat` 调用）；
  - Windows/macOS 系统指标（系统侧仍是 Linux `/proc` 语义）；
  - Web UI、HTTP 接口、时序数据库写入。
- **仓库关系**：
  - `orzdba-perl/`：Perl 原版，只读参考；
  - `orzdba-go/`：社区 Go 实现，**作为参考与反面教材**（详见 §2.2–2.4），不直接复用其代码；
  - `orzdba/`（新）：本次重写的目标仓库，独立 git 历史。

### 1.3 与 v1.x 计划的差异
v1.x 计划基于 Perl 原版分析；v2.0 在 v1.x 基础上：
1. 新增 §2.2–2.4 对 orzdba-go 的逐项分析（可借鉴点 / 必须修复的问题）；
2. 把 orzdba-go 已正确实现的功能（slave/semi/扩展 hit%）纳入范围；
3. 在 §7 关键实现要点中显式标注"避免 orzdba-go 的 X bug"；
4. 性能与安全预算（§9）针对 orzdba-go 的具体失败案例收紧约束。

---

## 2. 参考实现分析

### 2.1 Perl 原版（orzdba-perl/orzdba，1146 行）

**定位**：淘宝 DBA 团队 2010–2012 的实时轮询监控工具，按固定间隔采集 Linux 主机与 MySQL 指标，按列对齐打印到终端或日志。

**数据源**：
- 系统：`/proc/loadavg`、`/proc/stat`、`/proc/vmstat`、`/proc/net/dev`、`/proc/diskstats`、`/proc/cpuinfo`
- MySQL：`SHOW GLOBAL STATUS`、`SHOW VARIABLES`、`SHOW ENGINE INNODB STATUS`、`SHOW DATABASES`
- RT：外部 `/usr/bin/tcprstat`

**实现方式**：Perl 内嵌解析 `/proc`；MySQL 通过 shell 调用 `mysql -s --skip-column-names -uroot`（每 tick 每子模块各一次）；tcprstat 通过 `fork+exec` + `File::Lockfile` 进程锁。

**关键设计**：
- 系统侧采集与 MySQL 侧采集在主循环中顺序执行；
- 表头每 15 行重打一次；
- 首次采样输出 0（无前次值）；
- 颜色阈值固化（load>ncpu 红、cpu usr>10 红、iow>10 红、hit>99 绿等）；
- 日志按天切分，跨天时 `count -= mycount; mycount = 0`。

### 2.2 orzdba-go 参考实现分析（main.go，1823 行）

**作者**：lflxp，2017，MIT 协议。

**整体结构**（单文件）：
- `basic` 结构体（28–197 行）：**单一巨型结构体**承载所有采集字段——load/cpu/swap/net/disk/rt/Com_*/Innodb_*/Threads_*/Bytes_*/Log_*/Handler_read_*/Created_tmp_*/Key_*/Slow_queries/Select_*/Rpl_semi_sync_*/Slave_*；
- `flags` 结构体（202–240 行）：所有命令行参数；
- `flags.init()`（242–321 行）：用标准库 `flag` 解析；
- `createCommand()`（385–724 行）：**核心采集函数**，构建 shell 管道并执行；
- `gotNumber()`（916–1648 行）：**730 行的巨型渲染函数**，按启用的模块拼接 title/data；
- `add_log()`（1650–1676 行）：日志写入；
- `main()`（1678–1823 行）：入口、信号、主循环。

**orzdba-go 的功能扩展**（值得吸收，见 §2.3）：
- `-slave`：`SHOW SLAVE STATUS` 解析（Master_Host、IO/SQL_Running、Seconds_Behind_Master、Read/Exec_Master_Log_Pos）；
- `-semi`：半同步复制监控（`Rpl_semi_sync_master_*`）；
- `-H`：MySQL 远程主机（不再限定本机）；
- `-u/-p`：显式账号（不再硬编码 root）；
- 扩展 hit%：Key Buffer read/write hit、Index total/current hit、Qcache hit、Innodb hit 五列；
- Thread cache hit%：`(1 - Threads_created_diff / Connections_diff) * 100`；
- TPS = commit+rollback（Perl 用 insert+update+delete）；
- 启动期一次性展示更多变量（Max_used_connections、Aborted_*、Handler_read_*、Created_tmp_*、Key_*、Slow_queries、Select_*、Opened_tables）。

### 2.3 orzdba-go 可借鉴点

| 项 | 借鉴方式 |
|---|---|
| `basic` 单结构体承载一次快照 | 采纳模式，但拆分为 `SysSample`/`MySample`/`RTSample` 子结构，避免单结构体过胖 |
| `Colorize(text, fg, bg, underline, bold)` API | 采纳签名，但用 `uint8` 常量替代字符串 switch，且 `nocolor` 时**真正**不输出 ANSI |
| `changeUntils()` 自动 k/m/g 格式化 | 采纳，扩展到 g/pg（与 Perl 一致为 k/m/g） |
| `hit(num, in)` hit% 格式化 helper | 采纳模式 |
| 组合参数展开（mysql/innodb/sys/lazy） | 采纳，但**在 first 采样前展开**（见 §2.4 第 10 项 bug） |
| `first/second` 双快照 diff 模式 | 采纳 |
| `-slave` / `-semi` 功能 | 采纳为新模块 `internal/mycol/slave.go`、`internal/mycol/semi.go` |
| 扩展 hit%（5 列） | 采纳为 `-hit` 的可选扩展模式 `-hit full`，默认仍只输出 Innodb hit 以兼容 Perl |
| Thread cache hit% | 采纳为 `-T` 的附加列 |
| MySQL 远程连接 `-H` | 采纳，但默认仍走 unix socket |

### 2.4 orzdba-go 必须修复的严重问题（重写时禁用清单）

> 以下问题按严重度排序。重写版必须**逐条避免**，并在 §9 验收中显式检查。

#### P0 — 安全与性能的灾难性缺陷

1. **MySQL 密码出现在进程命令行**（406–457、498、567、603、680、702 行等所有 mysql 调用）：
   - 形如 `mysql -u<user> -p<pass> -e '...'`，**任何本机用户都能通过 `ps -ef` 或 `/proc/<pid>/cmdline` 看到密码**。
   - 重写版：**禁止 shell 调用 mysql 客户端**，用 `database/sql` + 驱动走原生协议，DSN 不进进程列表。

2. **整个工具基于 `bash -c <pipeline>`**（371–383 行 `execCommand`）：
   - 每次 `createCommand` 触发 **6+ 次 fork**（bash + cat/grep/sed/awk/xargs/mysql）；
   - Perl 原版每 tick 1 次 fork（mysql shell），orzdba-go 比 Perl **更糟 6 倍**；
   - **直接违反"不能增加服务器负载"的硬约束**。
   - 重写版：`/proc` 用 Go 直接读；MySQL 用原生驱动；**全程 0 次 fork**（tcprstat 仅启动一次，不计入 tick）。

3. **默认密码硬编码在源码**（254 行 `flag.String("p", "system", ...)`）：
   - 默认 `-u root -p system`，二进制分发即带默认凭证。
   - 重写版：**无默认密码**；凭证来自 my.cnf / 环境变量 / CLI / 编译期注入（见 §8）。

4. **日志文件权限 0606**（1667 行 `os.OpenFile(..., 0606)`）：
   - 0606 = owner rw / group write / other write —— **world-writable**，且含 MySQL 指标。
   - 重写版：日志文件强制 `0600`，并在 `--logfile` 创建后立即 `os.Chmod` 兜底。

5. **`killall tcprstat`**（1690、1784 行）：
   - 信号处理与正常退出都调用 `killall tcprstat`，会**杀掉主机上所有 orzdba 实例的 tcprstat**，包括其他用户的。
   - 重写版：用 `exec.Cmd` 启动 tcprstat，记录 PID，退出时只 `cmd.Process.Signal(SIGTERM)` 本实例的子进程。

6. **`os.O_APPEND` 作为 `log.New` 的 flags 参数**（1672 行 `log.New(lf, "", os.O_APPEND)`）：
   - `log.New` 第三参数是日志前缀 flag（`Ldate`/`Ltime`/`Lshortfile`），不是文件 flag；
   - `os.O_APPEND=8` 恰好等于 `Lshortfile|Ltime`，导致每行日志带 `时间 文件:行号: ` 前缀，污染数据。
   - 重写版：不用 `log` 包写数据日志，直接 `lf.Write([]byte(s))`；`log` 仅用于 stderr 错误。

#### P1 — 计算正确性 bug

7. **`-i` 间隔被忽略**（1768–1784 行主循环 `time.Sleep(time.Second)` 硬编码）：
   - `-i 2`、`-i 5` 完全无效，仍按 1 秒采样；
   - swap/net/disk 的 diff 计算还硬编码 `flag_info["interval"] == "1"` 判断（1081、1113、1081 行），非 1 时**永远输出 0**。
   - 重写版：`time.Sleep(time.Duration(interval) * time.Second)`；diff 用真实 elapsed time（`time.Since(prev)`）而非固定除数。

8. **magic number `/0.99`、`/0.9999`、`/0.9999`、`/10`**（1116–1117、1147–1154、1168 行）：
   - net_recv/net_send diff 除以 0.99；rs_disk/ws_disk 除以 0.9999；rkbs/wkbs 除以 1.9999；util 除以 10。
   - 这些是作者为"补偿 1 秒 sleep 的实际耗时"瞎凑的系数，**与真实 interval 完全无关**；
   - util `/10` 与 Perl 的 `100*ticks/deltams`（deltams = 1000*(usr+sys+idl+iow)/ncpu/HZ）语义完全不同。
   - 重写版：严格按 Perl §2.4.5 的 deltams 公式实现。

9. **CPU usr/sys 公式与 Perl 不一致**（1045–1048 行）：
   - orzdba-go：`usr = (cpu_usr2 - cpu_usr1) * 100 / total_diff` —— **不含 nice**；
   - Perl：`user_diff = (user2+nice2) - (user1+nice1)`；
   - orzdba-go：`sys = (cpu_sys2 - cpu_sys1) * 100 / total_diff` —— **不含 irq+softirq**；
   - Perl：`system_diff = (sys2+irq2+softirq2) - (sys1+irq1+softirq1)`；
   - orzdba-go 用**整数除法**，丢精度（Perl 用 `int(x+0.5)` 四舍五入）。
   - 重写版：严格按 Perl 公式，用浮点计算后 `math.Round`。

10. **组合参数展开发生在 first 采样之后**（1759–1757 行）：
    - `first := createCommand(info, 0)` 在 `if info["mysql"]==true {info["com"]=true...}` **之前**调用；
    - 导致 `-mysql` 启用时，first 采样没查 Com_*/hit/threads/bytes，全部为 0；
    - 第二次采样 second 拿到真实值，diff = second - 0 = second，**首行 diff 数据爆炸式偏大**。
    - 重写版：参数展开在 `first` 采样**之前**完成。

11. **`util_disk` 阈值分支不可达**（1214–1220 行）：
    ```go
    if util_disk > 80.0 { ... red ... }
    else if util_disk > 100.0 { ... " 100.0" green ... }  // 永远不可达
    else { ... green ... }
    ```
    重写版：先判 `>100` 截断为 100，再判 `>80`。

12. **`if 1 != 1` 死代码**（1063、1172、1178 行等多处）：
    - 大量 `if 1 != 1 { red } else { normal }` 永远走 else 分支；
    - usr/sys/idl/rs_disk/ws_disk 的红色阈值逻辑实际未生效。
    - 重写版：直接按 Perl 阈值条件实现。

13. **nocolor 仍输出 ANSI**（1697–1717 行）：
    - `nocolor` 时把颜色变量设为 `""`，但 `Colorize` 仍输出 `\033[;22m...\033[0m`；
    - 重写版：`nocolor` 时 `Colorize` 直接返回原文本，**零 ANSI 字节**。

#### P2 — 工程质量问题

14. **`runtime.GOMAXPROCS(runtime.NumCPU())`**（1679 行）：
    - Go 1.5+ 默认即用满核；显式调用无意义，且让 runtime 在多核调度 goroutine，对单线程轮询工具是额外开销。
    - 重写版：删掉；保持单 goroutine 主循环。

15. **`strconv.Atoi(..., _)` 全程忽略解析错误**（全文 50+ 处）：
    - MySQL status 返回非数字时（如 `"ON"`），静默为 0；
    - 重写版：解析失败记 stderr 警告，该字段输出 0，不中断。

16. **`perSecond_Int/Float` 的 `panic("not fount number type")`**（812、851 行）：
    - 类型断言失败直接 panic，整个工具崩溃；
    - 重写版：返回 error，由调用方决定降级。

17. **`basic` 字段类型不一致**（如 `Aborted_connects string` 但 `Connections int`）：
    - 反映了作者对哪些值可能是非数字的妥协；
    - 重写版：所有 status 值统一用 `int64`/`uint64`，非数字字段单独处理。

18. **`flag.NArg() != 0` 半成品**（318–320 行）：
    - 注释"预留给 MYSQL远端数据接收服务器"——死代码。
    - 重写版：删除。

19. **`hit()` 的 `< 0.01` 分支不可达**（907 行）：
    - 由于 `innodb_hit` 计算用 `+0.0001` fudge，永远接近 100，不会 < 0.01；
    - 重写版：按 Perl 逻辑——`read_request==0` 时直接返回 100.00。

20. **`ifconfig` 依赖**（590 行 net 采集）：
    - 用 `ifconfig <dev> | grep bytes | sed ...` 取网卡字节；
    - ifconfig 在容器/最小化镜像中常缺失；且字段位置随发行版变化；
    - 重写版：直接读 `/proc/net/dev`，按设备名过滤行，取 `field[1]`（recv bytes）和 `field[9]`（send bytes）。

21. **`/tmp/mysql.sock` 默认 socket**（257 行）：
    - Debian/Ubuntu 默认是 `/var/run/mysqld/mysqld.sock`，硬编码 `/tmp/mysql.sock` 在这些系统上连不上；
    - 重写版：默认 socket 为空字符串，由 my.cnf 解析或自动探测（`/tmp/mysql.sock`、`/var/lib/mysql/mysql.sock`、`/var/run/mysqld/mysqld.sock` 逐个尝试）。

22. **无单元测试**：单文件 `main.go`，无 `_test.go`。
    - 重写版：每个采集器/解析器接受 `io.Reader`/`[]byte`，用固化样本单测，覆盖率 ≥ 80%。

23. **`-h` 无自定义帮助**：依赖 `flag` 自动生成的 usage，无 sample。
    - 重写版：自定义 usage，含 sample 与参数分组说明，与 Perl 输出风格一致。

### 2.5 orzdba-go 与 Perl 的语义偏差汇总

| 项 | Perl | orzdba-go | 重写版选择 |
|---|---|---|---|
| TPS 定义 | insert+update+delete | commit+rollback | 提供 `-tps_mode=iud\|commit`，默认 `iud` 兼容 Perl |
| hit 列数 | 仅 Innodb hit | 5 列（Key/Index/Qcache/Innodb） | 默认 1 列兼容 Perl；`-hit full` 展开 5 列 |
| net 单位 | bytes/s | bytes/s（但 `/0.99` 系数错误） | 严格 `/elapsed`，单位 bytes/s |
| disk util | `100*ticks/deltams` 截断 100 | `ticks/10`（语义错） | 严格 Perl 公式 |
| cpu usr 含 nice | 是 | 否 | 是 |
| cpu sys 含 irq/softirq | 是 | 否 | 是 |
| interval 尊重 | 是（`sleep $interval`） | 否（硬编码 1s） | 是 |
| 组合参数展开时机 | 启动时 | first 采样后 | 启动时 |
| 表头重打周期 | 每 15 行 | 每 20 行 | 默认 15 兼容 Perl；`--header-period` 可配 |

---

## 3. 重写目标与原则

### 3.1 目标
1. **行为对齐 Perl**：相同参数下，去色后输出与 Perl 原版逐行一致（核心矩阵见 §13）；
2. **吸收 orzdba-go 扩展**：slave/semi/扩展 hit% 作为可选模块；
3. **性能零负担**：每 tick 0 fork、≤ 2 SQL、单 goroutine、RSS ≤ 30MB（§9）；
4. **安全合规**：MySQL 密码不进进程列表、日志 0600、tcprstat PID 精准清理（§8、§9）；
5. **结构清晰**：采集-渲染分离，便于单测与后续扩展。

### 3.2 非目标
- 不重写 tcprstat；
- 不支持非 Linux 平台的等价系统指标；
- 不引入 HTTP/Prometheus/JSON 输出（留待后续）；
- 不改命令行语义（参数名、默认值与 Perl 兼容）。

### 3.3 设计原则
1. **采集-渲染分离**：采集器产出结构化 `Sample`，渲染器负责格式化与着色；
2. **状态封装**：每个采集器持有自己的"上一次值"与"是否首次"；
3. **失败局部化**：单模块失败只影响该列；
4. **可测试性**：所有 `/proc` 与 MySQL 解析函数接受 `io.Reader`/`[]byte`；
5. **最小依赖**：仅 `pflag` + `go-sql-driver/mysql` 两个第三方库；
6. **YAGNI**：不为"将来可能支持多个数据库"预留接口。

---

## 4. 总体架构

### 4.1 分层

```
+--------------------------------------------------------------+
|                      cmd/orzdba (main)                       |  CLI 解析、组合参数展开、主循环、信号
+--------------------------------------------------------------+
        |                |                |              |
+---------------+ +----------------+ +--------------+ +-----------+
| internal/syscol    | | internal/mycol      | | internal/rtcol    | | internal/render|
| (系统采集器)  | | (MySQL 采集器) | | (RT 采集器)  | | (渲染层)  |
+---------------+ +----------------+ +--------------+ +-----------+
        |                |                |              |
        +-------+--------+----------------+--------------+
                |        |                |
            internal/proc  internal/mysqlc      internal/logsink
            (/proc)   (DB 连接+凭证)   (日志/轮转)
                |
            internal/metric (公共类型)
```

### 4.2 数据流（每个 tick）
1. `main` 计算真实 elapsed（`time.Since(prev)`）；
2. 按启用的采集器顺序调用 `Collect()`，每个返回 `Row`；
3. `render` 拼接为单行（含 ANSI 与分隔符）；
4. `logsink` 输出到 stdout 或当前日志文件；
5. 每 15 行重打表头；按天切分时重打标题；
6. `time.Sleep(interval)`。

### 4.3 运行时模型
- 单 goroutine 主循环；
- MySQL：启动时建立一条长连接，每 tick 复用，断开自动重连一次；
- tcprstat：启动一个子进程，stdout 重定向到临时文件；主循环每 tick 读最后一行；
- 信号：`signal.Notify` 监听 `SIGINT/SIGTERM`，触发精准清理。

---

## 5. 模块划分与职责

### 5.1 `cmd/orzdba`（main 包）
- 解析命令行（§6 兼容性矩阵）；
- **在 first 采样前**展开组合参数（`-sys`/`-mysql`/`-innodb`/`-lazy`）；
- 决定颜色开关、日志开关；
- 实例化采集器与渲染器；
- 跑主循环、处理 `-C`/`-logfile_by_day`/`SIGINT`；
- 启动时调用 `print_title`。

### 5.2 `internal/syscol`（系统采集器）
- 子模块：`load`、`cpu`、`swap`、`net`、`disk`；
- 接口：
  ```go
  type Collector interface {
      Name() string
      Headline() (string, string)   // 上行、下行
      Collect() Row
  }
  ```
- `cpu` 暴露 `DeltaUsr/DeltaSys/DeltaIdle/DeltaIow` 给 `disk` 用（`deltams` 依赖）；
- `disk` 启动时一次性读取 `ncpu` 与 `HZ`（`HZ` 硬编码 100，与 Perl 一致）。
- **禁止**：调用 `ifconfig`、`cat`、`grep`、`awk`、`sed` 等外部命令（避免 orzdba-go P0-2 问题）。

### 5.3 `internal/mycol`（MySQL 采集器）
- 子模块：`com`、`hit`、`innodb_rows`、`innodb_pages`、`innodb_data`、`innodb_log`、`innodb_status`、`threads`、`bytes`、`slave`、`semi`；
- 内部维护 `statusMap`（当前）与 `prevMap`（上一次）；
- **每 tick 只执行一次 `SHOW GLOBAL STATUS WHERE Variable_name IN (...)`**，结果塞 map 共享（避免 orzdba-go 每子模块各自 shell mysql 的问题）；
- `innodb_status` 单独执行 `SHOW ENGINE INNODB STATUS`，按 §7.6 解析；
- `slave`/`semi` 仅在启用时执行 `SHOW SLAVE STATUS`/相关 status。

### 5.4 `internal/rtcol`（RT 采集器）
- 启动 tcprstat 子进程：`exec.Command("/usr/bin/tcprstat", "--no-header","-t","1","-n","0","-p",port,"-l",ip)`；
- stdout 重定向到 `/tmp/orzdba_tcprstat.<pid>.log`（权限 0600）；
- **进程锁**：`O_CREAT|O_EXCL` 创建 `.lck`；
- 每 tick 读日志最后一行，取 `count/avg/avg_95/avg_99`；
- 退出：`cmd.Process.Signal(SIGTERM)`，等 200ms 后 `SIGKILL` 兜底，删 log/lck；
- **禁止**：`killall tcprstat`（避免 orzdba-go P0-5 问题）。

### 5.5 `internal/render`（渲染层）
- 维护表头缓冲（由各启用模块的 `Headline()` 拼接）；
- 行计数 `mycount`，每 15 行重打表头（`--header-period` 可配）；
- 颜色常量用 `uint8` 而非字符串（避免 orzdba-go 字符串 switch 开销）；
- `nocolor` 时 `Colorize` 直接返回原文本（避免 orzdba-go P1-13 问题）；
- 工具函数：`FormatBytesAuto`（k/m/g）、`FormatPercent`、`PadLeft`。

### 5.6 `internal/logsink`（日志与轮转）
- 接口：`Write(p []byte) (int, error)`、`MaybeRotate(now time.Time)`、`Close()`；
- stdout sink / 文件 sink / 按天切分 sink；
- 文件权限强制 `0600`，创建后 `os.Chmod` 兜底（避免 orzdba-go P0-4 问题）；
- 句柄长期持有，不每 tick open/close（避免 orzdba-go P2 资源浪费）。

### 5.7 `internal/mysqlc`（MySQL 连接与凭证）
- `conn.go`：长连接 + 重连 + 查询封装 + context 超时；
- `defaults.go`：my.cnf 解析 + 凭证优先级合并（§8）；
- **禁止**：`exec.Command("mysql", ...)`（避免 orzdba-go P0-1、P0-2）。

### 5.8 `internal/metric`（公共类型）
- `Row`：列值列表，每列含 `Text`、`Color`、`Width`；
- `SysSample`/`MySample`/`RTSample`：子结构体（避免 orzdba-go 单一巨型 basic）。

---

## 6. 命令行参数兼容性矩阵

> 与 Perl 原版兼容的参数必须保持单划线形式；orzdba-go 扩展的参数保留；新增参数用 `--` 长形式。

| 参数 | 类型 | 说明 | 来源 |
|---|---|---|---|
| `-h,--help` | flag | 自定义帮助 | Perl |
| `-i,--interval` | int | 采样间隔秒，默认 1 | Perl |
| `-C,--count` | int | 采样次数后退出 | Perl |
| `-t,--time` | flag | 输出当前时间 | Perl |
| `-nocolor` | flag | 关闭颜色（真正不输出 ANSI） | Perl |
| `-l,--load` | flag | load avg | Perl |
| `-c,--cpu` | flag | CPU 使用率 | Perl |
| `-s,--swap` | flag | swap in/out | Perl |
| `-d,--disk` | string | 磁盘 IO（如 `sda`） | Perl |
| `-n,--net` | string | 网卡收发（如 `eth0`） | Perl |
| `-sys` | flag | 组合：`-t -l -c -s` | Perl |
| `-com` | flag | MySQL QPS/TPS | Perl |
| `-hit` | flag | Innodb 命中率（默认 1 列） | Perl |
| `-hit full` | flag | 扩展 5 列命中率 | orzdba-go |
| `-innodb_rows` | flag | Innodb 行级状态 | Perl |
| `-innodb_pages` | flag | Innodb BP 页状态 | Perl |
| `-innodb_data` | flag | Innodb 数据状态 | Perl |
| `-innodb_log` | flag | Innodb log 状态 | Perl |
| `-innodb_status` | flag | `SHOW ENGINE INNODB STATUS` 解析 | Perl |
| `-innodb` | flag | 组合：`-t -innodb_pages -innodb_data -innodb_log -innodb_status` | Perl |
| `-T,--threads` | flag | 线程状态（含 thread cache hit%） | Perl+orzdba-go |
| `-B,--bytes` | flag | MySQL 收发字节 | Perl |
| `-rt` | flag | tcprstat DB 响应时间 | Perl |
| `-mysql` | flag | 组合：`-t -com -hit -T -B` | Perl |
| `-lazy` | flag | 组合：`-t -l -c -s -com -hit` | Perl |
| `-slave` | flag | `SHOW SLAVE STATUS` 解析 | orzdba-go |
| `-semi` | flag | 半同步复制状态 | orzdba-go |
| `-P,--port` | int | MySQL 端口，默认 3306 | Perl |
| `-S,--socket` | string | MySQL socket（默认空，自动探测） | Perl（改默认） |
| `-H,--host` | string | MySQL 主机，默认 127.0.0.1 | orzdba-go |
| `--mysql-user` | string | MySQL 账号 | 新增 |
| `--mysql-pass` | string | MySQL 密码（明文，仅调试） | 新增 |
| `--mysql-defaults-file` | string | 替代默认 my.cnf 搜索 | 新增 |
| `--mysql-defaults-group` | string | my.cnf 段名，默认 `client` | 新增 |
| `--mysql-timeout` | duration | SQL/连接超时，默认 1s | 新增 |
| `--mysql-tls` | flag | 启用 TLS | 新增 |
| `--tps-mode` | enum | `iud`(默认)/`commit` | 新增 |
| `--header-period` | int | 表头重打周期，默认 15 | 新增 |
| `-L,--logfile` | string | 日志文件 | Perl |
| `-logfile_by_day` | flag | 按天切分，后缀 `.yyyy-mm-dd` | Perl |

**约束**：
- 无参数 → 打印 usage 退出；
- `-L` 隐含 `nocolor`；
- `-d`/`-n` 设备名在 `/proc` 中找不到 → 启动报错退出；
- 默认 `interval=1`、`port=3306`；
- 用 `pflag`（标准库 `flag` 不支持 `-sys` 风格）。

---

## 7. 关键实现要点与公式对照

> 本章给出从 Perl 行为到 Go 实现的关键映射，并显式标注 orzdba-go 的对应 bug 以避免。

### 7.1 `/proc` 解析
- 全部用 Go 直接读，`bufio.Scanner` 逐行扫，`strings.Fields` 切分（等价 Perl `split(/\s+/)`）；
- `/proc/net/dev` 行含 `:`，先按 `:` 切再按空白切（等价 Perl `split(/\s+|:/)`）；
- `/proc/diskstats` 设备匹配用字段精确匹配（field[2] 或 field[1]）；
- **禁用**：`cat`、`grep`、`sed`、`awk`、`ifconfig`（orzdba-go P0-2、P2-20）。

### 7.2 数值类型与舍入
- 计数器 `uint64`；差值 `int64`；百分比/速率 `float64`；
- Perl `int(x + 0.5)` → Go `math.Round(x)` 后转 `int`（orzdba-go P1-9 用整数除法丢精度）；
- `printf "%3d"` 等格式串可直接复用。

### 7.3 颜色
- ANSI 转义用 `uint8` 常量索引 byte 切片，避免字符串 switch（orzdba-go P2）；
- `nocolor` 时 `Colorize` 直接返回原文本，**零 ANSI 字节**（orzdba-go P1-13 仍输出 `\033[;22m`）。

### 7.4 字节自动格式化（k/m/g）
```go
func FormatBytesRate(bps float64) string {
    if bps/1024/1024/1024 >= 1 { return fmt.Sprintf("%5.1fg", bps/1024/1024/1024) }
    if bps/1024/1024 >= 1      { return fmt.Sprintf("%6.1fm", bps/1024/1024) }
    if bps/1024 >= 1           { return fmt.Sprintf("%5dk", int(bps/1024+0.5)) }
    return fmt.Sprintf("%5d", int(bps))
}
```
注意：保持列宽一致（靠 `printf` 宽度对齐）。

### 7.5 CPU 公式（严格按 Perl）
```
total = sum(field1..field7)  // user nice system idle iowait irq softirq
user_diff   = (user2+nice2)   - (user1+nice1)            // orzdba-go 漏了 nice
system_diff = (sys2+irq2+softirq2) - (sys1+irq1+softirq1) // orzdba-go 漏了 irq/softirq
idle_diff   = idle2 - idle1
iowait_diff = iowait2 - iowait1
usr%  = round(user_diff   / (total2-total1) * 100)
sys%  = round(system_diff / (total2-total1) * 100)
idl%  = round(idle_diff   / (total2-total1) * 100)
iow%  = round(iowait_diff / (total2-total1) * 100)
```
阈值：`usr>10` 红；`sys>10` 红；`iow>10` 红。

### 7.6 间隔与 diff（修复 orzdba-go P1-7、P1-8）
- 主循环：`time.Sleep(time.Duration(interval) * time.Second)`（不硬编码 1s）；
- diff 用真实 elapsed：`elapsed := time.Since(prev).Seconds(); rate := float64(delta) / elapsed`；
- swap/net/disk 的 diff 计算不依赖 `interval == "1"` 判断；
- **禁用** `/0.99`、`/0.9999`、`/10` 等 magic number（orzdba-go P1-8）。

### 7.7 Disk 公式（严格按 Perl，修复 orzdba-go P1-8）
```
deltams = 1000.0 * (user_diff + system_diff + idle_diff + iowait_diff) / ncpu / HZ
n_ios    = rd_ios + wr_ios            (delta)
n_ticks  = rd_ticks + wr_ticks        (delta)
n_kbytes = (rd_sectors + wr_sectors) / 2.0  (delta)
queue    = aveq / deltams
wait     = n_ios ? n_ticks / n_ios : 0
svc_t    = n_ios ? ticks / n_ios : 0       // ticks = field[13] delta
busy     = 100.0 * ticks / deltams         // 截断 100
rkbs     = 1000.0 * rd_sectors / deltams / 2
wkbs     = 1000.0 * wr_sectors / deltams / 2
rd_ios_s = 1000.0 * rd_ios / deltams
wr_ios_s = 1000.0 * wr_ios / deltams
```
阈值：`rkbs>1024` 红；`wkbs>1024` 红；`wait>5` 红；`svc_t>5` 红；`busy>80` 红。
**禁用**：`ticks/10` 的简化公式（orzdba-go P1-8）。

### 7.8 innodb_status 文本解析
- `SHOW ENGINE INNODB STATUS` 原生协议返回**真实换行**（Perl 经 mysql 客户端拿到 `\n` 字面量）；
- Go 版按 `\n` 分割（不需要反转义）；
- 行匹配用 `strings.Contains` + `strings.Fields`；
- 解析失败的字段填 0，不中断输出。

### 7.9 MySQL 一次性拉取
- 每 tick 只发**一次** `SHOW GLOBAL STATUS WHERE Variable_name IN (...)`，结果塞 map；
- 各子模块从 map 取值，不再各自查（避免 orzdba-go 多次 shell mysql）；
- `SHOW ENGINE INNODB STATUS` 仍单独一次（仅 `-innodb_status` 时执行）。

### 7.10 组合参数展开时机
- **在 first 采样前**完成展开（orzdba-go P1-10 顺序错误）；
- 展开后的 `info` 才传给 `createCommand(info, 0)`。

### 7.11 表头重打周期
- 默认每 15 行重打（Perl 语义；orzdba-go 用 20）；
- `--header-period` 可配；
- 按天切分时跨天立即重打标题与表头。

### 7.12 首次采样输出 0
- 所有差分类指标首次输出 0（与 Perl 一致）；
- `statusMap` 初始化为全 0 的 `mystat1`。

### 7.13 `-C` 与按天切分交互
- 跨天时 `count -= mycount; mycount = 0`（与 Perl 一致）。

---

## 8. MySQL 凭证解析策略

### 8.1 凭证来源与优先级（从高到低）

1. **CLI**：`--mysql-user`、`--mysql-pass`（明文，仅调试；启动后建议 `procfs` 权限 0500 隔离 `/proc/<pid>/cmdline`）
2. **环境变量**：`ORZDBA_MYSQL_USER`、`ORZDBA_MYSQL_PASS`
3. **显式 defaults-file**：`--mysql-defaults-file=<path>` + `--mysql-defaults-group=<section>`（默认 `client`）；指定后不再走默认搜索
4. **默认 my.cnf 搜索**（仅当未指定 `--mysql-defaults-file` 时），按 mysql 客户端兼容顺序取首个存在者：
   - `/etc/my.cnf`、`/etc/mysql/my.cnf`、`~/.my.cnf`
   - 段名由 `--mysql-defaults-group` 指定，默认 `client`
5. **编译期注入**：`go build -ldflags "-X main.defaultMySQLUser=... -X main.defaultMySQLPass=..."`
6. **无凭证兜底**：以当前 OS 用户走 unix socket peer auth（兼容原 Perl 在 OS root 下的无密码场景）

字段级合并：高优先级来源命中某字段后，低优先级来源**不再覆盖该字段**；`user` 与 `password` 可分别来自不同来源。`socket` 优先于 `host:port`。

### 8.2 my.cnf 解析器（自实现，~120 行，不引第三方）

支持特性：
- 段头 `[section]`、`key=value`/`key = value`、布尔键（忽略）、注释 `#`/`;`；
- `!include <path>` 与 `!includedir <dir>`（递归深度上限 8、文件数上限 32、单文件 100ms 上限）；
- 引号值剥引号；
- 仅返回指定 group 的 `user/password/host/port/socket`，其它键忽略；
- 文件权限检查：mode 对 group/other 可读时 stderr 警告。

接口：
```go
type CNFSource struct {
    User, Password, Host, Socket string
    Port                         int
    Found                        bool
}
func ParseMySQLDefaults(filePath, group string) (*CNFSource, error)
```

### 8.3 编译期注入的安全边界
- 编译期密码通过 `-ldflags -X` 注入到 `main` 包变量，运行时存在二进制中；
- **风险**：`strings <binary>` 可提取；任何能读取二进制的用户都能获取密码；
- **适用**：受控主机、二进制 `0500` 且属主为运行用户；
- **不适用**：多租户机器、CI 产物分发；改用 my.cnf 或环境变量；
- 启动时若编译期密码非空且二进制 mode 对 group/other 可读，stderr 警告。

### 8.4 凭证安全（运行期）
- **不打印**：日志/stderr 不出现 password；DSN 仅显 `user@socket:port`，密码段 `***`；
- **不外泄到子进程**：tcprstat 命令行不含密码；MySQL 凭证不通过环境变量传给 tcprstat；
- **不持久化**：不写任何含凭证的文件；
- **my.cnf 权限告警**：读取前检查 mode；
- **socket 优先**：同时配置 `host` 与 `socket` 时优先 socket；
- **进程隔离**：启动后 `umask 077`；日志 `0600`。

### 8.5 默认 socket 自动探测（修复 orzdba-go P2-21）
`-S` 未指定且 my.cnf 未配置时，按顺序探测：
1. `/tmp/mysql.sock`
2. `/var/lib/mysql/mysql.sock`
3. `/var/run/mysqld/mysqld.sock`
4. `/run/mysqld/mysqld.sock`

首个存在者使用；都无则回退到 TCP `127.0.0.1:3306`。

---

## 9. 性能与安全预算（硬约束）

> 本章是**可验收的硬约束**，不是建议。重写后的工具必须满足下列全部指标；任一不达标视为未完成。直接针对 orzdba-go 的失败案例收紧。

### 9.1 性能预算

| 指标 | 上限 | 测量方式 |
|---|---|---|
| 单 tick 采集+渲染 CPU 时间 P99 | ≤ 5 ms | `runtime` 自埋点 + pprof |
| 常驻 RSS | ≤ 30 MB | `/proc/self/status` VmRSS |
| 每 tick fork/exec 次数 | 0 | strace 计数（tcprstat 仅启动一次，不计入 tick） |
| 每 tick MySQL 查询数 | ≤ 2 | 1 次 `SHOW GLOBAL STATUS` + 可选 1 次 `SHOW ENGINE INNODB STATUS` |
| 每 tick MySQL 网络往返 | ≤ 2 | 同上 |
| 每 tick 打开的 `/proc` 文件数 | ≤ 6 | ltrace/inotify |
| 每 tick Go heap 分配 | ≤ 64 KB | `runtime.ReadMemStats` |
| goroutine 数 | ≤ 3 | 主循环 1 + tcprstat wait 1 + 信号 1；禁止 fan-out |
| 启动时间（到首行输出） | ≤ 200 ms | 不含 MySQL 连接建立 |
| 日志文件 open/close 频率 | 1 次/文件生命周期 | 不每 tick open/close（orzdba-go P2） |

与 orzdba-go 对比的预期收益：
- orzdba-go 每 tick 6+ 次 fork（bash + cat/grep/sed/awk/xargs/mysql）+ 多次 mysql 客户端连接建立 → 数十 ms CPU + RSS 抖动 + 密码泄露；
- Go 版每 tick 0 fork、1 次复用连接的 SQL 查询、5~6 次小文件读 → CPU P99 < 5ms，稳态 RSS < 30MB。

### 9.2 采集节奏的自我保护
- **固定间隔，禁止自适应加速**：interval 最低 1 秒；
- **退避重连**：MySQL 连接失败按 1s→2s→5s 指数退避，上限 5s；
- **查询超时**：每条 SQL 带 `context.WithTimeout(ctx, --mysql-timeout)`，超时即取消；
- **慢查询保护**：单次 `SHOW GLOBAL STATUS` 超过 500ms，本 tick 跳过 MySQL 列输出（输出 0）并记 stderr 警告；
- **采集失败不级联**：单模块失败只影响该列，不重试、不补采；
- **不并发采集**：系统侧与 MySQL 侧串行（系统侧总耗时 < 1ms，并发收益可忽略）。

### 9.3 MySQL 安全
| 项 | 规则 |
|---|---|
| 凭证来源 | 见 §8；按优先级解析，**不默认 root** |
| 推荐权限 | `USAGE + PROCESS + REPLICATION CLIENT`（`SHOW ENGINE INNODB STATUS` 需 PROCESS；`SHOW SLAVE STATUS` 需 REPLICATION CLIENT） |
| **凭证不入进程列表** | **禁止** `exec.Command("mysql", "-u...", "-p...")`；用 `database/sql` 原生协议（直接修复 orzdba-go P0-1） |
| 凭证脱敏 | stderr 仅显 `user@socket:port`，密码段 `***` |
| 连接模式 | 单连接长连，不用连接池；异常关闭后重建 |
| 传输 | 优先 unix socket |
| 语句白名单 | 仅 `SHOW GLOBAL STATUS`、`SHOW VARIABLES`、`SHOW ENGINE INNODB STATUS`、`SHOW DATABASES`、`SHOW SLAVE STATUS`；禁止 DML/DDL/LOCK；驱动层不启用多语句 |
| 查询超时 | 见 §9.2 |
| 连接超时 | 启动期 `DialContext` 超时 2s；运行期 SQL 超时 1s |
| TLS | `--mysql-tls` 启用时不允许 fallback 跳过校验 |

### 9.4 系统侧安全
- **只读**：不写任何系统文件、不 `sysctl`、不 `setrlimit`、不修改 `/proc`；
- **不调 shell**：`cat/grep/sed/tr/tail/awk/hostname/ifconfig` 全部用 Go 标准库；`os/exec` 仅用于 tcprstat，路径硬编码 `/usr/bin/tcprstat`（防 PATH 注入）；
- **文件句柄**：`/proc/*` 每次 open-read-close；
- **缓冲区复用**：`bufio.Scanner` buffer 与字段切片复用，避免每 tick 分配。

### 9.5 tcprstat 子进程安全
- **不抓包**：依赖 tcprstat 的 setuid 配置（文档说明，不自动 setuid）；
- **命令路径硬编码** + **参数白名单**：`exec.Command("/usr/bin/tcprstat", ...)`；不接受外部输入注入参数；
- **stdout 重定向**到 `/tmp/orzdba_tcprstat.<pid>.log`，权限 `0600`；
- **进程锁**：`O_CREAT|O_EXCL` 创建 `.lck`；
- **崩溃检测**：每 tick `cmd.Process.Signal(0)` 探活；崩溃重启一次，连续 2 次崩溃放弃（输出 0）；
- **退出清理**：`SIGINT/SIGTERM` 时 `SIGTERM` 子进程，等 200ms 后 `SIGKILL` 兜底，删 log/lck；
- **不读 stderr**：tcprstat 的 stderr 丢弃；
- **禁止 `killall tcprstat`**（直接修复 orzdba-go P0-5）。

### 9.6 日志与输出安全
- 日志文件默认权限 `0600`；创建后 `os.Chmod` 兜底（直接修复 orzdba-go P0-4）；
- **不写密码**：日志不出现连接串、密码、SQL 全文；
- **不监听端口**：不开任何 HTTP/TCP 监听；
- **不写 `/tmp` 之外临时文件**：tcprstat 的 log/lck 限定 `/tmp`（或 `--rt-tmpdir`）；
- **数据日志不用 `log` 包**：直接 `lf.Write([]byte)`，避免 `log.New` flags 误用（直接修复 orzdba-go P0-6）。

### 9.7 资源边界与故障行为矩阵

| 故障 | 行为 |
|---|---|
| MySQL 宕机 | 系统侧照常；MySQL 列输出 0；退避重连（1→2→5s） |
| MySQL 慢（>500ms） | 跳过本 tick MySQL 列；记 stderr 警告；不重试 |
| MySQL 拒绝连接 | 同"宕机" |
| `/proc` 读失败 | 该模块本 tick 输出 0；不重试；下 tick 自动恢复 |
| tcprstat 崩溃 | 重启一次；再崩则输出 0；不循环重启 |
| 用户 Ctrl-C | 清理本实例的 tcprstat 子进程与残留文件后退出 |
| `count` 达到 | 同上清理后退出 |
| OOM | RSS ≤ 30MB，自身不是压力源；若被 oom-kill，退出码非 0 |

### 9.8 验收（性能与安全专项）
1. 启动 `orzdba -mysql -i 1` 持续 10 分钟，`pidstat -p <pid> 1` 监控：CPU 均值 < 0.5%、RSS 稳态 < 30MB；
2. 同期 MySQL 侧 `SHOW PROCESSLIST` 不应出现本工具多于 1 条连接、不应出现锁等待；
3. `strace -c -p <pid>` 60s：`fork/clone/execve` 计数为 0（tcprstat 启动后）；
4. `ps -ef | grep mysql` 不应出现本工具调用 `mysql` 客户端（直接验证 orzdba-go P0-1 已修复）；
5. `ps -ef | grep <pid>` 不应出现密码（验证 P0-1）；
6. `kill -9` tcprstat 子进程，本工具应重启一次后继续输出，不应僵死；
7. `strings <binary> | grep -i pass` 应无密码字面量（除非编译期注入且文档已警告）；
8. `cat /proc/<pid>/environ`（同用户）不应泄露密码到其他用户；
9. `nc -l 12345 &` 占用任意端口，本工具不应尝试连接任何非 MySQL 端口（验证不开监听、不外联）；
10. `ls -l <logfile>` 权限应为 `-rw-------`（验证 P0-4 已修复）；
11. `ls -l /tmp/orzdba_tcprstat.*` 应只有本实例的文件，且权限 `0600`；
12. 多实例并跑，`Ctrl-C` 一个实例不应影响另一个实例的 tcprstat（验证 P0-5 已修复）。

---

## 10. 依赖选型

| 用途 | 选型 | 理由 |
|---|---|---|
| CLI 解析 | `github.com/spf13/pflag` | 支持 `-sys` 风格长选项；标准库 `flag` 不支持 |
| MySQL 驱动 | `github.com/go-sql-driver/mysql` | 事实标准，纯 Go，无 cgo；替代 orzdba-go 的 `mysql` 客户端 shell 调用 |
| my.cnf 解析 | **自实现**（~120 行） | 不引入第三方 INI 库，缩小凭证处理攻击面 |
| 日志 | 标准库 `log`（仅 stderr） | 数据日志直接 `Write`，避免 orzdba-go P0-6 |
| 测试 | `testing` + `github.com/stretchr/testify/assert` | 减少样板 |
| 字节格式化、ANSI | 标准库 | 无需第三方 |
| 时间 | 标准库 `time` | 替代 POSIX `strftime` |

**不引入**：`cobra`（无子命令）、`zap`/`zerolog`、`prometheus/client_golang`、`gopkg.in/ini.v1`。

**禁用**（orzdba-go 用过的）：`os/exec` 调用 `bash/cat/grep/sed/awk/xargs/ifconfig/hostname/mysql/killall`。

---

## 11. 错误处理与信号

### 11.1 致命错误（启动期）
- 无参数 → 打印 usage 退出；
- `-d`/`-n` 设备在 `/proc` 中找不到 → 报错退出；
- 启用 MySQL 指标但连接失败 → 报错退出；
- `-rt` 启用但 `/usr/bin/tcprstat` 不存在 → 报错退出；
- `-L` 路径不可写 → 报错退出。

### 11.2 可恢复错误（运行期）
- 某 tick `/proc` 读失败 → 跳过该模块输出，下 tick 重试；
- MySQL 查询失败 → 该 tick MySQL 列输出 0，下 tick 自动重连；
- tcprstat 日志暂无可读行 → 输出 `0 0 0 0`。

### 11.3 信号
- `SIGINT`：rtcol 清理 log/lck + 杀本实例 tcprstat 子进程，打印 `Exit Now...` 后 `os.Exit(0)`；
- `SIGTERM`：同上（便于 systemd 停止）。

---

## 12. 测试策略

### 12.1 单元测试
- `internal/syscol/*`：每个解析函数接受 `io.Reader`，固化 `/proc` 样本断言字段；
- `internal/mycol/*`：固化 `SHOW GLOBAL STATUS` 输出断言差值/速率；
- `internal/mycol/innodb_status`：真实 `SHOW ENGINE INNODB STATUS` 样本（脱敏后存 `testdata/`）；
- `internal/mycol/slave`、`semi`：固化 `SHOW SLAVE STATUS` 样本；
- `internal/mysqlc/defaults`：my.cnf 解析含 `!include`/`!includedir`/引号/注释的边界样本；
- `internal/render`：断言表头拼接、列对齐、颜色开关、按天切分触发；
- `internal/logsink`：临时目录测试轮转与权限 0600。

### 12.2 行为对齐测试（黄金样本）
- 准备一组 `/proc` 样本 + MySQL status 样本；
- 分别用原 Perl 工具（Linux 容器）与 Go 版本产出 N 行；
- **去色后逐行比对**，要求一致；
- 允许的差异：MySQL 变量展示顺序（用 `ORDER BY` 对齐）。

### 12.3 集成测试
- Docker Linux 容器 + `mysql:8.0`：跑 `./orzdba -mysql -C 3` 验证连通；
- 跑 `./orzdba -sys -d sda -n eth0 -C 3` 验证系统侧；
- 跑 `./orzdba -lazy -L /tmp/x.log -logfile_by_day -C 3` 验证日志切分；
- 跑 `./orzdba -slave -semi -C 3` 验证扩展模块。

### 12.4 性能基线
- 单 tick 采集+渲染 P99 < 5ms；
- 常驻 RSS < 30MB；
- 与 orzdba-go 对比：相同 interval 下 CPU 占用降低 ≥ 90%（因去掉 6+ 次 fork/tick）。

### 12.5 orzdba-go bug 回归测试
对 §2.4 的每个 P0/P1 bug 编写反例测试，确保重写版不重现：
- 密码不入 `ps -ef`；
- `-i 5` 真正按 5 秒采样；
- `-mysql` 启用时 first 采样已包含 Com_* 字段；
- `nocolor` 输出零 ANSI 字节；
- `util_disk` 阈值分支可达；
- 单实例退出不影响其他实例的 tcprstat。

---

## 13. 里程碑与阶段划分

### M0 — 仓库初始化（0.5 人日）
- 在 [x777777x/orzdba](https://github.com/x777777x/orzdba) 仓库中 `git init`；
- `go mod init`、`pflag`/`go-sql-driver/mysql` 依赖；
- 目录结构按附录 A；
- CI（lint + test）骨架。

### M1 — 骨架与 CLI（0.5 人日）
- `pflag` 参数解析、组合参数展开（first 采样前）、`print_title` 非数据库部分；
- 主循环空跑、`-C`、`-t`；
- 自定义 usage 含 sample；
- 交付：`./orzdba -t -C 3` 打印时间行。

### M2 — 系统采集器（1.5 人日）
- `load`、`cpu`、`swap`、`net`、`disk` + 单测；
- 表头拼接、颜色、按 15 行重打；
- 交付：`./orzdba -sys -d sda -n eth0 -C 5` 与原 Perl 逐行一致（去色比对）。

### M3 — MySQL 连接与凭证（1.5 人日）
- `mysqlc/conn.go` 长连接 + 重连 + context 超时；
- `mysqlc/defaults.go` my.cnf 解析 + 凭证优先级合并 + socket 自动探测；
- `SHOW VARIABLES`、`SHOW DATABASES` 用于 `print_title`；
- 交付：`./orzdba -t -C 3`（连接成功，无密码泄露）。

### M4 — MySQL 采集器（2 人日）
- `com`、`hit`、`threads`、`bytes`、`innodb_rows`、`innodb_pages`、`innodb_data`、`innodb_log`；
- 每 tick 单次 `SHOW GLOBAL STATUS` 拉取共享；
- 交付：`./orzdba -mysql -C 5` 输出对齐。

### M5 — Innodb Status 解析（1 人日）
- `SHOW ENGINE INNODB STATUS` 文本解析；
- `testdata/mysql/innodb_status.txt` 固化样本 + 单测；
- 交付：`./orzdba -innodb_status -C 5` 输出对齐。

### M6 — 扩展模块（1 人日）
- `slave`、`semi` 子模块；
- 扩展 hit% 5 列（`-hit full`）；
- Thread cache hit%；
- 交付：`./orzdba -slave -semi -hit full -C 5`。

### M7 — RT 采集器（1 人日）
- tcprstat 子进程管理、PID 跟踪、日志读取、进程锁；
- `SIGINT` 精准清理（不 `killall`）；
- 交付：`./orzdba -rt -C 5`（需 tcprstat 环境）。

### M8 — 日志与轮转（0.5 人日）
- `logsink` 的 stdout/file/按天切分三种实现；
- 文件权限 0600 + `os.Chmod` 兜底；
- 交付：`./orzdba -lazy -L /tmp/x.log -logfile_by_day -C 5` 行为对齐。

### M9 — 行为对齐与 orzdba-go bug 回归（1.5 人日）
- 黄金样本比对测试；
- §12.5 回归测试；
- `README`、`--help` 文案；
- 交付：可在 Linux 容器内替代原 Perl 工具与 orzdba-go。

合计约 **10 人日**（不含代码评审与返工）。

---

## 14. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| `SHOW ENGINE INNODB STATUS` 原生协议返回真实换行，与 Perl 经 mysql 客户端转义后不同 | 解析逻辑需调整 | §7.8 已识别，按 `\n` 分割；真实样本单测 |
| `printf` 列宽与 Go 格式串的细微差异 | 输出对不齐 | 黄金样本测试兜底 |
| tcprstat 需要 root 或 setuid | `-rt` 在非 root 下不可用 | 文档说明；与 Perl 一致，不额外处理 |
| 不同内核 `/proc/diskstats` 字段差异 | disk 列计算偏差 | 限定支持内核 2.6+；文档说明 |
| MySQL 8.x 移除/重命名部分 status 变量 | 部分列恒为 0 | 文档说明；`semi` 在无插件时优雅降级 |
| `pflag` 对 `-sys` 单划线长选项的解析 | 解析失败 | M1 冒烟测试；不支持则自定义解析器 |
| my.cnf `!include` 循环引用 | 解析器死循环 | 深度上限 8 + 文件数上限 32 |
| 黄金样本测试需原 Perl 可运行 | CI 复杂 | Docker 镜像固化 Perl + 依赖；或预录样本 |
| orzdba-go 的 `-lazy`/`-mysql` 默认 hit 为 5 列 | 用户预期偏差 | 默认 1 列兼容 Perl；`-hit full` 显式开启 |

---

## 15. 验收标准

1. **功能对齐**：对以下 10 组参数组合，去色后输出与原 Perl 工具逐行一致（在相同 Linux + MySQL 环境下）：
   - `-sys`
   - `-sys -d sda -n eth0`
   - `-mysql`
   - `-innodb`
   - `-lazy`
   - `-mysql -innodb_rows -innodb_status -rt`
   - `-sys -mysql -d sda -n eth0 -C 10`
   - `-lazy -L /tmp/orzdba.log -logfile_by_day -C 10`
   - `-slave -semi -C 5`（无原 Perl 对照，按 orzdba-go 行为对齐）
   - `-hit full -T -C 5`（同上）
2. **性能与安全**：§9.8 全部通过；
3. **无外部 Perl 依赖**：单一静态二进制；目标机无需 Perl、CPAN、`mysql` 客户端、`ifconfig`；
4. **信号行为**：`Ctrl-C` 后 `/tmp` 下**无本实例的** `orzdba_tcprstat.*` 残留，且不影响其他实例；
5. **错误退出**：所有 §11.1 致命错误给出明确错误信息，退出码非 0；
6. **测试覆盖**：`internal/syscol`、`internal/mycol`、`internal/mysqlc` 单测覆盖率 ≥ 80%；
7. **凭证安全**：`ps -ef` 不见密码；`strings` 不见密码（除非编译期注入且文档已警告）。

---

## 16. 后续演进方向（本期不做，仅记录）

- 用 `gopacket` 原生实现 tcprstat 等价功能；
- 增加 JSON / Prometheus / OpenMetrics 输出后端；
- 增加 HTTP 远程采集模式（中心节点拉取多机指标）；
- 支持 macOS / Windows 等价系统指标（通过 `gopsutil`）；
- 配置文件支持（替代长命令行）；
- 容器化场景下从 cgroup 读取 CPU/内存上限，修正 `ncpu` 口径；
- 多实例聚合同一 display（如 `--aggregate host1,host2`）。

---

## 附录 A：项目目录结构

新仓库根目录：[x777777x/orzdba](https://github.com/x777777x/orzdba)（`git@github.com:x777777x/orzdba.git`）

```
orzdba/
├── cmd/
│   └── orzdba/
│       └── main.go
├── internal/
│   ├── metric/
│   │   └── types.go            # Row, SysSample, MySample, RTSample
│   ├── syscol/
│   │   ├── collector.go        # Collector 接口
│   │   ├── load.go
│   │   ├── cpu.go
│   │   ├── swap.go
│   │   ├── net.go
│   │   └── disk.go
│   ├── mycol/
│   │   ├── collector.go
│   │   ├── com.go
│   │   ├── hit.go              # 含 full 模式（5 列）
│   │   ├── innodb_rows.go
│   │   ├── innodb_pages.go
│   │   ├── innodb_data.go
│   │   ├── innodb_log.go
│   │   ├── innodb_status.go
│   │   ├── threads.go          # 含 thread cache hit%
│   │   ├── bytes.go
│   │   ├── slave.go            # 扩展自 orzdba-go
│   │   └── semi.go             # 扩展自 orzdba-go
│   ├── rtcol/
│   │   └── tcprstat.go
│   ├── mysqlc/
│   │   ├── conn.go             # 长连接 + 重连 + 查询封装
│   │   └── defaults.go         # my.cnf 解析 + 凭证优先级 + socket 探测
│   ├── render/
│   │   ├── ansi.go             # uint8 常量 + Colorize
│   │   ├── format.go           # 字节/百分比格式化
│   │   └── render.go           # 表头/行拼接
│   └── logsink/
│       ├── sink.go             # 接口
│       ├── stdout.go
│       ├── file.go             # 0600 + Chmod 兜底
│       └── dailyfile.go
├── testdata/
│   ├── proc/                   # /proc 样本
│   ├── mysql/                  # SHOW STATUS / ENGINE INNODB STATUS / SLAVE STATUS 样本
│   └── mycnf/                  # my.cnf 解析样本（含 !include 边界）
├── go.mod
├── go.sum
└── README.md
```

## 附录 B：原 Perl 变量查询清单（直接迁移）

**SHOW GLOBAL STATUS 变量集**（一次性查询）：
```
Com_select, Com_insert, Com_update, Com_delete, Com_commit, Com_rollback,
Innodb_buffer_pool_read_requests, Innodb_buffer_pool_reads,
Innodb_rows_inserted, Innodb_rows_updated, Innodb_rows_deleted, Innodb_rows_read,
Threads_running, Threads_connected, Threads_cached, Threads_created,
Bytes_received, Bytes_sent,
Innodb_buffer_pool_pages_data, Innodb_buffer_pool_pages_free,
Innodb_buffer_pool_pages_dirty, Innodb_buffer_pool_pages_flushed,
Innodb_data_reads, Innodb_data_writes, Innodb_data_read, Innodb_data_written,
Innodb_os_log_fsyncs, Innodb_os_log_written,
Connections, Qcache_hits,
Handler_read_first, Handler_read_key, Handler_read_next, Handler_read_prev,
Handler_read_rnd, Handler_read_rnd_next,
Created_tmp_tables, Created_tmp_disk_tables,
Key_read_requests, Key_reads, Key_write_requests, Key_writes,
Max_used_connections, Opened_tables, Slow_queries,
Select_scan, Select_full_join, Binlog_cache_disk_use, Binlog_cache_use,
Aborted_connects, Aborted_clients
```
（前 26 个来自 Perl 原版；后 22 个吸收自 orzdba-go 用于扩展 hit% 与 title 展示）

**SHOW VARIABLES 第一组**：
```
sync_binlog, max_connections, max_user_connections, max_connect_errors,
table_open_cache, table_definition_cache, thread_cache_size, binlog_format,
open_files_limit, max_binlog_size, max_binlog_cache_size
```

**SHOW VARIABLES 第二组**：
```
innodb_flush_log_at_trx_commit, innodb_flush_method, innodb_buffer_pool_size,
innodb_max_dirty_pages_pct, innodb_log_buffer_size, innodb_log_file_size,
innodb_log_files_in_group, innodb_thread_concurrency, innodb_file_per_table,
innodb_adaptive_hash_index, innodb_open_files, innodb_io_capacity,
innodb_read_io_threads, innodb_write_io_threads, innodb_adaptive_flushing,
innodb_lock_wait_timeout
```

**按 G/M 换算的变量**：`innodb_buffer_pool_size`、`innodb_log_file_size`、`innodb_log_buffer_size`、`max_binlog_cache_size`、`max_binlog_size`。

## 附录 C：orzdba-go 问题清单（带行号速查）

| ID | 严重度 | 行号 | 问题 |
|----|---|---|---|
| P0-1 | 安全 | 406,411,443,450,457,498,567,603,680,702 | MySQL 密码在 `mysql -u<user> -p<pass>` 命令行，泄露给所有用户 |
| P0-2 | 性能 | 371-383, 542, 566, 590, 603, 618, 680, 702 | 全程 `bash -c <pipeline>`，每 tick 6+ 次 fork |
| P0-3 | 安全 | 254 | 默认密码 `system` 硬编码 |
| P0-4 | 安全 | 1667 | 日志文件权限 0606（world-writable） |
| P0-5 | 安全 | 1690, 1784 | `killall tcprstat` 杀全部实例 |
| P0-6 | 正确性 | 1672 | `log.New` 第三参数误用 `os.O_APPEND` |
| P1-7 | 正确性 | 1768-1784 | `time.Sleep(time.Second)` 硬编码，`-i` 失效 |
| P1-8 | 正确性 | 1116,1147,1154,1168 | `/0.99`、`/0.9999`、`/1.9999`、`/10` magic number |
| P1-9 | 正确性 | 1045-1048 | CPU usr/sys 公式漏 nice/irq/softirq，整数除法丢精度 |
| P1-10 | 正确性 | 1759-1757 | 组合参数展开在 first 采样之后，首行 diff 爆炸 |
| P1-11 | 正确性 | 1214-1220 | `util_disk` 阈值分支不可达 |
| P1-12 | 正确性 | 1063,1172,1178 | `if 1 != 1` 死代码 |
| P1-13 | 正确性 | 1697-1717 | `nocolor` 仍输出 ANSI |
| P2-14 | 工程 | 1679 | `runtime.GOMAXPROCS` 多余 |
| P2-15 | 工程 | 全文 | `strconv.Atoi(..., _)` 忽略错误 |
| P2-16 | 工程 | 812,851 | `panic` 处理类型断言 |
| P2-17 | 工程 | 152,155,156... | `basic` 字段类型不一致 |
| P2-18 | 工程 | 318-320 | `flag.NArg` 半成品死代码 |
| P2-19 | 正确性 | 907 | `hit()` `<0.01` 分支不可达 |
| P2-20 | 兼容 | 590 | 用 `ifconfig` 取网卡，依赖缺失 |
| P2-21 | 兼容 | 257 | 默认 socket `/tmp/mysql.sock` 不通用 |
| P2-22 | 工程 | - | 无单元测试 |
| P2-23 | 体验 | - | 无自定义 `-h` |

---

（完）
