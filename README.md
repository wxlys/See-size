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

## 验证

```bash
go test ./...
go vet ./...
```

## 当前边界

- Linux Agent 能读取 `/proc`、`/etc/os-release` 和根文件系统容量。
- Windows 等非 Linux 平台只能用于开发联调，上报主机名、操作系统和网络地址。
- Hub 使用本地 SQLite 持久化服务器状态和原始历史指标，重启后数据保留。
- 分层降采样与保留策略、用户登录、磁盘扫描和异常归因将在后续迭代加入。
