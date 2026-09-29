# Agent v0.1.0-rc.2 预发布清单

状态：2026-09-29 已本地构建，尚未公开发布，下载路径尚未验收。不得将下方发布后命令当作当前可用链接。

- 来源提交：548155832f4354cb7df187d44ff5bcb658ff7626。
- Windows 产物目录：`C:/Users/1/Desktop/SeeSize-agent-v0.1.0-rc.2`。
- Ubuntu/Debian、Linux amd64、systemd、Python 3 标准库；无需安装 SQLite。
- Go 全量测试通过（部分缓存）。安装器九项测试及更新流程的隔离实验见 AGENT-UPDATE-ACCEPTANCE.md。
- 更新范围：安装/更新工具；采集逻辑、协议、页面未改。现有已验证 Hub 无需更新，云端原 Agent 不需要为此替换。其他 Hub 构建需独立核对兼容性。
- 不含自动版本通知、自动兼容判断、静默更新。版本号是包版本，Agent 上报仍可能显示 0.1.0-dev。

## 发布步骤（仓库管理者在 GitHub 页面）

1. 仓库 Releases → Draft a new release。
2. 新建标签 `v0.1.0-rc.2`，目标固定为上述提交，不要误选后续 main。
3. 勾选预发布（pre-release），不要标记为稳定最新版。
4. 上传产物目录里的四个文件，保持名称不变：`agent-setup.py`、`agent-release.json`、`seesize-agent-linux-amd64`、`seesize-enroll-linux-amd64`。只有源码 zip 不够，安装器需要独立资产。
5. 说明中写明支持范围、Hub 不需更新、采集逻辑不变，以及仍待完成的在线下载验收。正式发布后告知维护者继续验证。

## 发布后在新的隔离 Ubuntu 机器验证

先在管理网页生成专用设备 ID 和一次性注册码。不要在云端主机或已有安装上执行 install。下载过程较慢时可等下载完成后再生成注册码。

```sh
mkdir -p ~/seesize-install-rc2
cd ~/seesize-install-rc2
curl --fail --location --proto '=https' --proto-redir '=https' -o agent-setup.py https://github.com/wxlys/See-size/releases/download/v0.1.0-rc.2/agent-setup.py
sha256sum agent-setup.py
```

脚本 SHA256 应为 `2d715ce962f3ab63b931710c4461a7217425ebbe504222a6b53d1c460dcad2c6`。检查脚本后执行：

```sh
sudo python3 agent-setup.py install --version v0.1.0-rc.2
```

按提示输入 HTTPS Hub 地址、设备 ID、`install` 确认、注册码。注册码不是管理凭据。网页应出现专用设备，观察至少三次十秒采样推进；服务状态 active，凭据权限 600，且原设备不受影响。下载失败不得用跳过 TLS 校验来绕过。

已有本安装器管理的测试设备不重复 install：先启动测试服务，再使用下载并审核的脚本执行 `update --version v0.1.0-rc.2`，确认要求后输入 `update`。不要套用到原手工安装的云端 Agent。

## 资产哈希

Agent：`769470ac46fa76f51d18cbdb0e18e826b4818a3090df5a32ca419245e7196d32`

Enroll：`15c82491fe766d126b74984c68a349cfd3735e34dfaac71dee1c32228c297df1`

哈希用于核对资产，不能替代独立签名或仓库访问控制。本次未创建标签、未公开发布、未更改线上服务。
