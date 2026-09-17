# 2026-09-17 主机迁移

原测试机 SSH 别名 18 即将回收。用户授权迁移到 wsrser，并手工启用 `loginctl enable-linger wsr`，已确认 Linger=yes。

## 新部署

- 目录 `/home/wsr/seesize`，属 wsr，根目录权限 0700，管理和 Agent 凭据 0600。
- 使用 systemd 用户服务，模板位于 `deploy/user/`。Ubuntu 20.04/systemd 245 不沿用旧 LoadCredential 模板。
- Hub 仅监听 `127.0.0.1:18081`，不占用已有业务 18080；不修改防火墙、代理或业务服务。
- 新 Agent ID `wsrser-main`，独立注册，10 秒上报，默认不扫描目录。
- 每日 03:00 加最多 5 分钟随机延迟备份，保留 7 份；手动备份验证成功。
- `scripts/preview.ps1` 默认转发至 wsrser:18081，本地地址仍为 http://127.0.0.1:18082。旧机预览需显式指定 `-SshHost 18 -RemotePort 18080`。

## 数据与验证

迁移一致性快照生成于香港时间 2026-09-17 21:27:12。快照包含 2 个服务器、50150 条指标、211 个目录快照、0 个告警事件和 2 个设备身份。恢复后记录数一致，新 Agent 开始产生额外指标与身份；旧机 24 小时趋势返回 360 个聚合点，管理员登录、退出及新版侧边栏 HTML 检查通过。

旧机在线源数据库未直接复制，使用经过完整性验证的备份恢复到新路径。历史备份和管理凭据同时迁移；管理登录凭据不变。新主机不会沿用旧机身份，因此旧机和此前临时测试身份显示离线属于预期。

旧服务暂未停止、旧数据未删除。该迁移是上述时点的快照，不是持续复制；原机此后新产生的数据仍只在原机，不能宣称零数据丢失切换。若需要补齐这段尾部数据，应在回收前单独保全，不能覆盖已经接收新 Agent 数据的新库。

原始传输归档保留在新主机 `/home/wsr/seesize/migration/transfer.tar.gz`；源快照位于 `/home/wsr/seesize/migrate-20260917/`。本地受限中转目录位于用户临时目录 `seesize-migration-20260917`，包含敏感归档，未加入 Git。

## 维护

```sh
systemctl --user status seesize-hub seesize-agent seesize-backup.timer
journalctl --user -u seesize-hub -u seesize-agent -n 50 --no-pager
systemctl --user start seesize-backup.service
systemctl --user list-timers seesize-backup.timer
```

旧文档 `/opt/seesize-dev`、系统级 systemctl 命令及仅限 18 的压力脚本不应直接用于该业务主机。后续压力实验仍需隔离和重新确认资源预算。

Docker、site_total 在迁移前后均 active，主要原有监听端口保持。此检查不能证明业务功能完全无影响，需用户复验业务页面和核心读写。尚未执行整机重启或真实 HTTPS 验收。用户服务采用同一 wsr 身份，隔离强度低于独立专用系统用户，这是当前无 sudo 部署的边界。
