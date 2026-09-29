# 采集端安装与版本交付（开发中）

2026-09-29：开始安装交付阶段。用户冻结采集指标、协议和已验收页面，本轮不改 cmd/agent、cmd/enroll、Hub 或页面逻辑。

## 当前实现

- scripts/agent-setup.py：Ubuntu/Debian x86_64 + systemd，Python 3 标准库，不安装 SQLite。
- install：要求明确发布版本；从固定 wxlys/See-size GitHub Releases 下载清单与两个程序，限制下载大小、检查 ELF/哈希。也支持可信本地 --bundle 供隔离测试。
- 先下载后输入 HTTPS Hub、设备 ID，明确确认 Hub 兼容要求，再建立独立系统账号 seesize-agent。注册码隐藏输入，经环境变量交给注册工具，不写命令行/日志；本地 root 或同账号进程不属于此方式可防御的边界。
- 程序路径 /opt/seesize-agent，凭据 /var/lib/seesize-agent/agent.token，系统级 seesize-agent.service。低权限运行、文件系统只读保护、服务自启，不修改防火墙、不重启主机。
- 已有目录、账号、系统服务拒绝覆盖/接管。注册失败保留安装文件，register 动作可重新输入新码；非空凭据不覆盖，避免重复消费注册码。注册响应丢失等情况应重新生成注册码。
- check-update --version：指定目标版本的只读查询，打印安装包版本和 Hub 要求，不自动判断最新版，不安装、不重启。
- scripts/package-agent-release.ps1：干净源码构建 Agent/enroll，输出 agent-release.json 和安装器，不创建 tag 或发布 GitHub Release。

发布包版本由安装清单记录，现有 Agent 上报可能仍显示 0.1.0-dev，不修改其采集协议。SHA256 检查传输完整性，清单信任来源是固定仓库的 HTTPS 发布，不是独立签名；仓库被入侵的风险不能靠同来源哈希消除。

## 目前不能宣称已完成

七项本地测试覆盖 URL/设备 ID/版本校验、清单与 ELF 哈希、拒绝 HTTP 降级、版本检查无服务操作。2026-09-29 补充真实 root 安装、错误注册码重试、低权限 systemd 与 HTTPS 上报短时验收通过，详见 [安装验收记录](AGENT-INSTALLER-ACCEPTANCE.md)。卸载、真实重启自启、网络下载发布路径及其他安装中断恢复尚未验证。

尚无公开可用的发布版本/一键下载命令。不得把示例 v0.1.0 当作已发布。确认式升级、兼容性自动判定、失败回退、最新版发现与推送提醒尚未实现。不得对外称“一键升级已完成”。

## 下一阶段测试顺序

1. 固定干净提交，构建本地发布包；首先在干净隔离虚拟机测试，不能在云端 wsrser 覆盖原安装。
2. 从 HTTPS 管理页创建专用测试 ID，虚拟机 install 输入 HTTPS 地址与注册码，确认 Hub 在线和样本推进。
3. 测试错误/过期注册码重试、已有路径保护、哈希错误、缺少权限、失联。验证注册码/凭据不出现在日志；系统服务与原用户服务分别识别。
4. 确认账号权限和目录权限、自启（真实重启需授权），再设计卸载：停止专属服务，凭据是否删除另行确认，Hub 历史不自动删除。
5. 升级流程明确组件、Hub 最低兼容要求、版本选择、输入 update 确认、凭据保留和故障回退后，才实现实际替换。
6. Linux 验收通过后，发布指定版本资产与安装指令。使用者自行下载审阅脚本后 sudo 执行，不提供未经审查的 curl | sudo sh。

## 尚未发布的本地包测试调用形式

在一次性虚拟机、发布包目录中（不是现有线上主机），未来完成预检查后使用：

```sh
sudo python3 agent-setup.py install --version v0.1.0 --bundle /absolute/path/to/test-bundle
```

版本须与清单一致。失败注册重试的形式：

```sh
sudo python3 /opt/seesize-agent/agent-setup.py register
```

若失败发生在服务文件已建立之后，register 会拒绝重复配置；应先人工检查 systemctl status seesize-agent 与文件权限，不能删除有效凭据后盲目重注册。安装中断后的完整清理/恢复流程仍在待验收范围。

## HTTPS 接入基线

用户于 2026-09-28 在 https://8.148.5.169 验证登录、指标及退出；正式 IP 证书申请和指定证书续期 dry-run 成功，Nginx reload 钩子已配置，snap.certbot.renew.timer 已安排执行。真实定时续期仍需观察。该网页验收不代替另一台服务器注册和 HTTPS 上报实验。
