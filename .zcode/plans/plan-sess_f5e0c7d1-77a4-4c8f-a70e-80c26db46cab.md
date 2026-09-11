继续执行已批准的计划（内容无变化，仅更新进度状态）

## 当前进度
- 已提交：P0-1（e6c02d1 日志写错误）、P0-3（88b388a my.cnf host/port）、P0-4（ff55502 daemon 剥离集）
- P0-2 进行中（可编译、无回退）：
  - ✅ syscol/collector.go 泛型 clamp0
  - ✅ net.go dRecv/dSend + full 模式 rate 闭包
  - ✅ swap.go dIn/dOut
  - ⏳ disk.go 8 差值、disk_darwin.go 4 差值、net_darwin.go 两处
  - ⏳ 测试：clamp0 纯函数（无 tag 文件）+ net/swap/disk 计数器归零用例（syscol_test.go，CI 运行）

## P0-2 后的批次（与已批准计划一致，各独立 commit）
- P1-5 rateDenom 分母；P1-6 IOKit 单行释放；P1-7 !include 告警；P1-8 SIGHUP + README；P1-NEW whitelist 8 死条目改连字符
- P2-9 MySQL 8.4 回退+缓存+双列名+semi source；P2-10 findLogfileArg + daemon stderr 预打开 + also-stdout 提示；P2-NEW socket/远程 host 冲突检查；P2-11 转义 6 序列；P2-12 mem availKB 同源；P2-13 isSystemDB
- P3：ipCol 定宽；Chmod 告警；README 三处；删 reopen；覆盖率文档；CI matrix；exitedCh；--sep 拒 \n\r；ping 超时 max(flag,2s)

## 验证
每项独立 commit；本机 go vet + go test -race ./...；GOOS=linux/windows go vet；覆盖率文档最后生成。