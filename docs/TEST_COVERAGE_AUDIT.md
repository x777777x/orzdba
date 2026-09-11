# orzdba 单测覆盖现状

> 生成日期：2026-09-12（本文件由人工在代码改动后重新生成；重新生成方式见文末）
> 运行环境：macOS（darwin）。`go test -race -coverprofile=cover.out ./...`

## 各包覆盖率（darwin 视角）

| 包 | 覆盖率 | 说明 |
|---|---|---|
| `internal/render` | 93.4% | 格式化/渲染全路径 |
| `internal/rtcol` | 83.1% | tcprstat 生命周期（fake 二进制驱动，双平台可跑） |
| `internal/logsink` | 82.8% | stdout/file/daily/tee |
| `internal/mycol` | 81.9% | StatusSource（mock driver）+ 各采集器 |
| `internal/mysqlc` | 80.4% | 凭证解析、DSN、my.cnf（含转义/!include 告警） |
| `cmd/orzdba` | 36.5% | args/daemon/ip 纯函数有测；`main`/`runLoop` 无单测（需 /proc + MySQL + tcprstat） |
| `internal/syscol` | 23.7% | **darwin 视角偏低是 build-tag 假象**：23 个解析级测试全部 `//go:build !darwin`，在 Linux CI 上运行 |

**关键点：syscol 在 Linux 上的真实覆盖率远高于 23.7%** —— cpu/net/swap/disk/mem 的 fixture 测试（含计数器归零 clamp、5s 漂移窗口）都是 `!darwin` 文件。CI（ubuntu + macos matrix）才是覆盖率数字的权威来源；本机 darwin 跑出的数字不代表 Linux。

## 关键函数（darwin 可测部分）

| 函数 | 覆盖率 |
|---|---|
| `normalizeArgs` / `daemonChildArgs` / `findLogfileArg` | 100% |
| `mycol.StatusSource.Fetch`（经 mock） | 覆盖（shift/降级/错误路径有专测） |
| `StatusSource.SlaveStatus`（8.4 回退+缓存） | 90.3% |
| `isSystemDB` / `unescapeCnf` / `rateDenom` | 100% |
| `runLoop` / `main` | 0%（未单测——启动流程依赖 /proc、MySQL、tcprstat） |

## 已知残留缺口

1. `main()` / `runLoop()`：只有 sinkWriter 纯函数有测。若要覆盖，需注入 mock Sink + mock StatusSource 的 loop 集成测试（历史建议，仍有效）。
2. `mysqlc.Open()`：真实拨号路径未测（可用 mock driver，但收益有限——DSN 构造与凭证解析已覆盖）。
3. syscol 的 darwin 原生路径（IOKit/host_statistics/getifaddrs）只有冒烟断言，无解析级单测。

## 重新生成

```bash
go test -race -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out
```

（完）
