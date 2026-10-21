# 目录分析改进上线记录（2026-10-21）

用户授权后，于香港时间 2026-10-21 13:54～13:56 完成 wsrser Hub 更新与公网验证。

## 构建与范围

- 干净源码提交：174b91a0a7ed859a2583ce87535308f51365019d。
- Go 1.27.1、Linux amd64、CGO_ENABLED=0、-trimpath。
- 新 Hub SHA256：9d0f8d971d90ff347f67d66782f81887c027905332fd66b91bf55d43e4f829ff。
- 旧 Hub SHA256：aa208982928dfaef26b78f65dc8969b7e0129a0833b99ca09d0c678711275e5d。
- 只更新 Hub。Agent PID/哈希、凭据、Hub 服务配置及 drop-in 未变；没有数据库迁移或扫描任务配置变更。

## 备份与恢复

- 现有可信备份程序创建并验证升级前数据库：`/home/wsr/seesize/pre-disk-20261021-174b91a/seesize-20261021T055450.455530209Z.db`。
- 回退点登记为 rollback，纳入最近三份回退点保留策略；保留旧程序、配置、候选和备份。
- 临时目录 `/home/wsr/seesize/restore-check-disk-deploy-174b91a` 登记为 temporary，一天后到期，由维护任务清理。
- 升级时暂停备份/维护定时器并等待正在运行的任务结束，替换时持有共享维护锁；结束恢复原定时器状态。

## 线上验证

- 部署检查 blockers=0；已知 degraded 为九月到期临时恢复实例，与本轮服务无关。
- 公网/内网 healthz 正常，Hub Result=success、NRestarts=0，无部署窗口警告日志。
- 主设备 online，观察到 last_seen 从 05:54:43 UTC 推进到 05:55:03 UTC；Agent 未重启。
- 30m/1h/24h、历史时间段及备份状态接口正常。
- 目录接口真实响应包含新增 reference_snapshot，当前 snapshots=[]、reference_snapshot=null、comparable=false，页面正常显示暂无快照。
- 公网 Edge 登录、新页面、目录接口和退出后 401 验证通过。
- 使用线上实际页面配合浏览器内拦截的临时目录数据，验证大小／增长排序、占比、树展开、不完整原因、完整参考与部分结果切换。没有向 Hub 上传该测试数据，没有在服务器启动扫描。
- 已有图表公网回归仍通过，包括四图悬浮值与接口一致、放大和历史选择。
- 最终 Hub、Agent、备份 timer、维护 timer 均 active。

当前线上没有保留期内目录快照，因此真实扫描→上传→新目录 UI 的端到端人工体验尚未覆盖，不把浏览器测试数据当作实际扫描结果。

## 用户查看

刷新 https://8.148.5.169/，必要时 Ctrl+F5；Hub 重启后原会话失效，使用原管理凭据重新登录。只有服务器上传目录快照后才出现目录排序和树结构。Agent 无需更新或重新注册。

scripts/deploy-disk-hub.py 是这个版本的指定路径/哈希部署记录，复用 deploy-chart-hub.py 的备份、切换、检查与回退流程，不是适用于任意版本的通用更新命令。
