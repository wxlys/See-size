# 演示与维护交接清单

## 演示前：本地 Windows PowerShell 7

```powershell
cd 'F:\procedure\codex\program\See-size'
.\scripts\preview-control.ps1 -Action Status
```

预期 Managed=True、Listening=True、Health=HTTP 200。没有连接时执行 Start，异常时 Diagnose。浏览器打开 http://127.0.0.1:18082/，使用已有管理凭据，不把凭据放入截图、录像或论文。

当前用户已启用登录自启。下次正常重启登录后，在没有手动 Start 的前提下检查 Status 和页面，成功后补录实际自启验收；不必专门重启，更不要重启云服务器。

## 5—8 分钟演示顺序

1. 介绍小型 Linux 主机监控、资源历史追踪和受控目录空间分析的目标。
2. 展示主机、在线状态与指标更新。说明当前正式面板一台在线，多节点有历史联调证据，不虚构节点。
3. 切换 30 分钟、1 小时、24 小时，展示坐标、悬浮均值/峰值，解释实际起止和缺口。
4. 用已有隔离测试结果展示目录变化和完整性。线上无数据时明确无记录，不临时扫描业务全盘制造数据。
5. 展示备份状态，说明七天历史、七份全库快照、三个回退点的区别。
6. 退出后确认权限失效，再登录。不为演示撤销或删除真实 wsrser-main。
7. 展示 24h 基础挂测、1k/5k/10k 受控扫描、升级回退结果，注明实验日期和边界。

## 日常只读检查：本地 PowerShell

```powershell
ssh wsrser "systemctl --user is-active seesize-hub seesize-agent"
ssh wsrser "systemctl --user list-timers seesize-backup.timer seesize-maintenance.timer --no-pager"
ssh wsrser "systemctl --user show seesize-backup.service seesize-maintenance.service -p Id -p Result -p ExecMainStatus"
```

| 现象 | 优先检查 | 不要先做 |
| --- | --- | --- |
| 18082 拒绝连接 | 本地 Status/Diagnose、自启、SSH 网络 | 重启云服务器 |
| 需要登录 | 会话到期/Hub 重启，重新登录 | 重置设备身份 |
| 主机离线 | Agent 状态、心跳、网络 | 删除节点隐藏离线 |
| 历史不足 24h | 实际数据、范围和缺口 | 补零伪造连续数据 |
| 备份/维护失败 | 服务 Result、磁盘、日志 | 删除现库/WAL/SHM |

升级先 Hub 后 Agent，整组回退先 Agent 后 Hub；先保存回退包并协调维护任务。scripts 中固定路径、固定哈希的一次性演练/发布脚本不能直接用于另一环境。旧 ssh 18 永不再连接。

## 交接结论

- 已确认核心功能、本轮浏览器交互、线上自动检查、转发恢复。
- 待自然事件验证：Windows 重启登录自启。
- 不扩展公网部署、额外业务主机常驻、规模上限；论文和答辩材料另行准备。
- 不在收尾时盲删回退点；服务器按登记策略轮换，本地实验材料保留为证据。

推荐声明：当前限定范围版本交付完成，已知限制与后续场景验收单独列示。不要写“所有场景验证完毕”或“占用永远不增长”。
