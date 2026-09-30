# Agent v0.1.0-rc.3 预发布清单

状态：已准备本地资产，尚未发布。目录：`C:/Users/1/Desktop/SeeSize-agent-v0.1.0-rc.3`。

## 组件与来源

- 标签目标：`86110b2dab059b1259fa14960dfd856cac02456d`，不要选择后续文档提交。
- 仅安装工具改动：下载进度、单资源 180 秒总预算、最多三次尝试、取消说明。
- Hub 无需更新；现有 Agent 无需强制更新。采集逻辑、协议和页面均未改变。
- 两个 Linux 程序直接复用已发布 rc.2 资产，SHA256 校验一致，没有重新编译。清单分别记录包源码提交及二进制来源提交 `548155832f4354cb7df187d44ff5bcb658ff7626`。两个提交之间 cmd/agent、cmd/enroll、internal、go.mod、go.sum 无差异。
- Windows 本轮测试 15 项通过、Linux 计时器专属测试跳过；Linux 计时器及下载故障模拟的前次结果见 AGENT-DOWNLOAD-HARDENING.md。本次完整 rc.3 在线更新尚未验证。

## GitHub 页面发布

1. 新建 Release，创建标签 `v0.1.0-rc.3`，Target 选择上述完整提交。
2. 标题 `v0.1.0-rc.3`，选择 Pre-release。
3. 上传目录中四个原名文件：agent-setup.py、agent-release.json、seesize-agent-linux-amd64、seesize-enroll-linux-amd64。
4. 不修改 rc.2，不标记稳定最新版。发布后再执行下载验收。

发布说明可用：

> 安装工具改进：显示下载进度和平均速度，每个资源总预算 180 秒，临时网络失败最多尝试三次，并提供清晰的取消提示。仅安装工具变化，复用 rc.2 已验证采集程序；当前已验证 Hub 和现有 Agent 无需强制更新。支持 Ubuntu/Debian amd64、systemd、Python 3。本改进不能消除网络连接重置，不包含自动版本提醒。rc.3 完整在线更新待发布后验收。

## 发布后验证

在本地 Ubuntu 虚拟机的新目录下载 rc.3 的 agent-setup.py，先核对 SHA256：

`0bce2483a0018981e2550184be42bdb98ec5996afd734100df62edeb470e56c3`

现有测试安装应执行下载的新脚本 `update --version v0.1.0-rc.3`，不是 install。不要使用已安装的 rc.2 旧脚本来验收新下载提示，因为下载发生在旧脚本被替换之前。确认前有下载进度；网络失败应在预算内退出，原设备继续上报。成功后输入 update 才替换并验证心跳；观察原设备一分钟，无重复设备且历史时间推进。

首次联网验收不要加 --bundle；网络失败仍可离线更新，但两者必须分开记录。不得用忽略 TLS 证书或第三方不可信镜像解决下载失败。
