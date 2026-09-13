# 备份与恢复

`seesize-backup` 是独立程序，使用 SQLite `VACUUM INTO` 生成在线一致性快照，再检查数据库完整性与 SeeSize 表结构。它从数据库连接读取，包含已经提交但仍位于 WAL 的内容，不依赖直接复制主数据库。依据：[SQLite VACUUM INTO 文档](https://www.sqlite.org/lang_vacuum.html)。此方式会读取整个数据库并消耗 CPU/IO，不是增量备份；单次超时为 5 分钟，较大数据库后续需要增量备份优化。

## 手动与定期备份

```sh
seesize-backup -source /opt/seesize-dev/data/seesize.db -dir /opt/seesize-dev/backups -keep 7
seesize-backup -source /opt/seesize-dev/data/seesize.db -dir /opt/seesize-dev/backups -keep 7 -interval 24h
```

周期模式启动后立即备份，每次完成后等待指定间隔。备份成功并校验后才清理旧备份；只处理目录中符合本工具时间戳命名的普通文件，不递归删除目录。保留最近 7 份不是严格保留 7 个日历日：手动备份或进程重启会增加备份次数。必须使用专用目录，避免多进程对同一备份目录执行清理。当前进程未注册为系统服务，机器重启后需要重新启动，systemd 部署待完善。

生成期间使用 `.partial` 文件，验证后通过同文件系统硬链接发布，不覆盖同名目标；文件系统不支持硬链接时操作失败，不降级为覆盖写入。异常断电可能留下 `.partial` 文件，确认备份进程不在运行后可人工清理；它们不计入成功备份。

## 验证和恢复

```sh
seesize-backup -source /path/to/backup.db -verify
seesize-backup -source /path/to/backup.db -restore-to /path/to/new-restored.db
```

恢复目标必须不存在，包括空文件也拒绝覆盖。恢复不会自动切换正在运行的 Hub。正式恢复时：先验证备份，生成新数据库，停止 Hub 后修改 `-data` 指向新路径，再启动并验证登录、节点上报和历史查询；保留原数据库以便回退，不混用旧路径的 WAL/SHM 文件。

备份包含服务器信息、历史指标、扫描路径、告警、设备凭据摘要和当时尚未兑换的注册码摘要。管理员登录凭据与 Agent 原始凭据在数据库之外，需要单独安全保存。恢复旧备份会回退设备撤销／注册状态，应重新核查设备授权，必要时撤销并重新注册设备。会话不在备份内，恢复后重新登录。程序不提供备份加密，应将备份放在受限目录或加密存储中。

## 本次验证

本地：在线 WAL 内容、备份数量轮转、恢复查询、损坏输入拒绝、已有目标拒绝全部通过。

测试机：恢复验证得到 1 台服务器、15867 条指标、20 份磁盘快照、1 个设备凭据摘要；完整性检查通过。测试恢复使用独立文件，未切换生产数据路径。备份进程每 24 小时执行，保留 7 份；另下载一份到本地 `.data/backups/`，未加入 Git。
