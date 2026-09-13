# SeeSize

SeeSize 是一个面向个人开发者和小型服务器集群的轻量资源监控与磁盘异常定位系统。

当前仓库包含第一个纵向原型：

- `seesize-hub`：接收 Agent 心跳，持久化历史指标，并提供内嵌 Web 总览与趋势图。
- `seesize-agent`：采集主机信息和基础资源指标并主动上报。
- `internal/model`：Hub 与 Agent 共用的协议模型。
- `internal/collector`：Linux 指标采集及其他平台的开发占位实现。

## 本地运行

要求 Go 1.27 或更高版本。

先启动 Hub：

```bash
go run ./cmd/hub -agent-token dev-secret -data .data/seesize.db
```

再启动 Agent：

```bash
go run ./cmd/agent -hub http://127.0.0.1:8080 -token dev-secret -interval 10s
```

打开 `http://127.0.0.1:8080` 查看服务器列表。默认只监听本机地址；当前共享 Token 只用于第一轮链路验证，后续将替换为一次性注册码和每台设备独立凭据。

历史指标接口：`GET /api/v1/servers/{agentID}/metrics`。可选参数 `from`、`to` 使用 RFC3339 时间，`limit` 范围为 1–5000；默认返回最近一小时数据。

## 目录空间扫描（手动触发）

在被监控服务器运行配套的 `seesize-scan`，指定目录与已有 Agent ID。扫描只读取目录和文件大小，不读取文件内容。上报凭据可通过环境变量 `SEE_SIZE_AGENT_TOKEN` 提供。

```bash
seesize-scan -root /var/log -id your-agent-id -hub http://127.0.0.1:8080 -depth 3 -max-entries 20000 -timeout 15s
```

不提供 `-hub` 时，仅输出本地 JSON。构建入口为 `go build ./cmd/scan`。面板的“目录空间分析”展示最近扫描及两次完整扫描的增量。默认不定时扫描，当前显示每台服务器最近扫描根目录的最后两次结果。统计逻辑大小，符号链接不跟随，硬链接按路径计数；因此结果不等于 `df` 的物理占用。数量或软时间预算耗尽时保留部分结果并停用增量判断。当前未实现 IO 限速，建议先扫描较小目录。

数据清理、备份及进一步的扫描优化见 [后续优化](docs/ROADMAP.md)。

## 历史保留

Hub 默认保留最近 7 天的指标与磁盘快照。可使用 `-retention-days 1` 改为 1 天（范围 1–365 天）。启动时及每 10 分钟检查一次，可用 `-cleanup-interval 10m` 调整。每批每表最多删除 1000 条，单轮最多运行 30 秒；积压数据在后续轮次继续清理。离线服务器清单不会删除。过期以采集／快照时间为准，需要服务器时钟准确。

清理是永久删除，不会自动备份；缩短保留期限前请确认数据需求。SQLite 删除后空闲页供后续写入复用，文件不会立即缩小；目前不自动执行整库压缩。到期数据可能在下次清理前短暂保留。备份、降采样尚未实现。

## 验证命令

```bash
go test ./...
go vet ./...
```

## 当前边界

- Linux Agent 能读取 `/proc`、`/etc/os-release` 和根文件系统容量。
- Windows 等非 Linux 平台只能用于开发联调，上报主机名、操作系统和网络地址。
- Hub 使用本地 SQLite 持久化服务器状态和原始历史指标，重启后数据保留。
- 已支持手动目录扫描、快照保存与完整快照增量比较。
- 分层降采样与保留策略、用户登录、定时扫描和异常告警将在后续迭代加入。
