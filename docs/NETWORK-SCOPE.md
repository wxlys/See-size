# 网络统计口径

2026-09-24 已部署 wsrser：核对默认路由 eth0 后，先升级 Hub，再升级 Agent 并启用 network-eth0.conf。一致性备份及旧程序保留在 /home/wsr/seesize/pre-network-20260924。首次样本 rate_unavailable=true，后续样本 scope=selected、interfaces=[eth0] 且有正常速率；Hub/Agent 与原有 Docker/site_total 均 active。Go 测试、静态检查、Linux 构建及现有浏览器回归通过。真实故障网卡场景仅由隔离逻辑测试覆盖，未在业务机改变网卡。

Agent 新参数 `-network-interfaces eth0` 或 `-network-interfaces eth0,eth1` 显式选择接口。名称以目标 Linux 的实际网卡名为准，不修改网卡、路由或业务配置。重复名称去重，lo 和非法空项拒绝。

未配置时保留旧行为：合计全部非 lo 接口，可能含 Docker 网桥、veth 等重复流量。这是兼容默认值，不是公网带宽；建议在明确拓扑后选择指定接口。物理接口也可能承载内网流量，不能宣称全部为公网。

每个接口独立计算相邻计数差 / 时间差，再合计速率。新接口、消失后重新出现、任一方向计数倒退时重新建立基线，该接口当次速率为 0，不把新累计计数当成瞬时流量。相同名称的接口在两个采样间被替换且计数未倒退，当前无法辨认；不是所有拓扑变动都能检测。

上报新增 scope、interfaces、missing_interfaces、rate_unavailable，页面展示当前口径。指定接口缺失时继续上报其他系统指标，网络仅包含实际存在的选中接口，并提示合计不完整；读取失败标记 unavailable。首次基线、接口变化、计数倒退或缺失标记速率不可用，不是确认无流量。趋势在桶内含任一不可用样本或口径混合时隐藏该桶网络值，其他资源不受影响；相邻桶口径不同则断线。此保守策略可能隐藏同桶其他有效网络样本。

历史样本不回写。未含新标记的历史保持旧数值，无法追溯识别旧异常。升级必须先 Hub 后 Agent：旧 Hub 严格拒绝未知 JSON 字段。wsrser 专属接口配置见 deploy/user/network-eth0.conf，使用前核对实际接口；不适用于任意主机。
