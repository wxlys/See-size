# systemd 部署与维护

当前部署脚本面向已完成注册的 Linux amd64 单机测试环境，要求 systemd 支持 `LoadCredential`（测试机为 Debian 12 / systemd 252）。它不是任意云服务器通用的一键安装器：不安装系统依赖、不开放防火墙、不配置公网 HTTPS，也不会替用户创建新的设备身份。

## 安装准备

独立目录（默认 `/opt/seesize-dev`）需要已有 Hub、Agent、backup Linux amd64 程序，`admin.token`、`agent-auth.token`、`data/seesize.db`。将仓库 `deploy` 目录上传进去。默认 Agent ID 为 `aliyun-test-18`，应与注册时一致。

```sh
sh /opt/seesize-dev/deploy/install-services.sh /opt/seesize-dev aliyun-test-18
```

脚本需要 root，创建专用 `seesize` 系统用户，仅将 data/backups 目录所有权交给它；程序目录保持 root 管理，凭据源文件保持 root-only，由 systemd 的私有凭据目录传给服务。现有 PID 文件的可执行文件必须准确匹配，脚本才停止旧进程。已有同名 systemd 单元时脚本拒绝覆盖。

Hub 监听 `127.0.0.1:18080`；Agent 每 10 秒上报，专用测试目录每 30 分钟扫描；备份每天服务器本地时间 03:00 后随机延迟最多 5 分钟运行，保留最近 7 份，错过计划后支持补执行。旧 nohup 日志和 PID 文件不会删除，但不再代表当前服务状态。

## 常用操作

```sh
systemctl status seesize-hub seesize-agent seesize-backup.timer
systemctl restart seesize-hub
systemctl restart seesize-agent
systemctl start seesize-backup.service
systemctl list-timers seesize-backup.timer
journalctl -u seesize-hub -u seesize-agent -n 100 --no-pager
journalctl -u seesize-backup -n 30 --no-pager
```

日志使用 journald，由系统现有日志保留策略轮转；服务配置日志速率限制，未修改主机全局日志配额。Hub 和 Agent 异常退出后 5 秒重启，60 秒内最多尝试 5 次；持续失败会进入 failed，修复后用 `systemctl reset-failed` 再启动。正常停止不会重启。Hub 重启后登录会话失效，需要重新登录。

## 修改配置与升级

使用 `systemctl edit seesize-agent` 的 override 修改配置。改变 ExecStart 前先写空的 `ExecStart=` 再写新命令；保留 `-token-file %d/agent-token`。可改上报周期、扫描目录和 Hub 地址。扫描受专用用户权限、ProtectHome 和只读沙箱限制，读取失败会产生不完整快照，不要直接改为 root 运行解决权限问题。当前模板只覆盖本机 Hub，远程 Agent 的地址与网络配置需单独调整。

升级时先备份数据库，上传新可执行文件到临时名称，验证后停止相应服务、保留旧版本、替换程序，再启动并检查 healthz、登录和心跳。不要直接覆盖运行中的可执行文件。修改单元文件后执行 `systemctl daemon-reload`。此流程尚未封装为自动升级命令。

## 停用和卸载

```sh
systemctl disable --now seesize-backup.timer seesize-agent.service seesize-hub.service
systemctl stop seesize-backup.service
```

上面只停用服务，保留所有数据。完整卸载可在确认停止后，人工移除 `/etc/systemd/system/` 中本项目的三个 service 文件和一个 timer 文件，再执行 `systemctl daemon-reload`；数据库、备份、凭据和系统用户不会自动删除。当前未提供自动删除数据的卸载脚本。

## 已执行验证

- systemd 单元语法检查通过；三个启动单元 active 且 enabled。
- 专用用户成功读取私有凭据，Agent 心跳及扫描被接收。
- 系统任务生成并验证备份，退出状态 0。
- 对测试 Agent 发送 SIGKILL 后，新 PID 自动启动，NRestarts 从 0 变为 1。
- 未进行整台服务器重启，避免影响主机上其他服务。
