# 登录、启动与进场排错

先运行 `STATUS.bat`。启动客户端前，至少应确认：

- MySQL 显示 `RUNNING` 且 `MySQL Ready: true`；
- root DSN 可以连接 `dnf_local` 并完成临时表写入/删除检查；
- 服务端显示 `RUNNING` 且 `Ready: true`；
- PVF 与 `channel_info.etc` 的大小和 SHA256 正确；
- `127.0.0.1:7001`、`127.0.0.1:18111` 正在监听，频道表派生出的全部游戏端口正在 `server.advertiseIp` 指定的本机私有 LAN IPv4 上监听；
- 客户端兼容文件与清单一致。

## 首次启动提示 MySQL ZIP 缺失或校验失败

发布包必须包含：

```text
deploy/vendor/mysql/mysql-8.4.10-winx64.zip
deploy/assets/mysql-portable.json
```

控制器会核对 ZIP 的大小和 SHA256，并在解压后再次核对关键文件。不要重命名、重新压缩或替换归档。恢复同一发布包中的原文件后重试；不要把未经清单验证的 `mysqld.exe` 复制进 `runtime/mysql/server`。

## MySQL 提示缺少 DLL 或无法启动

官方 MySQL 8.4 Windows x64 二进制依赖 Microsoft x64 运行库。本包将所需 DLL 作为应用本地文件携带，控制器会按大小和 SHA256 校验后放到 `runtime/mysql/server/bin`，不修改系统目录。

如果 `mysqld.exe` 报 DLL 缺失、应用程序无法正常启动或刚启动就退出：

1. 确认系统是 Windows 10/11 x64；
2. 确认 `deploy/vendor/vcruntime/x64` 与 `deploy/assets/vcruntime-app-local.json` 完整，且没有被杀毒软件隔离；
3. 不要手工替换 `runtime/mysql/server/bin` 中的运行库 DLL；
4. 查看 `runtime/logs/mysql-error.log` 和最新的 `mysql-*.stderr.log`；
5. 恢复原发布包文件后再次执行 `START.bat`。

不要从随机网站单独下载 DLL 放到项目目录。

## 13306 端口被占用

默认本包 MySQL 使用 `127.0.0.1:13306`。先运行：

```text
STATUS.bat
```

如果状态文件证明是本包已管理的 MySQL，重复 `START.bat` 会复用它；如果端口属于其他程序，先停止冲突程序，或在 `runtime/config/instance.json` 中为这个新实例选择另一个未占用的 loopback 端口。

不要用任务管理器按名称批量结束所有 `mysqld.exe`。`STOP.bat` 会核对 PID、进程创建时间、EXE 路径和 SHA256，只停止本安装拥有的进程。

## root 密码错误

实际明文凭据位于：

```text
runtime/config/instance.json
runtime/configs/dnf/logic.toml
```

首次初始化完成后，MySQL 数据目录与该实例的 root 密码绑定。之后只手工修改 `instance.json` 不会同步修改数据库内的账号。

优先恢复与当前数据目录匹配的原配置或备份。不要通过删除 `runtime/mysql/data` 来“修密码”；那会丢失角色和业务数据。确需重置时，应先停止 MySQL、做完整备份，并把它作为有意识的数据重建操作处理。

## 数据目录或所有权状态被拒绝

为了防止误覆盖，控制器不会初始化或接管一个非空但缺少有效安装状态的数据目录，也不会接受与当前安装 ID、路径或 `mysqld.exe` 哈希不匹配的状态。

检查：

```text
runtime/mysql/data-state.json
runtime/mysql/server/.dnf90-install.json
runtime/state/mysql-process.json
```

不要手工伪造这些文件。若目录来自另一份安装，应连同原 `runtime/config/instance.json` 一起恢复，或在安全备份后做明确的数据迁移。

## 提示缺少 DNF90Control.exe 或 Go

正常发布包应包含：

```text
runtime/bin/DNF90Control.exe
runtime/bin/DNF90Server.exe
runtime/bin/DNF90Doctor.exe
runtime/bin/DNF90Launcher.exe
```

普通启动不需要 Go。如果控制器被删除，BAT 会尝试用 Go 从源码重建；电脑没有 Go 时会明确退出。恢复发布包内的 EXE，或安装 `go-server/go.mod` 对应版本的 Go 后执行 `REBUILD.bat`。

## MySQL 已启动，但服务端没有 ready

依次检查：

1. `runtime/logs/mysql-error.log` 是否有数据库错误；
2. 最新 `server-*.stderr.log` 和 `server-*.stdout.log`；
3. `runtime/configs/dnf/logic.toml` 的 DSN 是否与 `instance.json` 一致；
4. PVF 与频道表是否通过资产校验；
5. `127.0.0.1:7001`、`127.0.0.1:18111` 和 `server.advertiseIp` 上的游戏端口是否被其他程序占用。

启动失败时，控制器会尽量回滚本次启动的服务进程，同时保留 MySQL 数据。不要通过删除数据目录排查普通端口或配置问题。

## 服务 ready，但客户端收不到频道或进不了场景

依次检查：

1. 客户端启动入口/频道下载必须是 `127.0.0.1:7001`；不要把启动参数直接改成游戏地址。
2. `server.advertiseIp` 必须是自动探测或显式配置的本机私有 LAN IPv4，不能是 `127.0.0.1`、`0.0.0.0` 或公网地址。
3. 确认该地址确实属于当前活动的本机网卡。若自动探测选中 VPN、虚拟网卡或错误网卡，先执行 `STOP.bat`，显式配置正确的私有 LAN IPv4，再执行 `START.bat`。
4. 客户端初始 game port 必须是 `0`，不能写成 10011。
5. 用 `STATUS.bat` 确认频道表派生出的全部游戏端口都只监听在 `server.advertiseIp` 对应的本机接口上，而不是 loopback、`0.0.0.0` 或公网接口。
6. `DNF_HOOK_CREATE=1` 必须在启动客户端前设置；使用 `LAUNCH_CLIENT.bat` 会自动处理。
7. 90CN profile 必须使用 `server16` header、上下行 `plaintext` codec、固定 outer token 和频道索引 1。
8. `DNF.exe`/`NoPack.exe`、`ijl15.dll`、`ijl15_real.dll`、`90CN.dll` 必须整套匹配。
9. `crack` 频道需要对应的游戏端口；不要只检查 7001。
10. 必须由顶层 BAT 启动，让控制器以 `runtime` 为工作目录运行服务；不要直接双击服务端 EXE。

## 异步任务与数据一致性

当前异步队列在 DNF90Server 进程内。角色、背包、装备、金币、任务等关键资产应在成功响应前同步提交 MySQL；纯内存通知或可重建任务不会作为数据备份。

如果异常退出后发现关键资产与客户端最后一次提示不一致，应保留日志并按对应事务排查，不能用“异步还没刷缓存”解释或伪造数据。进程退出时尚未完成的非关键内存任务不会跨启动保留。

## 登录器无法注册或登录

先确认登录名为 3–32 个字符，只包含字母、数字、汉字、下划线、短横线或点；密码为 8–72 个 UTF-8 字节。登录名不区分大小写。

登录器需要本包 MySQL，因此第一次注册可能会先完成数据库初始化。账号密码不会写入日志；数据库表 `dnf_login_accounts` 只保存 bcrypt 哈希。若勾选记住凭据后没有自动填充，检查当前 Windows 用户的凭据管理器是否被系统策略禁用。取消勾选并成功登录会删除该实例保存的凭据。

服务端已 READY 时，登录另一个账号不会重启 DNF90Server，也不会切换全局账号。登录器会认证当前输入账号、启动一个新客户端，并把新进程 ID 绑定到该账号。认证失败只会中止本次启动，不应断开已有客户端。

## 双开不同账号

推荐顺序是“填写账号 1 并登录 → 填写账号 2 并登录”。两个登录按钮都会认证各自输入账号，但不切换兼容回退账号，也不重启已 READY 的服务端，因此不会主动断开第一开。每次启动都会启用当前 EXE 精确地址限定的双开兼容，登记新进程与账号的绑定，并等待 5 秒确认进程仍在运行；若认证失败或进程提前退出，登录器会直接显示失败。登录器不隐藏游戏窗口或任务管理器中的进程。

## 多个客户端仍看到同一批角色

先确认两个客户端都是通过 `LOGIN.bat` 输入各自账号启动，而不是直接双击 `DNF.exe` 或连续使用 `LAUNCH_CLIENT.bat`。登录器启动的连接会由 Windows TCP 所属进程解析到各自进程 ID，再选择对应账号；直接启动的客户端只能使用 `server.accountId` 兼容回退。检查服务日志中的 `client_pid` 和 `session_account_bound=true`，若未绑定，退出该客户端并从登录器重新认证启动。

## 收集证据

保留：

```text
runtime/logs/mysql-error.log
runtime/logs/mysql-*.stdout.log
runtime/logs/mysql-*.stderr.log
runtime/logs/server-*.stdout.log
runtime/logs/server-*.stderr.log
runtime/logs/packet_log.txt
runtime/state/asset-state.json
runtime/state/mysql-process.json
runtime/state/server-process.json
runtime/mysql/data-state.json
```

封包日志默认关闭；只有协议诊断时才把 `server.packetLog` 设为 `true`。提交问题前应删除或遮蔽明文密码与管理口令。
