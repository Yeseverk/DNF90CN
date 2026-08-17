# Windows 本地部署

## 交付目标

默认方案是 Windows 原生服务端 EXE、包内官方 MySQL 8.4.10 和纯 BAT 控制入口。完整目录解压后即可启动，不要求 Docker、Redis、PowerShell 脚本环境或联网下载数据库。

本地 profile 固定为：

- Windows 10/11 x64；
- 单机、可注册多个账号、一个 DNF90Server 进程可同时承载多个已由登录器绑定的客户端账号；未登记进程才回退到实例兼容账号；
- MySQL 只监听 `127.0.0.1:13306`；
- 唯一读写库为 `dnf_local`；
- 游戏仓储使用明文 `root` 凭据；
- 90CN 启动入口/频道下载与管理端只监听 `127.0.0.1`；
- 动态频道目录和全部游戏端口使用自动探测或显式配置的本机私有 LAN IPv4，并只绑定该网卡接口；
- 异步任务、事件、缓存和锁在服务进程内运行；
- 角色、背包、装备、金币、任务等关键资产同步持久化到 MySQL。

## 系统边界

发布包已包含预编译的四个 Go EXE（包括原生 Windows 登录器）、官方 `mysql-8.4.10-winx64.zip`，以及 MySQL 直接依赖的 Microsoft x64 应用本地运行库 DLL。普通启动不需要安装 Go。

控制器会先按清单校验这些 DLL，再复制到 `runtime/mysql/server/bin` 与 `mysqld.exe` 同目录，并实际执行 `mysqld.exe --version` 验证加载。因此不要求目标电脑预装 VC++ 运行库，也不会安装或替换系统目录中的 DLL。

建议把项目解压到空间充足、当前用户可写的本地磁盘目录。MySQL ZIP 约 268 MiB，首次解压、系统表和后续角色数据还需要额外空间。不要从压缩软件的临时预览目录直接运行 BAT。

## 第一次启动

双击：

```text
START.bat
```

控制器按以下顺序执行：

1. 创建 `runtime/config/instance.json`；
2. 生成安装 ID和管理口令，并载入发布模板固定的明文 root 密码；
3. 自动探测本机活动网卡的私有 LAN IPv4；需要时可在实例配置中显式指定；
4. 校验包内 MySQL ZIP 的大小与 SHA256；
5. 安全解压到临时目录，校验关键文件后原子安装到 `runtime/mysql/server`；
6. 校验并安装包内应用本地运行库，然后执行 MySQL 版本检查；
7. 生成只监听 loopback 的 `runtime/config/mysql.ini`；
8. 对空的 `runtime/mysql/data` 执行 `mysqld --initialize-insecure`；
9. 用 `--no-monitor` 启动单一 MySQL 服务进程，并强制核对控制器状态 PID、`mysql.pid` 与监听 PID；
10. 核对 server UUID、端口和数据目录，设置 root 密码并创建 `dnf_local`；
11. 验证数据库身份和临时表读写；
12. 校验 PVF、频道表和已配置的客户端文件；
13. 启动 DNF90Server，并等待 `/healthz/ready` 和全部监听端口通过检查。

窗口显示以下内容后，才代表服务端可以接收客户端：

```text
DNF90 is READY.
```

首次解压和初始化耗时明显高于以后启动。控制器等待服务端读取 PVF 的上限为 120 秒，不会在几秒后误判失败。

## 后续启动与停止

再次执行 `START.bat` 时，控制器会校验并复用：

- `runtime/mysql/server` 中已解压的 MySQL；
- `runtime/mysql/data` 中的原有角色和业务数据；
- `runtime/config/instance.json` 中的安装 ID 与明文凭据。

`STOP.bat` 会先停止 DNF90Server，再安全关闭本包 MySQL。它只删除已停止进程的运行状态，不删除：

```text
runtime/mysql/server
runtime/mysql/data
runtime/config/instance.json
```

因此正常停止后再次启动应继续使用原有角色数据。不要为了普通排错删除 `runtime/mysql/data`。

## 客户端

先双击 `INSTALL_CLIENT_PATCH.bat`，输入已有 DNF 客户端目录。安装器只复制本项目编译的 `client-patch\bin\90CN.dll`，不包含也不分发 DNF 商业客户端本体。

编辑：

```text
runtime/config/instance.json
```

把 `client.directory` 改为兼容客户端的绝对目录，然后双击：

```text
LOGIN.bat
```

第一次使用先注册登录账号。首个账号会继承当前实例已有的角色数据，以后注册的账号使用独立内部账号 ID。输入账号密码并登录后，登录器会确保服务端 ready、检查客户端兼容清单，再以 `127.0.0.1:7001` 作为启动入口/频道下载地址、以固定值 `0` 作为初始 game port，并带上 outer token 与 hook 环境启动客户端。控制器会把新客户端的 Windows 进程 ID 与本次认证账号登记到服务端；客户端取得动态频道目录后，会按其中的本机私有 LAN IPv4 连接游戏端口。

登录器提供账号 1/密码 1 和账号 2/密码 2 两组输入，Tab 可在输入框、复选框和按钮间按顺序切换。勾选“记住两组账号密码”会把两组凭据分别保存到当前 Windows 用户的凭据管理器。数据库只保存 bcrypt 密码哈希；项目配置、日志和命令行都不保存登录密码。`LAUNCH_CLIENT.bat` 仍可用于直接启动兼容回退账号，但不执行账号认证或进程账号绑定。

双开不同账号的客户端：

1. 在账号 1 区域输入第一个已注册账号及其正确密码，点击“登录账号 1”；
2. 在账号 2 区域输入第二个已注册账号及其正确密码，点击“登录账号 2”；
3. 分别等待登录器确认两个客户端通过 5 秒启动检查。

第二开使用当前 EXE 已核对的单实例互斥和现有窗口分支兼容。两个登录按钮都不会停止已 READY 的服务端；每次只认证对应输入账号、启动新进程并登记进程账号。登录器返回成功前会确认新进程至少存活 5 秒，避免把客户端提前退出误报为启动成功。

不同客户端通过各自的进程 ID 绑定登录账号。登录器不再提供隐藏或显示游戏窗口按钮；任务管理器会正常显示客户端进程。

## 监听端口

- 90CN 启动入口/频道下载：7001/TCP，仅 loopback
- 管理/健康检查：18111/TCP，仅 loopback
- 私有 MySQL：13306/TCP，仅 loopback
- 游戏：由 `channel_info.etc` 派生的全部端口，仅绑定 `server.advertiseIp` 指定的本机私有 LAN IPv4

游戏监听不能使用 `127.0.0.1`、`0.0.0.0` 或公网地址。这个本机私有 LAN IPv4 只是当前 EXE 完成动态频道连接所需的接口地址，并不把本地单机 profile 变成局域网或公网服务。

客户端自动选择 `crack` 频道时会使用其对应游戏端口。排错时不能只检查 7001；如果自动探测选中了 VPN、虚拟网卡或错误网卡，应停服后显式配置当前活动网卡的私有 LAN IPv4，再通过 BAT 启动。

## 数据与备份

MySQL 是唯一权威业务存储。建议在执行 `STOP.bat` 并确认 MySQL 已停止后，成对备份：

```text
runtime/mysql/data
runtime/config/instance.json
```

前者保存数据库，后者保存与该数据目录匹配的安装 ID 和明文 root 密码。只备份其中一项可能导致恢复后所有权或凭据校验失败。

86JP 一键包的 `inventory.db` 是 SQLite，不能作为本项目的 MySQL 数据目录使用。现有数据若要迁移，必须做表结构和字段级转换。

## 无外部缓存时的异步边界

本地单进程版本使用 Go worker、goroutine、channel 和内存队列完成异步分发，因此没有外部缓存服务也能异步运行。关键资产更新仍必须同步提交 MySQL，异步只用于进程内可延后的工作。

该方案的优势是部署依赖少、数据权威单一，适合开箱即用的本地测试端；限制是纯内存任务不能跨进程保留，也不提供多节点共享缓存、分布式锁或持久消息队列。

## 验收

每台目标电脑都应独立完成：

```text
LOGIN.bat
注册或登录账号
STATUS.bat
进入场景
STOP.bat
再次 LOGIN.bat
确认原有角色数据仍在
```

只有源码测试、EXE 构建、MySQL ZIP 或应用本地运行库校验通过，不代表目标电脑上的客户端兼容性和完整游戏链路已经通过。以该电脑的 `STATUS.bat`、日志和实际登录结果为准。
