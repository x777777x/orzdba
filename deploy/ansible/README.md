# orzdba — Ansible 部署

部署 orzdba 二进制、orzdba 专属凭证文件 `/etc/orzdba.cnf`（0600、正确属主——否则 **orzdba 拒绝启动**）、systemd 服务单元，以及（可选）最小权限的 MySQL 监控账号。

## 目录结构

```
ansible/
├── inventory.example
├── group_vars/
│   └── all.yml              # 变量（密码放 vault 文件）
├── playbooks/
│   ├── deploy.yml           # 首次部署：二进制 + cnf + 服务单元 + 启动（含建账号）
│   ├── start.yml            # 停止后/手动停机后的启动
│   ├── stop.yml             # 停止整个集群
│   └── upgrade.yml          # 滚动升级：分批 + 健康检查 + 失败回滚
├── releases/                # 把编译好的二进制放这里（git 忽略）
│   └── orzdba-linux-amd64
└── roles/orzdba/
    ├── defaults/main.yml
    ├── tasks/               # deploy / upgrade / mysql-user / health
    └── templates/           # orzdba.cnf.j2、orzdba.service.j2
```

## 编译二进制

```bash
cd <仓库根目录>
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o deploy/ansible/releases/orzdba-linux-amd64 ./cmd/orzdba
```

## 凭证（vault）

把真实密钥放进 ansible-vault 文件，**不要**写在 `all.yml` 里：

```bash
ansible-vault create group_vars/all.vault.yml
#   orzdba_mysql_password: "真实密码"
#   orzdba_mysql_admin_password: "管理员密码"   # 仅在需要建账号时
```

playbook 把密码渲染进 `/etc/orzdba.cnf`，权限 0600、属主正确，因此主机上唯一的明文密码副本就是这个 root 可读的文件。**CLI 没有密码参数**——argv 里的密码会被同机任意用户通过 `ps(1)` 看到，已移除。

## 最小权限 MySQL 账号

orzdba 只需要两项授权——`PROCESS`（用于 `SHOW ENGINE INNODB STATUS`）和 `REPLICATION CLIENT`（用于 `SHOW SLAVE STATUS`）。设置 `orzdba_create_mysql_user: true` 后，playbook 会创建只带这两项授权、绑定 `localhost` 的账号。密码即使泄露，也仅限于只读的服务端状态，够不到业务数据。

如果想让服务用专用系统用户（而非 root），设置 `orzdba_service_user: orzdba`，凭证文件属主即该用户；orzdba 的严格校验接受「运行用户或 root」。

## /etc/orzdba.cnf 的作用

- **orzdba 专属**：不与 mysql 客户端、备份脚本等共享 `[client]` 段，互不干扰、互不覆盖。
- **程序强制最小权限**：orzdba 启动时校验文件必须 0600 且属主为运行用户或 root，否则拒绝启动——playbook 负责落对权限，orzdba 负责运行时兜底（手工 `chmod 644` 会失败关闭）。
- **本机唯一明文密码副本**：渲染后的密码只在这个 root 可读文件里，不在 argv、不在环境变量、不在其他程序可读的 my.cnf。
- 若改为 Unix socket 认证（`orzdba_mysql_socket`），此文件可完全不存密码。

## 用法

```bash
# 首次部署（有 vault 时加 --ask-vault-pass）
ansible-playbook -i inventory playbooks/deploy.yml --ask-vault-pass

# 停止 / 启动
ansible-playbook -i inventory playbooks/stop.yml
ansible-playbook -i inventory playbooks/start.yml

# 滚动升级，每次 3 台
ansible-playbook -i inventory \
  -e orzdba_bin=../releases/orzdba-linux-amd64-1.1.0 \
  -e orzdba_version=1.1.0 \
  -e orzdba_rollout_batch_size=3 \
  playbooks/upgrade.yml
```

升级行为：每批先备份当前二进制 → 安装新版本 → 重启 → 健康检查（最新 `orzdba.log.*` 必须在 `orzdba_health_check_seconds` 内出现新数据行）。失败则本批恢复旧二进制并重启、play 停止——未触及的主机保持旧版本继续监控。

## 加固说明

- `/etc/orzdba.cnf` 的 0600 + 属主由 **orzdba 自身**在启动时强制（不只是 playbook），因此手工 `chmod 644` 会失败关闭。
- 把 `/etc/orzdba.cnf` 从通用备份任务中排除，或让它的备份进入你的密钥管理系统（它是主机上唯一的明文密码）。
- 定期轮换监控密码并重新运行 `deploy.yml`（模板与账号授权均为幂等）。
