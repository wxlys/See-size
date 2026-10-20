# 趋势图线上更新记录（2026-10-20）

用户授权更新后，于香港时间 2026-10-20 21:00～21:02 完成 wsrser 的 Hub 更新及公网浏览器验收。

## 组件

- Hub 构建源码：`e0f1030`（干净工作区构建），Go 1.27.1，Linux amd64，CGO_ENABLED=0，-trimpath。
- 新 Hub SHA256：`aa208982928dfaef26b78f65dc8969b7e0129a0833b99ca09d0c678711275e5d`。
- 旧 Hub SHA256：`56e9bb7955992d7054d25098d05bfe660ad641cf3d9b6898418ba8d20a052859`。
- Agent 未替换、未重启，PID、哈希保持不变；采样周期仍为十秒。凭据、Hub systemd 单元及 drop-in 均未改变。无需数据库迁移。

## 备份与生命周期

- 升级前使用原有可信备份程序创建并验证快照：`/home/wsr/seesize/pre-charts-20261020-e0f1030/seesize-20261020T130057.748450559Z.db`。
- 回退点包含旧程序、候选程序、服务配置、验证备份和 release.json，已登记为 rollback，纳入最新三份回退点保留策略。
- staging：`/home/wsr/seesize/restore-check-chart-deploy-e0f1030`，含部署脚本、候选和 deployment-result.json，已登记 temporary，约一天后到期，由后续维护任务清理。
- 备份/维护 timer 升级时暂停，等待其服务空闲，结束后恢复原 active 状态。替换阶段持有共享维护锁。
- 没有恢复或覆盖数据库，没有删除设备、凭据或历史数据。

## 实际验证

- 部署检查 blockers=0。用户管理器 degraded 来自已到期的九月临时恢复实例，并非当前 Hub/Agent 故障；该历史单元未改动。
- 内网与公网 HTTPS healthz 正常；主设备 online，升级后两次心跳时间从 13:00:53 UTC 推进至 13:01:03 UTC。
- 30m/1h/24h 返回数据，节点数分别 180/360/360；节点数仅记录开发验收，不在页面展示。30m 仍按原始十秒采样，缺桶不补零。
- 历史半小时结束时间接口、备份状态接口通过。
- 公网 Edge 自动化通过：HTTPS 登录、三个范围、悬浮提示、图表放大、半小时/整小时历史段、Esc 关闭、退出后 API 401。
- 四张图 CPU、内存、根磁盘、网络的节点平均/峰值文本，逐项与实际线上接口返回的数据格式化值一致。未宣称覆盖所有历史节点或重新验证底层 Linux 原始计数。
- 最终 Hub、Agent、backup.timer、maintenance.timer 均 active；Hub Result=success、NRestarts=0，部署窗口无警告日志。

## 用户查看

访问 https://8.148.5.169/，刷新页面；如仍见旧样式使用 Ctrl+F5。Hub 重启会使原会话失效，使用原管理凭据重新登录。四图右上角新增放大按钮，Agent 不需要执行更新命令。

仓库中的 `scripts/deploy-chart-hub.py` 是该指定提交、哈希、服务器路径的一次性执行记录，不是通用自动升级命令，不应照抄用于其他版本。
