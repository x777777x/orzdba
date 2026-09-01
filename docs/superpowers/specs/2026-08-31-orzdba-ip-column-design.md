# orzdba -ip 参数设计（IP 列 + 远程 MySQL 互斥校验）

> 文档版本：v1.0
> 编写日期：2026-08-31
> 状态：已批准（2026-08-31）

## 1. 背景与目标

新增 `-ip` 参数，输出一列 IP（被监控主机），并处理远程 MySQL 时的系统指标误导问题。

**目标**：
1. `-ip`：在时间列后输出一列 IP，值 = 被监控主机（本地→本机网卡 IP；远程→`-H` IP）
2. **远程 MySQL 互斥**：当 `-H` 指向远程 MySQL 时，禁止输出本地系统指标（sys 系参数），否则报错退出——避免「本地采集的 sys 指标」与「远程 MySQL 指标」混排误导

**非目标**：
- 不自动解析域名判断本机/远程（域名一律视为远程，避免 DNS 脆弱判定）
- 不改 `-lazy` 在本地模式的混合行为
- 不做 IP 列的 CSV/JSON 专用格式（跟随现有 `--sep`/`-noheader` 渲染）

## 2. 参数

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `-ip` | bool | off | 输出 IP 列（时间后），值 = 被监控主机 |

## 3. 本机/远程判定（isLocalHost）

规则（宽松判定，不解析域名）：

| `-H` 取值 | 判定 | 说明 |
|-----------|------|------|
| `127.0.0.1` / `localhost` / 空 | 本机 | 显式回环/默认 |
| 本机某网卡 IP（`net.InterfaceAddrs` 枚举） | 本机 | 含 `192.168.x.x` 等 |
| 其它 IP / 域名 | 远程 | 域名一律视为远程（不解析） |

**注意**：`-H` 为空时 config 默认已是 `127.0.0.1`，所以实际只会遇到 `127.0.0.1`/`localhost`/本机IP/远程IP/域名。

## 4. 互斥校验

**触发条件**（同时满足）：
1. `mysql` 模式开启（`-mysql`/`-lazy`/`-innodb`/任意 mysql 叶子参数）
2. `-H` 为远程（非本机）
3. 指定了任一 sys 系参数：`-l`/`-c`/`-s`/`-m`/`-d`/`-n`/`-sys`/`-lazy`

**行为**：报错退出（类似现有 `-logfile_by_day requires -L` 的启动校验）：

```
ERROR: -H <host> is a remote MySQL; local system metrics
(-l/-c/-s/-m/-d/-n/-sys/-lazy) would be misleading.
Use them only with a local -H.
```

**兼容点**：
- 本地模式（`-H` 本机）→ 无校验，`-lazy` 混用照旧
- `-ip` 单独使用（无 mysql）→ 输出本机 IP 列，无互斥校验
- 不指定 `-ip` → 完全无行为变化（纯增量）

## 5. 实现落点

### 5.1 参数（args.go）
- config 加 `ip bool`
- pflag 注册 `-ip`（无短参）
- `expand` 后执行互斥校验（需在 composite 展开后，才能知道 mysql/sys 是否被 `-lazy` 等触发）

### 5.2 IP 列（internal/render + main.go）
- `Renderer` 加 `ipCol string`（空=不显示）
- `-ip` 时：renderer 设置 IP 值
- IP 列显示在 time 列后（新增一个 sys 组 collector 或直接注入）
- 实现：新建 `ipCol` collector（Name "ip"，Headline 显示 "IP"，Collect 返回 [IP]），`-ip` 时 AddSys 在 time 后

### 5.3 本机判定（main.go）
- `isLocalHost(host string) bool`：回环/空→true；枚举 `net.InterfaceAddrs` 对比→true；否则 false
- 复用现有 `primaryIP()` 获取本机网卡 IP（非回环）

## 6. 测试

- **args 单测**：
  - `-ip` 解析
  - 远程 `-H` + `-lazy` → 报错
  - 远程 `-H` + `-l` → 报错
  - 本地 `-H 127.0.0.1` + `-lazy` → 通过
  - 远程 `-H` + 仅 mysql 参数 → 通过
- **render 单测**：IP 列存在/缺失、与 `--sep`/`-noheader` 配合
- **本机实测（macOS）**：`-ip -sys` 输出本机 IP 列；`-H 127.0.0.1 -ip -mysql` 输出本机 IP；`-H 远程 -ip -mysql` 输出远程 IP；`-H 远程 -lazy` 报错
- **全平台回归**：darwin 全量 + centos/ubuntu 容器 + 交叉编译

## 7. 风险与权衡

| 风险 | 缓解 |
|------|------|
| 域名指向本机被误判为远程 | 接受（宽松判定）；用户可用本机 IP 显式指定 |
| 本机多网卡时 primaryIP 取第一个 | 与现有标题块行为一致，文档说明 |
| 互斥校验误伤合法远程+本地混搭 | 设计确认（用户要求禁止该场景） |
| `-lazy` 本地混用被破坏 | 仅远程时校验，本地照旧 |
