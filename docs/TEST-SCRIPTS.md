# 可重复验收与 24 小时挂测

仅在第一台测试机（SSH 别名 18）运行，不用于 wsrser。需要现有 Linux amd64 程序和 Python 3 标准库；无需额外安装 Python 包。脚本位于仓库 scripts/，已上传第一台 /opt/seesize-dev/。

## 短时独立测试

在本机终端执行（报告文件必须不存在）：

```powershell
ssh 18 "python3 /opt/seesize-dev/acceptance-smoke.py --out /opt/seesize-dev/smoke-run-02.json"
```

默认约十几秒：创建独立 Hub 和 Agent，使用临时回环代理断开测试请求，等待离线，再恢复连接。随后创建 1000 个 4 KiB 文件，按每秒 200 条扫描，检查大小及期间心跳。最多允许 2000 个文件；不关闭主机网络、不重启现有服务、不改写现有数据库。结束自动停止临时进程并删除临时目录，只保留 JSON 报告。进程被强制终止或主机断电可能来不及清理，应先确认没有测试进程后再处理临时目录。

这是连接故障模拟，不等于真实网络断开实验；1000 文件属于有限规模验证，不等于百万文件压力测试。

## 24 小时只读观察

在本机终端执行：

```powershell
ssh 18 "nohup python3 /opt/seesize-dev/soak-monitor.py --hours 24 --interval 60 --out /opt/seesize-dev/soak-24h-run01.csv > /opt/seesize-dev/soak-24h-run01.log 2>&1 < /dev/null &"
```

报告名每次换一个，避免覆盖；日志名也相应更换。每分钟采集一次，24 小时后自行退出。不应同时启动多份同用途观察脚本。检查进度：

```powershell
ssh 18 "tail -n 5 /opt/seesize-dev/soak-24h-run01.log"
ssh 18 "tail -n 5 /opt/seesize-dev/soak-24h-run01.csv"
```

下载报告到本机当前目录（请先确认本地没有同名文件）：

```powershell
scp 18:/opt/seesize-dev/soak-24h-run01.csv .
```

前台运行同样支持，Ctrl+C 或 SIGTERM 会结束观察；不会停止 Hub/Agent。后台版本未设置自动重启，服务器重启或观察进程退出会中断观察，需要重新开始实验。

## CSV 怎么看

- state 应保持 active；error 应为空。PID 和 restarts 变化表示发生重启，需要对照系统日志。
- rss_kib 为常驻内存，关注是否持续上涨；单次高低变化不直接判定内存泄漏。
- cpu_seconds 是进程累计 CPU 时间。相同 PID 的相邻行差值 / 时间差 × 100 得到单核口径 CPU 百分比；PID 变化后不能直接相减。
- db_bytes 和 wal_bytes 分开记录；WAL 合并时大小变化正常，数据库清理也不保证立即缩小文件。
- backup_bytes 是当前备份总大小，结合备份时间及保留份数判断。
- 每个采样时刻分别写 Hub/Agent 两行。CSV 只是观察原始数据，不自动声明 24 小时验收通过；还应核对历史接口的时间覆盖和告警日志。

## 已执行结果

2026-09-14 短时测试 PASS：连接中断后离线并恢复；1000 文件 / 4096000 字节，扫描约 5.018 秒，期间新增 5 个心跳。2026-09-15 已收到完整挂测 CSV：两服务各 1440 次采样，全部 active、无新增重启、错误栏为空。详细结论与剩余验收见 [验收计划](ACCEPTANCE-PLAN.md)；不能将基础挂测通过等同于全部功能验收通过。
