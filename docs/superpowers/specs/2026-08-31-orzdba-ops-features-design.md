# orzdba 运维功能增强设计（daemon / 双写输出 / 表头开关 / 自定义分隔符）

> 文档版本：v1.0
> 编写日期：2026-08-31
> 状态：已批准（2026-08-31）

## 1. 背景与目标

新增四个运维相关功能：

1. **后台运行**：进程自我 daemon 化（fork+setsid），父进程退出、子进程后台持续采集
2. **输出文件 + 按天截转 + 与 stdout 不冲突**：`-L` 写文件的同时可额外输出到 stdout（`--also-stdout`），文件按天截转（复用现有 `-logfile_by_day`）
3. **不输出表头**：`-noheader` 关闭启动标题块与周期性表头
4. **自定义列分隔符**：`--sep` 指定数据行的列间分隔符（不再按 Group 类型区分）

**非目标**：
- 不改变 `-L` 现有「只写文件」的默认行为（向后兼容）
- 不新增与 `-L` 功能重叠的独立文件参数（曾考虑 `-o`，已合并为 `-L` + `--also-stdout`）
- 不做 PID 文件、systemd unit 集成（用户可自行包装）
- Windows 不支持 daemon 化（报错退出，类似 `-rt` 的 Linux-only 处理）

## 2. 参数设计

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `--daemon` | bool | off | 后台 daemon 化运行（仅 Unix；Windows 报错；无短 `-d`，因 `-d` 已被 `--disk` 占用） |
| `--also-stdout` | bool | off | 与 `-L` 配合：写文件的同时输出到 stdout |
| `-noheader` | bool | off | 不输出表头（启动标题块 + 周期性表头） |
| `--sep` | string | `"|"` | 数据行列间分隔符；`\t` 表示制表符；指定后所有列用同一符号 |

**参数交互**：
^- `--daemon` 且未指定 `-L` → 自动 `-L /tmp/orzdba.log`（daemon 必须有落盘；由 daemonize 注入子进程 argv）
^- `--daemon` 且指定 `-L` → 照用（daemon 写该文件）
- `--also-stdout` 无 `-L` → 报错（`--also-stdout` 依赖 `-L`）
^- `--daemon` 与 `--also-stdout` 同时 → `-d` 生效（daemon 无终端，stdout 被重定向 /dev/null），`--also-stdout` 实际不输出但可接受

## 3. 架构与落点

### 3.1 daemon 化（main.go）

进程解析参数后、打开 sink 前，若 `--daemon` 则自我 daemon 化：

1. `os/exec` 以相同参数重新启动自身（去掉 `-d` 防递归），`SysProcAttr.Setsid = true` 脱离会话
2. 子进程 stdin/stdout/stderr 重定向 `/dev/null`（daemon 不占用终端）
3. 父进程立即 `os.Exit(0)`，终端返回
4. 子进程继续正常逻辑（打开 `-L` 输出文件等）

**跨平台**：`setsid` 仅 Unix；Windows 上 `-d` 直接报错退出（平台文件隔离，参考 `umask_windows.go` 模式）。

### 3.2 输出 sink（internal/logsink）

新增 `Tee` sink：同时写 stdout + 文件（复用现有 `File`/`DailyFile` 轮转逻辑）。

```
type Tee struct {
    out  *Stdout     // 写 stdout
    file *DailyFile  // 写按天截转文件（-logfile_by_day）或 File
}
```

- `-L path`（无 `--also-stdout`）→ `File`/`DailyFile`（现有行为不变）
- `-L path --also-stdout` → `Tee{Stdout, File/DailyFile}`（双写）
- 无 `-L` → `Stdout`（现状）

`Tee` 实现 `Sink` + `RotateSink` 接口（`MaybeRotate` 委托内部文件），main.go 的日切标题重打/计数重置逻辑复用。

### 3.3 表头开关（internal/render）

`Renderer` 加 `headerOff bool` 字段：
- `Header()` 在 `headerOff` 时返回 `""`
- main.go 的 `writeTitle`（启动标题块）在 `headerOff` 时不输出

### 3.4 自定义分隔符（internal/render）

`Renderer` 加 `sep string`（默认 `"|"`）：
- `sep()` 直接返回配置值，**不再按 Group 类型区分**（`|` vs 绿色 `|`）
- 仅数据行列间使用；表头行不动
- `--sep \t` → 制表符
- 分隔符改变后列不再对齐（用户主动选择，预期行为）

## 4. 数据流

```
argv → parseArgs (新增 4 参数 + 校验)
     → --daemon ? daemonize（fork+setsid，父进程退出）
     → 组装 renderer（-noheader / --sep 注入）
     → 组装 sink（-L / --also-stdout / 默认 / daemon 默认日志）
     → 主循环（写 sink，日切轮转复用）
```

## 5. 错误处理

- `-d` 在 Windows → 报错退出
- `--also-stdout` 无 `-L` → 报错退出
- daemonize 失败（exec 错误）→ 报错退出，不静默
- `--sep` 值中字面 `\t`（反斜杠+t）转换为制表符；其它 `\x` 序列不解析、按字面输出（例如 `--sep '\t'` → 制表符，`--sep '|'` → 竖线）

## 6. 测试

- **render 单测**：`-noheader` 时 `Header()` 为空；`--sep ,` 时数据行列间为 `,`；`--sep \t` 为制表符
- **logsink 单测**：`Tee` 双写（stdout + 文件）
- **args 单测**：参数解析、`--also-stdout` 无 `-L` 报错、`-d` Windows 报错
- **本机实测（macOS）**：`-d -L /tmp/x.log -C 3` 父进程退出、子进程写文件；`--also-stdout` 双写；`-noheader` 无表头；`--sep ,` 逗号分隔
- **跨平台**：darwin 测试全绿、linux/freebsd/windows 交叉编译、centos/ubuntu 容器回归

## 7. 风险与权衡

| 风险 | 缓解 |
|------|------|
| daemon 化参数透传遗漏 | 用原始 argv 重建命令，仅剔除 `--daemon` |
| Tee 双写缓冲/顺序 | 直接 io.Writer 串联，无缓冲层 |
| 分隔符改变破坏列对齐 | 文档说明为预期；默认 `|` 不变 |
| Windows daemon 不支持 | 明确报错，不静默降级 |
