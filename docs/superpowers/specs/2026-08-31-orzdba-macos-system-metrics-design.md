# orzdba macOS 系统指标支持设计

> 文档版本：v1.0
> 编写日期：2026-08-31
> 状态：已批准（2026-08-31）

## 1. 背景与目标

当前 `internal/syscol` 包的所有采集器（load/cpu/mem/swap/net/disk）都直接硬编码读取 Linux 的 `/proc` 文件。macOS 没有 `/proc`，因此在 macOS 上编译执行时全部系统指标都输出 0（仅 `time` 列有值）。

**目标**：macOS 上系统指标正确采集，不再全 0；Linux 行为完全不变。

**非目标**：
- 不改动 Linux 采集路径（`/proc`）与输出格式
- 不做接口抽象重构（保持现有 struct + `Name()`/`Headline()`/`Collect()` 约定）
- 不扩展 GitHub Actions CI 到 macOS runner（本机 go test + 手动验证即可）
- 不替换 tcprstat（`-rt` 仍仅 Linux）

## 2. 约束

继承 go-rewrite-plan.md 的硬约束：
- **零外部命令调用**：禁止 `ifconfig`/`netstat`/`iostat`/`sysctl`(shell) 等；macOS 用 cgo 原生 API（`syscall.Sysctl`、`host_statistics`、`getifaddrs`、IOKit）
- **单一静态二进制**：macOS 构建不引入外部依赖，cgo 链接系统框架（`IOKit.framework`）
- **输出格式统一**：macOS 复用 Linux 的列布局与渲染器，不新增渲染逻辑

## 3. 架构

### 3.1 文件组织

用 Go build tag（`//go:build darwin`）隔离。每个采集器一个 `_darwin.go` 文件，与现有 Linux 文件同名 struct + 同接口，同一时刻只有一个平台版本被编译。

```
internal/syscol/
  load.go          (Linux: /proc/loadavg)       ← 现有，不动
  load_darwin.go   (macOS: sysctl vm.loadavg)   ← 新增
  cpu.go           (Linux: /proc/stat)          ← 现有，不动
  cpu_darwin.go    (macOS: host_cpu_load_info)  ← 新增
  mem.go           (Linux: /proc/meminfo)       ← 现有，不动
  mem_darwin.go    (macOS: hw.memsize + host_statistics64) ← 新增
  net.go           (Linux: /proc/net/dev)       ← 现有，不动
  net_darwin.go    (macOS: getifaddrs)          ← 新增
  disk.go          (Linux: /proc/diskstats)     ← 现有，不动
  disk_darwin.go   (macOS: IOKit)               ← 新增
  swap.go          (Linux: /proc/vmstat)        ← 现有，不动
  swap_darwin.go   (macOS: sysctl vm.swapusage) ← 新增
  platform_linux.go  (现有 checkNetDevice/checkDiskDevices/detectCPU 逻辑搬入) ← 新增
  platform_darwin.go (macOS 版平台 helper)      ← 新增
```

### 3.2 平台 helper

`cmd/orzdba/main.go` 中与平台相关的三处逻辑拆为平台函数，main.go 只调用平台 helper，Linux 行为不变：

| 函数 | Linux（platform_linux.go） | macOS（platform_darwin.go） |
|------|---------------------------|------------------------------|
| `platformDetectCPU() int` | 读 `/proc/cpuinfo`，fallback `runtime.NumCPU()` | `sysctl hw.ncpu` |
| `platformCheckNetDevice(dev) error` | 读 `/proc/net/dev` 校验 | `getifaddrs` 校验接口存在 |
| `platformCheckDiskDevices(devs) error` | 读 `/proc/diskstats` 校验 | IOKit 校验磁盘存在 |

Linux 现有 `checkNetDevice`/`checkDiskDevices`/`detectCPU`/`findNetDevice`/`findDiskDevice` 纯函数保留在 `cmd/orzdba`（可测），平台 helper 只做「读数据 + 调现有校验」。

### 3.3 数据流

main.go 组装不变：`renderer.AddSys(collector)` → 主循环每 tick `Collect()` → `BuildRow` 格式化。macOS 采集器的 `Collect()` 内部从原生 API 取数，返回与 Linux 相同结构的 `[]metric.Cell`。

## 4. 各指标实现

### 4.1 load

- **数据源**：`syscall.Sysctl("vm.loadavg")`，返回 `struct loadavg { double 1m; double 5m; double 15m }` 二进制字节
- **解析**：`unsafe.Pointer` 映射到 `[3]float64`（注意字节序，darwin 为 little-endian）
- **复用**：`zeroLoad()`/`loadColor()` 逻辑与 Linux 一致，颜色阈值 `load>ncpu` 不变

### 4.2 cpu

- **数据源**：`host_processor_info`（`PROCESSOR_CPU_LOAD_INFO` → `host_cpu_load_info_data_t`），或用 `host_statistics` + `HOST_CPU_LOAD_INFO` 获取全局 `cpu_ticks[CPU_STATE_USER..CPU_STATE_IDLE]`
- **tick 语义**：macOS 的 `cpu_ticks` 与 Linux `/proc/stat` 同为累计 jiffies 计数器，直接复用现有 `CPU.consume` 的 diff 公式
- **字段映射**：user→`CPU_STATE_USER`、nice→`CPU_STATE_NICE`、system→`CPU_STATE_SYSTEM`、idle→`CPU_STATE_IDLE`
- **差异**：macOS 无 iowait/irq/softirq 计数，`iow=0`、`steal=0`；`--full` 9 列中 irq/soft 为 0
- **首 tick**：与 Linux 一致（prev 零初始化 → 开机至今平均值），不强制零

### 4.3 mem

- **数据源**：
  - 总内存：`syscall.Sysctl("hw.memsize")` → uint64 字节
  - 明细：`host_statistics64(HOST_VM_INFO64)` → `vm_statistics64_data_t`（`free_count`、`active_count`、`inactive_count`、`wired_count`、`purgeable_count`、`page_size` 等）
- **计算**（macOS 语义，口径固定为一种，避免歧义）：
  - `total = hw.memsize`
  - `used = (active_count + wired_count) * page_size`（含 purgeable，purgeable 归入 active 计数）
  - `free = free_count * page_size`
  - `available = (free_count + inactive_count) * page_size`（近似 MemAvailable）
  - `buff = 0`（macOS 无对应）、`cached = inactive_count * page_size`（近似）
- **usage%**：`used/total*100`（与 Linux 的 `(MemTotal-MemAvailable)/MemTotal` 近似语义）
- **`--full` 7 列**：usage total used free avail buff cached，buff 置 0

### 4.4 swap

- **数据源**：`syscall.Sysctl("vm.swapusage")` → `xsw_usage { uint64 xsu_total; uint64 xsu_used; uint64 xsu_free; uint32 xsu_encrypted }`
- **语义差异**：Linux 是 si/so 每秒换入换出**速率**；macOS 无速率计数器，只有**当前用量**
- **决策**：si 列显示 `xsu_used`，so 列显示 `xsu_free`（单位字节），README 标注「macOS 显示当前用量，非速率」
- 列布局不变（2 列 si/so），首 tick 输出 0 逻辑保留

### 4.5 net

- **数据源**：`getifaddrs` + `struct if_data`（`ifi_ibytes`/`ifi_obytes`/`ifi_ipackets`/`ifi_opackets`/`ifi_ierrors`/`ifi_oerrors`/`ifi_iqdrops`）
- **接口过滤**：遍历 `ifaddrs`，按 `ifa_name == dev` 匹配；未找到时返回零值（与 Linux 降级一致）
- **速率计算**：复用现有 first-tick guard + diff/interval 逻辑
- **`--full` 8 列**：rxbytes/rxpkts/rxerr/rxdrop/txbytes/txpkts/txerr/txdrop，全部可从 if_data 取得（rxerr=`ifi_ierrors`，rxdrop=`ifi_iqdrops`）
- **字节单位**：`render.FormatBytesValue` 复用，unit 参数不变

### 4.6 disk

- **数据源**：IOKit `IOServiceGetMatchingServices(kIOMainPortDefault, IOBSDNameMatching(...))` 找到块设备，`IORegistryEntryCreateCFProperty(kIOPropertyStatisticsKey)` 取 `Statistics { bytes read/written; operations read/write }`
- **设备匹配**：`-d` 参数接受 macOS 设备名（`disk0`、`disk1s1` 等）；IOBSDNameMatching 按 BSD 名匹配
- **列映射**：
  - `r/s` = read operations/s、`w/s` = write operations/s
  - `rkB/s` = bytes read/s / 1024、`wkB/s` = bytes written/s / 1024
  - `queue`/`await`/`svctm`/`%util` = 0（IOKit 无请求队列/服务时间统计）
- **`--full`**：avgqu/avgrq/%iow/%util 置 0，仅保留 r/s w/s rkB/s wkB/s 真实值

## 5. 错误处理

- 原生 API 调用失败（sysctl 返回错误、getifaddrs 失败、IOKit 找不到设备）→ 返回零值行，不中断主循环（与 Linux 读 /proc 失败降级一致）
- 设备不存在（用户给错接口/磁盘名）→ 平台 helper 在启动时报错退出（与 Linux 现有 check 行为对称）
- 首 tick 语义保持与各平台 Linux 实现一致

## 6. 测试

### 6.1 纯函数单测（新增，仅 macOS 编译）

- load：`vm.loadavg` 3 double 解析
- swap：`xsw_usage` 结构解析
- mem：`vm_statistics64` 结构到各列的映射
- cpu：`host_cpu_load_info` tick 数组 → diff 公式
- net：ifaddrs 结构 → 8 列映射（可用构造的 if_data 数据）
- disk：IOKit 统计字典 → 列映射

### 6.2 本机端到端验证（macOS 手动）

1. `./bin/orzdba -sys -i 1 -C 5` → load/cpu/mem/swap/net 有真实值
2. `./bin/orzdba -m --full -i 1 -C 3` → 内存全字段
3. `./bin/orzdba -n en0 --full -i 1 -C 3` → 网卡 8 列
4. `./bin/orzdba -d disk0 -i 1 -C 3` → 磁盘 IO（真实设备名）
5. `go build ./...` + `go test ./...` 全绿
6. `GOOS=linux go build ./...` 交叉编译 → Linux 代码仍编译通过
7. `GOOS=windows go build ./...` 交叉编译 → Windows 仍编译通过

### 6.3 回归

- Linux 现有 `internal/syscol` 测试不动、全绿
- `cmd/orzdba` 现有测试（含 disk_test.go 的 /proc 黄金样本）不动、全绿

## 7. 文档

- README「版本说明」更新：新增 macOS 系统指标支持
- README「与 Perl 原版的偏差」新增：macOS 数据源与语义差异（swap 用量、disk 列置 0、cpu 无 iow/steal）
- README「指标域」表：注明 macOS 支持
- go-rewrite-plan.md：M9 之后新增里程碑 M10「macOS 系统指标支持」或作为增强记录

## 8. 风险与权衡

| 风险 | 缓解 |
|------|------|
| cgo 使交叉编译复杂（linux 交叉编译时需禁用 darwin 文件） | build tag 隔离，GOOS=linux 时 darwin 文件不参与编译 |
| IOKit 磁盘语义与 Linux iostat 差异大 | 明确文档化，仅真实列，其余置 0 |
| 字节序/结构体布局跨 macOS 版本 | 用系统头文件定义的结构体，`unsafe` 映射时加校验 |
| 内存 used 计算口径与 Linux 不同 | 文档化 macOS 口径（active+wired） |
