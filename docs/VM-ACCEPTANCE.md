# 本地 Linux 虚拟机验收

2026-09-23 修正：初版脚本误传 `-timeout 90s`，超过扫描程序允许的 60 秒，导致首轮扫描直接退出。这不是性能验收失败。新版改为 60 秒（外层进程等待 75 秒），并记录失败命令的 stderr/stdout。旧报告保留，替换 acceptance-smoke.py 后用新报告名重新测试，二进制无需替换。10000 文件在 200 项/秒下约需 50 秒；若虚拟机额外开销导致超时，应如实记录不完整，不推断通过。

仅用于独立 Ubuntu/Debian x86_64 虚拟机，建议 2 vCPU、2 GiB 内存、至少 1 GiB 空闲磁盘。不在 wsrser 业务主机运行。无需安装 Go 或 SQLite 服务；需要 Python 3 标准库。Windows 本地先构建打包，再通过 SCP 复制到虚拟机。

## 打包（开发机）

安装 Go 1.27+，在仓库 PowerShell 7 执行：

```powershell
$env:GOOS='linux'
$env:GOARCH='amd64'
$env:CGO_ENABLED='0'
New-Item -ItemType Directory -Path vm-kit
go build -trimpath -o vm-kit/seesize-hub-linux-amd64 ./cmd/hub
go build -trimpath -o vm-kit/seesize-agent-linux-amd64 ./cmd/agent
go build -trimpath -o vm-kit/seesize-scan-linux-amd64 ./cmd/scan
Copy-Item scripts/acceptance-smoke.py vm-kit/
```

若已收到构建好的压缩包可跳过打包。构建输出不要提交 Git。将文件夹复制到虚拟机的专用目录，例如用户目录下 seesize-lab，不要混入真实数据库或凭据。

## 虚拟机运行

在放置文件的目录执行，报告文件名必须尚不存在：

```sh
python3 --version
uname -m
chmod 700 seesize-*-linux-amd64
python3 acceptance-smoke.py --root "$PWD" --files 1000 --out run-1000.json
python3 acceptance-smoke.py --root "$PWD" --files 5000 --out run-5000.json
python3 acceptance-smoke.py --root "$PWD" --files 10000 --out run-10000.json
```

逐条执行，上一轮 PASS 后再执行下一轮；出现明显卡顿或 FAILED 就停止增加规模。没有 Python 时，可在该虚拟机使用系统包管理器安装 python3；不需要第三方 Python 包。

每轮建立临时数据库、随机测试凭据和回环端口，启动独立 Hub/Agent。仅对临时代理模拟断连，不关主机网络。创建每个 4096 字节的小文件，扫描速率 200 项/秒；10000 文件约 39 MiB，扫描约 50 秒。退出时停止临时进程并清理临时文件，仅保留 JSON 报告。强制断电可能遗留临时目录，应先确认测试进程退出再清理。

## 验收证据和边界

- status=PASS：连接故障后离线、恢复；完整扫描大小正确，扫描期间心跳数量增加；限额扫描为不完整。
- directory_scan：文件数、逻辑大小、耗时、心跳增量、扫描子进程峰值 RSS（KiB）及累计 CPU 秒。
- 本脚本并非完整压力监测平台：不证明扫描期间从未出现短时心跳延迟，不测 Agent 内存长期趋势，也不验证限额快照上传后的告警行为。后者已有后端测试。
- 可另开终端使用 top 观察主机，勿以肉眼瞬时读数代替报告。
- 完成后回传三个 JSON 报告、虚拟机系统版本、核数/内存配置和异常现象。不提供任何真实凭据。

## 恢复实例人工验收（与虚拟机压力测试分开）

2026-09-22 在 wsrser 创建独立备份恢复实例，远端和本地转发均为 18086；地址 http://127.0.0.1:18086/。使用原管理凭据。为防止与 18082 同主机 Cookie 互相覆盖，建议用单独浏览器或无痕窗口访问。

检查：登录成功；服务器身份为 wsrser-main；历史趋势存在；各页面无报错；退出后 /api/v1/servers 拒绝访问。设备显示离线正常，因为实际 Agent 只向现用 Hub 上报。恢复实例不应做真实设备注册或撤销，不改变现用 Agent 地址。

临时服务最多运行 24 小时，未设置开机自启。提前结束：`ssh wsrser "systemctl --user stop seesize-restore-check-20260922"`。恢复文件保留在 /home/wsr/seesize/restore-check-20260922，未覆盖现用数据库。
