# 升级准备工具（尚不执行版本切换）

适用 Linux x86_64、Python 3、systemd 用户服务的现有部署。脚本为 `scripts/upgrade-prepare.py`。先执行维护规程中的 deployment-check，解释所有警告并解决阻断项。

## 行为与边界

- 默认仅校验并输出计划，不写文件、不执行候选程序、不重启服务。
- `--prepare` 保存三种旧程序、四个相关服务/定时器的完整配置文本、指定候选程序，并调用现有备份程序生成验证过的数据库快照。
- 生成专属 `pre-版本标签` 目录，成功后登记 `maintenance-artifacts.json`，由维护策略保留最新三个回退点。这不同于每日数据库备份的七份保留策略。
- 要求专属应用目录禁止组/其他用户访问、候选 ELF 为 amd64、可信构建的 SHA256 匹配、预估空间之外至少留有 2 GiB。
- 哈希只是完整性验证，不是软件签名；不能从不可信文件自身计算哈希后就认定来源可靠。ELF 头检查也不保证运行兼容性。
- 配置快照可能含敏感内容，只在私有目录保存，不应提交到仓库或公开分享。

## 执行位置与示例

以下在 **Linux 隔离虚拟机的终端**执行，不是在 Windows PowerShell 执行。先准备与现网相同结构的独立测试部署（包括三种程序、数据库、维护清单和用户服务），并将新候选程序和脚本复制到虚拟机。裸机仅有压测包时不能直接执行本流程。

示例路径 `/home/wsr/seesize` 为虚拟机上的测试部署；按实际路径修改。`HASH` 必须替换为可信构建产生的 64 位哈希，不能照抄占位文字。

```sh
python3 upgrade-prepare.py --root /home/wsr/seesize --release lab-upgrade-01 --candidate hub=/home/wsr/candidates/seesize-hub-linux-amd64 --sha256 hub=HASH
```

预期 `prepared: false`、`activate: false`，尚未生成回退目录。确认无误后，同一命令加 `--prepare`：

```sh
python3 upgrade-prepare.py --root /home/wsr/seesize --release lab-upgrade-01 --candidate hub=/home/wsr/candidates/seesize-hub-linux-amd64 --sha256 hub=HASH --prepare
```

预期 `prepared: true`、`activate: false`；目录包含旧程序、candidates、service-configs.json、release.json 和一个数据库快照。重复相同标签应拒绝，避免覆盖已有回退点。可重复传入 `--candidate` 和 `--sha256` 指定 agent 或 backup。

## 并发与文件生命周期

准备操作与新版 `storage-maintenance.py --apply` 共用 `.maintenance.lock`；竞争时直接失败，稍后重试。**部署新版维护脚本之前，这个锁不能约束旧维护脚本**，必须依照维护规程暂停维护定时器并等待已有任务结束。锁也不代替人工升级操作和备份定时器的协调。

普通失败会删除本次新建的未完成目录，不改现有数据库/程序。断电或强制终止可能留下未登记目录，应人工审查，不能假设会自动清除。候选输入文件不由本工具删除；确认快照中的候选哈希正确且不再需要输入副本后，由维护者清理明确的 staging 文件。不要不断创建未登记的 staging 目录。

## 验证状态与下一步

2026-09-27：本地测试覆盖候选格式/哈希、重复参数、只读预演、准备成功登记、备份失败清理及现有文件保持不变；systemd 和备份调用使用模拟，不能等同真实 Linux 端到端验收。维护脚本回归测试通过。

本轮未在 wsrser 部署该工具或新版维护锁，未创建线上回退包，未激活新程序。后续先在隔离虚拟机验证真实备份、准备前后服务状态与心跳、失败清理和锁冲突，再安排版本切换；不要为此重启业务服务器。
