# 存储生命周期与可持续维护

## 当前规则

| 数据 | 策略 | 不会做的事 |
| --- | --- | --- |
| 在线指标、快照、告警 | 7 天，启动时及每 10 分钟分批清理，单轮预算 30 秒 | 不删除设备身份来腾空间 |
| 日常备份 | 成功生成并验证后保留最近 7 份 | 不把 7 份当作严格 7 个日历日 |
| 升级回退点 | 注册清单内按创建时间保留最新 3 个 | 不扫描并删除任意未知目录 |
| 迁移、恢复演练文件 | 必须登记到期时间，过期后清理 | 不删活跃备份/恢复服务使用的文件 |
| 备份中断残留 | 无活跃备份/恢复服务时，仅删除 backups 内工具命名、超过 48 小时的 partial | 不删除 db、wal、shm |
| 维护报告 | storage-status.json 原子覆盖，只留最新一份 | 不按天追加无限报告文件 |
| 应用占用/剩余磁盘 | 每日检测；超过 1 GiB 或剩余不足 2 GiB 时服务返回失败并记录告警 | 不以告警阈值充当硬配额；无外部推送 |

部署文件：scripts/storage-maintenance.py、deploy/user/maintenance-artifacts.json 和 seesize-maintenance.service/timer。每天 04:00 后随机延迟最多 5 分钟运行，支持错过计划后执行。读写仅限当前 wsr 用户。维护最多执行 2 分钟，默认命令无 --apply 为预演。

新增升级回退目录、恢复实验必须登记清单，临时项设置 expires_at（建议验收后 48 小时），回退项填写 created_at（UTC 同格式）。未登记内容不会被自动删除，会计入空间告警；因此这项规则也必须纳入后续部署流程。不能同时运行手动备份/恢复/升级与清理，自动维护会检查 systemd 的 SeeSize 备份和恢复任务；未托管手动进程无法由该检查完整识别。

## 2026-09-25 执行证据

预演和隔离规则测试通过。首次删除 hub-before-charts-20260922、migrate-20260917、migration、restore-check-20260922；恢复实例 MainPID=0 且已到期停止。保留 pre-network-20260924、pre-delete-20260925、pre-backup-panel-20260925 三个回退点。

目录占用从约 510 MiB 降至 393 MiB，释放约 116 MiB。被删除文件没有放入回收站，不能保证恢复对应历史时点；新的日常备份和三个回退点仍在。Hub/Agent/backup timer/maintenance timer active，维护服务 Result=success，Docker/site_total active。

## 边界：不能承诺磁盘大小永远不变

- 固定设备数、采样率和数据规模时，7 天历史及 7 份快照应趋于稳定；设备数增加、扫描内容增多或提高采样率会提高稳定容量水平。需再评估预算。
- SQLite 删除数据后复用空闲页，不自动缩小主文件。不要高频 VACUUM，更不能直接删 WAL/SHM。需要回收高水位空间时，在维护窗口备份后安排专门压缩。
- 注册身份、撤销摘要、删除标记不自动轮换；删除标记用于阻止旧凭据和迟到请求恢复设备，不能为了空间盲目删除。大量身份变动另需配额/归档设计。
- journald 由主机既有共享日志策略管理，本轮没有修改全机或 wsr 用户日志配额，也没有执行全局日志清理。项目目录空间报告不包含 journald。本轮不是每服务硬配额方案。
- Windows 本地构建缓存、测试包、CSV/JSON 实验报告不在服务器维护范围内，不影响新机空间；本地缓存后续按开发机规则管理，实验报告作为毕业设计证据保留。
- 告警只在状态文件和维护服务退出状态记录，无主动通知。需通过下列命令检查，后续可接入面板/通知。

```sh
systemctl --user status seesize-maintenance.timer
systemctl --user show seesize-maintenance.service -p Result -p ExecMainStatus
cat /home/wsr/seesize/storage-status.json
python3 /home/wsr/seesize/storage-maintenance.py
```

结论：已建立历史期限、备份份数、回退数量、临时文件到期和容量检测五层规则；不是所有磁盘数据的绝对上限，也不是证明长期稳定性的替代品。
