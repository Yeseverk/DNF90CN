# DNF90 Windows 本地测试端

这是一份从当前项目提取的独立、干净、可编译源码与本地运行包。目标是把目录解压到 Windows 10/11 x64 电脑后，直接双击 BAT 启动，不要求安装 Docker、Redis、PowerShell 脚本环境或 Go。

发布包已经包含：

- `DNF90Launcher.exe`、`DNF90Control.exe`、`DNF90Server.exe`、`DNF90Doctor.exe`；
- 官方 MySQL Community Server 8.4.10 Windows x64 ZIP；
- 经清单和 SHA256 校验的 Microsoft x64 应用本地运行库 DLL；
- 当前 90CN 服务端源码、客户端补丁源码与成品 DLL、PVF、频道表和兼容清单；
- 纯 BAT 的启动、停止、状态、重建、客户端补丁安装和客户端入口。

发布压缩包不会携带制作电脑上的 `runtime/config`、MySQL 数据目录、角色数据、密钥、日志或进程状态。第一次启动会在目标电脑生成一个全新的本地实例和空业务库；以后执行 `STOP.bat` 再启动会继续复用该电脑自己的角色数据。

## 最快启动

1. 把完整目录解压到可写位置，例如 `D:\DNF`。
2. 双击 `INSTALL_CLIENT_PATCH.bat`，输入已有 DNF 客户端目录。
3. 把同一客户端目录写入 `runtime/config/instance.json` 的 `client.directory`。
4. 双击 `LOGIN.bat`。
5. 第一次使用先注册账号，再输入账号密码登录；登录器会启动服务端和客户端。
6. 需要保存登录信息时勾选“记住账号密码”，凭据只保存在当前 Windows 用户的凭据管理器中。

第一次通过登录器启动服务时会比以后更久，因为控制器会：

- 校验包内 MySQL 8.4.10 ZIP 的大小和 SHA256；
- 把 MySQL 解压到 `runtime/mysql/server`；
- 校验并把 Microsoft 签名的 x64 运行库 DLL 放到 MySQL 的应用本地目录，不改系统目录；
- 用 `--initialize-insecure` 初始化私有数据目录；
- 自动生成安装 ID 和管理口令，并使用发布配置中固定的明文 MySQL root 密码；
- 设置 root 密码并创建唯一读写库 `dnf_local`；
- 生成运行配置，校验 PVF、频道表和客户端兼容文件；
- 启动 MySQL 和 DNF90 服务端，完成数据库读写、端口与 ready 检查。

整个过程不联网下载依赖，也不注册 Windows 服务。以后启动会复用已经解压的 MySQL 和原有数据目录。

## 常用入口

- `START.bat`：准备配置和 MySQL，完成预检，启动服务并等待 ready。
- `LOGIN.bat`：打开独立登录器，提供两组账号密码的注册、登录、记住凭据和客户端启动。
- `STOP.bat`：核对安装 ID、PID、进程创建时间、EXE 路径和 SHA256 后停止服务端与本包 MySQL；角色数据保留。
- `STATUS.bat`：检查 MySQL、服务进程、ready、监听端口和最近错误。
- `INSTALL_CLIENT_PATCH.bat`：把包内编译好的 `90CN.dll` 安装到已有客户端目录。
- `LAUNCH_CLIENT.bat`：校验客户端兼容文件后启动游戏。
- `REBUILD.bat`：源码修改后重新编译四个 Go 程序；只有这个入口需要安装 Go。
- `REBUILD_CLIENT_PATCH.bat`：从正式的 `client-patch` 源码重建 `90CN.dll`。
- `PACKAGE_RELEASE.bat`：发布者使用；重新编译四个 EXE，按白名单收集源码和资产并生成 ZIP。

普通启动使用发布包内的 EXE，不需要安装 Go。所有顶层入口和控制器均不调用 PowerShell。

## 为什么不需要 Redis

异步执行和 Redis 是两件事。这个本地端是单机、单进程模式，任务分发、事件发布、响应队列、缓存和锁都可以由 Go 进程内的 worker、goroutine、channel 与内存组件完成。

角色、背包、装备、金币、任务等关键资产以 MySQL 为权威存储。需要保证持久化的业务结果应同步提交 MySQL 后再确认成功；进程内队列只承担本次运行中的异步工作，不替代数据库，也不作为跨进程持久队列。因此当前开箱即用版本不启动、连接或要求 Redis。

这一方案适合本地测试端，但不等同于多节点生产架构。进程退出后，尚未完成的纯内存任务不会跨进程保留；多服共享缓存、分布式锁或持久消息队列应在未来的多节点版本中单独设计。

## 目录

```text
D:\DNF
├─ go-server\       DNF90 所需的 Go 源码
├─ client-patch\    90CN 客户端补丁源码与已校验 DLL
├─ deploy\          BAT 控制入口、兼容清单和官方 MySQL ZIP
├─ runtime\         当前电脑的 EXE、明文配置、MySQL 数据、PVF、日志和状态
├─ releases\        后续发布包输出
└─ docs\            架构、配置、部署和排错说明
```

源码目录不携带旧 `configs`。运行时由控制器生成实际需要的配置：

```text
runtime/config/instance.json
runtime/config/mysql.ini
runtime/configs/dnfbridge.toml
runtime/configs/dnf/logic.toml
runtime/configs/servergroup/plan.json
```

MySQL 文件分为两部分：

```text
runtime/mysql/server    首次启动解压出的 MySQL 程序
runtime/mysql/data      权威业务数据；STOP.bat 不删除
```

备份时至少保存 `runtime/mysql/data` 与 `runtime/config/instance.json`，并在 MySQL 已停止后复制。

## 当前边界

这是单机本地测试端，不是公网多人服。可以注册多个相互隔离的账号；登录器认证每次输入的账号后启动一个本机客户端，并按该 `DNF.exe` 的 Windows 进程 ID 绑定账号。一个 DNF90Server 进程可以同时服务多个本地账号，每个客户端使用自己的角色、背包、账号仓库和进度。首个注册账号会接管升级前实例的活动账号 ID，保留原有角色；以后注册的账号使用独立随机 ID。`server.accountId` 只作为未经登录器启动的兼容回退。

登录密码在 MySQL 中只保存 bcrypt 哈希。登录器提供两组独立的账号/密码输入和对应注册、登录按钮，Tab 可按控件顺序切换。勾选“记住两组账号密码”时，两组明文凭据分别由 Windows 凭据管理器按当前 Windows 用户保护，不写入项目配置、日志、命令行或发布压缩包；取消勾选并成功登录会删除该组已保存的凭据。

需要双开不同账号时，在账号 1 和账号 2 区域分别填写已注册账号及其密码，依次点击“登录账号 1”和“登录账号 2”。任一登录按钮在服务端已 READY 时都不会停服或切换全局账号。登录器为每个新进程设置当前 EXE 已核对的双开兼容状态，认证并绑定该进程的账号，在返回成功前确认新进程至少存活 5 秒。

90CN 使用双地址本地布局：启动入口/频道下载固定为 `127.0.0.1:7001`，管理端和 MySQL 也只监听 `127.0.0.1`；动态频道目录中的 `advertiseIp` 以及由频道表派生的全部游戏端口，必须使用自动探测或显式配置的本机私有 LAN IPv4，并且只绑定该本机网卡接口。游戏端口不能使用 `127.0.0.1`、`0.0.0.0` 或公网地址；客户端初始 game port 固定为 `0`。

这个布局仍是本机测试模式，不代表对局域网或公网提供多人服务。如果自动探测选中了 VPN、虚拟网卡或错误网卡，应停服后在实例配置中显式填写当前活动网卡的私有 LAN IPv4，再通过 BAT 重新启动。

DNF 商业客户端本体不随源码分发；发布包只包含本项目维护的客户端补丁源码和 `90CN.dll`。要进入游戏，已有客户端目录中的 `DNF.exe`/`NoPack.exe`、`ijl15.dll`、`ijl15_real.dll` 和安装后的 `90CN.dll` 必须与本项目的兼容清单一致。

86JP 一键包使用的 SQLite 文件不能作为本项目的 MySQL 数据目录。已有数据若要迁移，必须做表结构和字段级转换，不能复制物理数据库文件。

目标电脑仍需完成自身的 `LOGIN.bat → 注册/登录 → STATUS.bat → 进入场景 → STOP.bat → 再次登录` 验收。控制器的构建、单元测试或资产校验不能替代目标电脑上的客户端兼容性和完整游戏链路验收。

更多说明：

- [Windows 本地部署](docs/deployment/windows-local.md)
- [源码一键端发布与使用教程](docs/deployment/source-release-tutorial.md)
- [配置说明](docs/deployment/configuration.md)
- [登录/进场排错](docs/deployment/troubleshooting.md)
- [源码结构](docs/architecture/overview.md)

## 生成源码一键发布包

发布电脑安装 `go-server/go.mod` 声明版本的 Go 后，双击：

```text
PACKAGE_RELEASE.bat
```

输出位于 `releases\DNF90-source-oneclick-时间.zip`。打包器会重新编译四个 Go EXE，校验 MySQL、VC++ 运行库、PVF、频道表和客户端补丁 DLL 的大小与 SHA256，生成 `SOURCE_MANIFEST.sha256`，并拒绝把本机 `runtime/config`、数据库数据、角色数据、登录账号、凭据、日志、PID、状态、备份或调试文件放入压缩包。
