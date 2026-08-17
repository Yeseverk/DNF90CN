# 配置说明

普通用户只编辑：

```text
runtime/config/instance.json
```

第一次运行时，如果该文件不存在，控制器会从 `deploy/templates/instance.example.json` 创建它，并自动填入安装 ID 和管理口令。发布模板中的 MySQL root 密码固定以明文保存。

控制器据此生成：

```text
runtime/config/mysql.ini
runtime/configs/dnfbridge.toml
runtime/configs/dnf/logic.toml
runtime/configs/servergroup/plan.json
```

`mysql.ini` 是本包数据库进程配置，另外三份是游戏服务实际使用的运行配置。源码目录不保留旧的整套 `configs`。

## 主要字段

| 字段 | 说明 |
|---|---|
| `installationId` | 首次运行自动生成，用于绑定 MySQL 数据目录和进程状态。 |
| `mode` | 当前固定为 `local-single-account`。 |
| `server.advertiseIp` | 动态频道目录与全部游戏端口使用的 IPv4；默认自动探测，也可显式配置本机活动网卡的私有 LAN IPv4。不能使用 `127.0.0.1`、`0.0.0.0` 或公网地址。 |
| `server.channelListen` | 90CN 启动入口/频道下载监听，固定为 `127.0.0.1:7001`。 |
| `server.adminListen` | 健康检查与管理监听，默认 `127.0.0.1:18111`。 |
| `server.accountId` | 当前服务进程激活的内部账号 ID。新实例自动生成；使用 `LOGIN.bat` 登录时由控制器切换，不要手工固定为 `dnf:1`。 |
| `server.packetLog` | 是否写封包日志；普通运行建议保持 `false`。 |
| `database.mode` | 当前只允许 `portable`。 |
| `database.host` / `database.port` | 本包 MySQL，默认 `127.0.0.1:13306`。 |
| `database.user` | 当前本地 profile 固定为 `root`。 |
| `database.password` | 明文 root 密码；当前源码一键版固定为 `aa123456`。MySQL 仅监听 loopback。 |
| `database.name` | 当前唯一读写库，固定使用 `dnf_local`。 |
| `game.pvfPath` | 90CN profile 固定为 `data/dnf/Script.pvf`。 |
| `game.channelInfoPath` | 固定为 `data/dnf/channel_info.etc`。 |
| `protocol.profile` | 固定为 `90cn-decode-bypass-v1`。 |
| `client.directory` | 兼容客户端目录；留空时不启动客户端。 |
| `client.initialGamePort` | 当前 90CN profile 固定为 `0`。 |
| `client.hookCreate` | 当前 90CN profile 固定为 `true`。 |
| `build.goExecutable` | 仅 `REBUILD.bat` 使用，默认 `go`。 |

地址分工是当前 90CN 客户端的兼容要求：`server.channelListen`、`server.adminListen` 和 MySQL 保持 loopback；动态频道目录中的地址及其派生的全部游戏监听使用 `server.advertiseIp`。游戏监听必须只绑定这个本机私有 LAN IPv4 对应的接口，不能改成 `0.0.0.0` 或公网地址。`client.initialGamePort` 必须保持 `0`，客户端先连接 `127.0.0.1:7001` 下载频道目录，再按目录中的私有 LAN IPv4 连接游戏端口。

不要直接修改生成的 TOML、JSON 或 `mysql.ini`。需要修改 `instance.json` 时先运行 `STOP.bat`；控制器会拒绝在 MySQL 或游戏服务仍运行时套用漂移配置，停服后的下一次 `START.bat` 才会重新生成运行文件。

## 登录账号与客户端会话

`LOGIN.bat` 使用独立的 MySQL 表 `dnf_login_accounts` 保存登录账号。每条记录把用户输入的登录名映射到现有 DNF 业务表使用的内部 `account_id`：

- 第一个注册账号接管 `instance.json` 当前的 `server.accountId`，因此升级前 `dnf:1` 下的角色不会丢失；
- 后续注册账号获得独立、随机的内部账号 ID，角色、背包、任务等继续由原业务表按 `account_id` 隔离；
- 密码只保存 bcrypt 哈希，控制器只从标准输入读取密码，不把密码写入命令行或日志；
- 勾选“记住账号密码”后，登录器把凭据交给 Windows 凭据管理器，不写入 `instance.json` 或项目目录。

登录器认证账号后启动客户端，并通过受保护的 loopback 管理端登记 `DNF.exe` 进程 ID 与内部 `account_id`。服务端接受游戏连接时通过 Windows TCP 所属进程解析客户端 PID，把所有角色、背包、任务、账号仓库等仓储访问绑定到该会话账号，因此一个本地服务进程可以同时承载多个不同账号。`server.accountId` 只保留为直接启动客户端或缺少登录器登记时的兼容回退。

## MySQL 初始化与凭据

`database.mode="portable"` 时，控制器只管理包内 MySQL：

1. 校验 `deploy/assets/mysql-portable.json` 与官方 ZIP；
2. 解压到 `runtime/mysql/server`；
3. 在空的 `runtime/mysql/data` 上执行 `mysqld --initialize-insecure`；
4. 启动仅监听 loopback 的 MySQL；
5. 设置 `instance.json` 中的 root 密码；
6. 创建 `dnf_local`，验证 root 身份和临时表读写；
7. 保存安装与数据目录所有权状态。

该流程只在新的空数据目录上初始化。控制器不会接管一个缺少有效所有权状态的非空数据目录，也不会为了修复配置错误自动删除旧数据。

明文 root 密码是用户明确选择的本机测试端配置方式。该固定值会出现在发布模板和首次生成的本地配置中，因此只能用于 `127.0.0.1` 本地测试，不能复用于公网、局域网数据库或其他系统。`runtime/config/instance.json`、`runtime/configs`、`runtime/config/mysql.ini` 和运行状态仍由 `.gitignore` 排除，不应随角色数据库一起公开。

手工修改已初始化实例的 root 密码会让配置与数据库实际凭据不一致。需要修改时，应先做好一致性备份，再通过明确的数据库管理流程同时更新 MySQL 和 `instance.json`；不要只改一处。

## 进程内异步配置

本地 profile 使用进程内 worker 和内存型 bus、cache、lock、presence。它们不需要独立连接配置。关键资产仍由 MySQL 仓储事务同步持久化，内存异步组件不作为数据备份或跨进程队列。

当前配置不提供多节点异步参数。若未来增加跨节点任务、持久消息队列或共享锁，应作为新的部署 profile 设计，不能在本地 profile 中悄悄增加外部依赖。
