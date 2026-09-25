# 本地预览转发管理（Windows PowerShell 7）

脚本 scripts/preview-control.ps1，默认 wsrser:18081 → 本地 127.0.0.1:18082。仅管理本地 SSH 进程，不启动/停止远端服务，也不修改 SSH 配置。不创建开机或登录自启任务。

在仓库目录运行：

```powershell
./scripts/preview-control.ps1 -Action Start
./scripts/preview-control.ps1 -Action Status
./scripts/preview-control.ps1 -Action Diagnose
./scripts/preview-control.ps1 -Action Stop
```

Start 启动隐藏的监督进程。重复启动同配置返回已管理，不再创建新连接；相同端口但不同目标拒绝。端口被未知进程占用则拒绝接管，不能自动杀掉该进程。旧 preview.ps1 仍保留用于前台运行，但不要与新工具同时使用同一端口。

Status 分别报告 Managed（监督进程存在）、Listening（端口监听）和 Health（本地 healthz），不是仅凭 PID 判断服务可用。Diagnose 显示最近连接退出/重试记录并向目标回环 healthz 做只读检查。进程身份按 PID、启动时间、命令行匹配，Stop 只停匹配的管理进程及 SSH 子进程，不按进程名批量终止。

状态与日志保存在 `%LOCALAPPDATA%\SeeSize\preview`，每个端口一份状态文件与最多 200 条事件日志，记录启动/退出/重试而非无限追加。锁文件是固定小文件；不同端口记录不会按天增殖。日志没有保存管理凭据。SSH 原始详细 stderr 未收集，不能替代所有认证错误诊断；可根据 Diagnose 的终端错误进一步排查。

SSH 退出（包括正常退出）后等待 5 秒重新启动；保活 30 秒、连续 3 次无响应可退出，具体恢复耗时受网络影响。监督进程自身被结束、电脑重启/注销后不会自行复活，应再次 Start；这是没有配置系统自启的明确边界。若监督进程异常死亡留下孤立 SSH，工具会拒绝盲目接管，使用 Diagnose 确认身份后人工处理。

## 2026-09-25 验证

- 独立本地 18083：启动返回健康 200，重复 Start 未重复连接，Stop 后端口释放。
- 18082 原旧脚本占用时新工具拒绝接管；核对旧监督与 SSH 命令行、父子关系后人工迁移到新工具。当前 Managed=True、Health=HTTP 200。
- 18083 主动停止测试 SSH 子进程，验证监督重试后恢复；未重启任何远端服务。
- 日志及状态保留在本机，未提交仓库。远端原有业务端口不变。
