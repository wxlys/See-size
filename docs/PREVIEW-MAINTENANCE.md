# 本地预览转发管理（Windows PowerShell 7）

脚本 scripts/preview-control.ps1，默认 wsrser:18081 → 本地 127.0.0.1:18082。仅管理本地 SSH 进程，不启动/停止远端服务，也不修改 SSH 配置。可显式启用当前 Windows 用户登录自启，不安装系统服务、不存储密码。

在仓库目录运行：

```powershell
./scripts/preview-control.ps1 -Action Start
./scripts/preview-control.ps1 -Action Status
./scripts/preview-control.ps1 -Action Diagnose
./scripts/preview-control.ps1 -Action Stop
./scripts/preview-control.ps1 -Action EnableAutoStart
./scripts/preview-control.ps1 -Action DisableAutoStart
```

Start 启动隐藏的监督进程。重复启动同配置返回已管理，不再创建新连接；相同端口但不同目标拒绝。端口被未知进程占用则拒绝接管，不能自动杀掉该进程。旧 preview.ps1 仍保留用于前台运行，但不要与新工具同时使用同一端口。

Status 分别报告 Managed（监督进程存在）、Listening（端口监听）和 Health（本地 healthz），不是仅凭 PID 判断服务可用。Diagnose 显示最近连接退出/重试记录并向目标回环 healthz 做只读检查。进程身份按 PID、启动时间、命令行匹配，Stop 只停匹配的管理进程及 SSH 子进程，不按进程名批量终止。

状态与日志保存在 `%LOCALAPPDATA%\SeeSize\preview`，每个端口一份状态文件与最多 200 条事件日志，记录启动/退出/重试而非无限追加。锁文件是固定小文件；不同端口记录不会按天增殖。日志没有保存管理凭据。SSH 原始详细 stderr 未收集，不能替代所有认证错误诊断；可根据 Diagnose 的终端错误进一步排查。

SSH 退出（包括正常退出）后等待 5 秒重新启动；保活 30 秒、连续 3 次无响应可退出，具体恢复耗时受网络影响。监督进程自身被结束后不会自行复活，应再次 Start；若监督进程异常死亡留下孤立 SSH，工具会拒绝盲目接管，使用 Diagnose 确认身份后人工处理。

## 登录自启动与诊断

2026-09-29 补录：用户实际重启本地电脑后，反馈公网 HTTPS 和本地 18082 均可访问。本地重启访问验收通过（用户反馈）；下述保存命令测试记录保留原始时点。不代表重启过云服务器。

EnableAutoStart 在 HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run 写入唯一值 SeeSizePreview-端口，命令使用当前 PowerShell 7 可执行文件和脚本的完整路径，隐藏窗口执行 Start。只在当前用户登录后生效，不是无人登录的开机服务。登录时网络未就绪，监督进程会继续重试 SSH。首次启用后需要 Start 立即恢复访问。

DisableAutoStart 只移除匹配配置的本工具条目，不停止现有连接。Stop 只停止当前连接，不取消下次登录自启；永久停用需两者都执行。对同名但不同内容的注册表值拒绝覆盖/删除。不修改其他启动项。仓库移动或 PowerShell 路径失效需重新配置；Windows 启动应用禁用策略也可能阻止启动。

Status 增加 AutoStartConfigured（注册表项存在）、AutoStartMatches（与本次脚本/目标一致）、LastBoot 和 StatePredatesBoot（保存进程早于本次开机）。这些不代表已完成真实重启验收。Diagnose 在进程不存在且状态早于本次开机时明确提示原因。监督异常记录错误；强制结束、关机等可能来不及写退出日志。

## 2026-09-28 故障与修复

系统事件显示本机 13:16 关机、19:41:26 开机；旧状态是 9 月 27 日监督进程。故障时 Managed=False、Listening=False，远端 healthz=200。原因是电脑关机终止本地转发，而旧版未配置登录自启，不是远端服务定时关闭。

已恢复 18082，并为当前用户启用 SeeSizePreview-18082 登录启动项。临时 18093 注册表条目启用/重复启用/禁用/重复禁用测试通过，已移除。直接执行实际保存的登录启动命令可恢复 18082，Health=HTTP 200；这不等于已执行注销或重启验证。不应为此重启云服务器。

独立 18093 转发的 SSH 子进程经 PID/启动时间/命令行核对后主动终止，七秒后监督进程已重连，Health=HTTP 200；随后停止临时转发。最终 18082 Managed=True、Listening=True、Health=HTTP 200、AutoStartConfigured=True、AutoStartMatches=True。线上服务未重启。

## 2026-09-25 验证

- 独立本地 18083：启动返回健康 200，重复 Start 未重复连接，Stop 后端口释放。
- 18082 原旧脚本占用时新工具拒绝接管；核对旧监督与 SSH 命令行、父子关系后人工迁移到新工具。当前 Managed=True、Health=HTTP 200。
- 18083 主动停止测试 SSH 子进程，验证监督重试后恢复；未重启任何远端服务。
- 日志及状态保留在本机，未提交仓库。远端原有业务端口不变。
