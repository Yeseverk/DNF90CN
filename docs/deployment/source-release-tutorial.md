# DNF90 源码一键端发布与使用教程

本文分为两部分：

- 第一部分给发布者：把当前源码制作成可分发 ZIP。
- 第二部分给使用者：拿到 ZIP 后从零启动、安装客户端补丁并进入游戏。

## 一、发布者：生成源码一键包

### 1. 发布电脑准备

发布电脑需要：

- Windows 10/11 x64；
- 当前完整的 DNF90 项目目录；
- `go-server/go.mod` 声明版本的 Go；
- 项目已经包含的 MySQL、VC++ 运行库、PVF、频道表和客户端补丁文件。

普通玩家运行发布包不需要安装 Go。只有制作发布包或修改源码后重新编译才需要 Go。

### 2. 发布前检查

先确认项目至少包含：

```text
go-server\go.mod
client-patch\90CN.cpp
client-patch\bin\90CN.dll
deploy\vendor\mysql\mysql-8.4.10-winx64.zip
runtime\data\dnf\Script.pvf
runtime\data\dnf\channel_info.etc
PACKAGE_RELEASE.bat
```

不要手工把当前电脑的以下目录复制进发布包：

```text
runtime\config
runtime\configs
runtime\logs
runtime\mysql
runtime\state
runtime\backups
```

这些目录可能包含本机安装 ID、管理口令、角色数据库、运行日志、PID 和进程状态。打包器会主动排除并检查这些内容。

### 3. 一键打包

在项目根目录双击：

```text
PACKAGE_RELEASE.bat
```

打包器会自动完成：

1. 校验 MySQL ZIP、VC++ 运行库、PVF、频道表和 `90CN.dll` 的大小与 SHA256；
2. 从当前源码重新编译：
   - `DNF90Launcher.exe`
   - `DNF90Control.exe`
   - `DNF90Doctor.exe`
   - `DNF90Server.exe`
3. 按白名单复制服务端源码、客户端补丁源码、文档和运行资产；
4. 排除数据库、角色、密码配置、日志、状态、备份、转储和调试产物；
5. 生成包内 `SOURCE_MANIFEST.sha256`；
6. 在 `releases` 目录生成发布 ZIP。

输出示例：

```text
releases\DNF90-source-oneclick-20260725-191749.zip
```

命令行会同时显示 ZIP 的字节数和 SHA256。发布 ZIP 时应把 SHA256 一起发给使用者。

### 4. 验证发布 ZIP

可以在 CMD 中执行：

```bat
certutil -hashfile "DNF90-source-oneclick-时间.zip" SHA256
```

结果必须与打包器输出一致。

发布包应包含：

```text
DNF90-source-oneclick
├─ START.bat
├─ LOGIN.bat
├─ STOP.bat
├─ STATUS.bat
├─ REBUILD.bat
├─ PACKAGE_RELEASE.bat
├─ INSTALL_CLIENT_PATCH.bat
├─ LAUNCH_CLIENT.bat
├─ go-server
├─ client-patch
├─ deploy
├─ docs
└─ runtime
   ├─ bin
   └─ data\dnf
```

发布包不包含 DNF 商业客户端本体；只包含本项目维护的兼容补丁源码和 `90CN.dll`。

## 二、使用者：解压后一键启动

### 1. 解压

把完整 ZIP 解压到空间充足、当前用户可写的本地磁盘目录，例如：

```text
D:\DNF90
```

不要直接从压缩软件的临时预览窗口运行 BAT，也不要解压到只读目录。

首次启动需要解压 MySQL 并初始化数据，建议预留至少 2 GB 可用空间。

### 2. 初始化服务端

双击：

```text
START.bat
```

第一次启动会自动：

1. 创建 `runtime\config\instance.json`；
2. 生成本机安装 ID 和管理口令；
3. 校验并解压 MySQL 8.4.10；
4. 初始化全新的本机数据库；
5. 设置明文 root 密码；
6. 创建 `dnf_local`；
7. 生成服务端配置；
8. 加载 PVF 和频道表；
9. 启动 MySQL 与 DNF90Server；
10. 检查全部监听端口。

看到以下内容才表示启动成功：

```text
DNF90 is READY.
```

也可以先完成下面的客户端目录配置，再直接通过 `LOGIN.bat` 注册或登录。登录器会自动完成服务启动，不要求预先手工运行 `START.bat`。

### 3. 数据库连接信息

当前源码一键版固定使用：

```text
地址：127.0.0.1
端口：13306
用户：root
密码：aa123456
数据库：dnf_local
```

密码以明文写入本机生成的配置。MySQL 只监听 `127.0.0.1`，不要把端口映射到公网，也不要把这个密码复用于其他数据库。

### 4. 地址说明

以下入口固定使用回环地址：

```text
频道下载：127.0.0.1:7001
管理接口：127.0.0.1:18111
MySQL：127.0.0.1:13306
```

当前 90CN 客户端的动态频道目录和 `100xx` 游戏端口需要使用目标电脑自己的私有网卡 IPv4。发布包不会写死制作电脑的 `192.168.x.x`；每台电脑启动时自动检测自己的地址，并且游戏端口只绑定该本机接口。

如果电脑有 VPN、虚拟网卡或多张网卡，自动检测可能选错。此时先运行 `STOP.bat`，再编辑：

```text
runtime\config\instance.json
```

把 `server.advertiseIp` 改为当前活动网卡的私有 IPv4，然后重新运行 `START.bat`。

### 5. 安装客户端补丁

先关闭正在运行的 DNF 客户端，然后双击：

```text
INSTALL_CLIENT_PATCH.bat
```

输入已有 DNF 客户端的完整目录。脚本会把：

```text
client-patch\bin\90CN.dll
```

复制到客户端根目录。

发布包不提供 `DNF.exe`、`NoPack.exe`、`ijl15.dll` 或 `ijl15_real.dll`。使用者必须自行准备与兼容清单完全匹配的客户端。

### 6. 配置客户端目录

打开：

```text
runtime\config\instance.json
```

把：

```json
"directory": ""
```

改成实际客户端目录，例如：

```json
"directory": "D:\\Games\\DNF"
```

JSON 中的反斜杠必须写成双反斜杠。

修改运行配置前应先执行 `STOP.bat`。保存后可以执行 `START.bat`，也可以直接使用下一节的登录器，让控制器重新生成配置并校验。

### 7. 注册、登录并启动客户端

双击：

```text
LOGIN.bat
```

第一次使用先输入账号和密码并点击“注册账号”。首个注册账号会继承升级前实例已有的角色数据；以后注册的账号使用独立角色数据。注册完成后点击“登录并启动游戏”。

登录器提供两组账号/密码输入，Tab 可按顺序切换控件。需要下次自动填充时勾选“记住两组账号密码”。登录密码在数据库中只保存 bcrypt 哈希；被记住的两组明文凭据分别进入当前 Windows 用户的凭据管理器，不会写入项目目录、运行日志或发布 ZIP。

登录器会认证当前输入账号、确保服务端 ready，并校验：

- `DNF.exe` 或 `NoPack.exe`；
- `ijl15.dll`；
- `ijl15_real.dll`；
- `90CN.dll`；
- 服务端 ready 状态。

所有哈希一致后才会启动客户端。

`LAUNCH_CLIENT.bat` 是保留的兼容入口，只启动 `server.accountId` 对应的回退账号，不提供注册、认证或进程账号绑定。

需要启动两个不同账号客户端时，分别在账号 1 和账号 2 区域输入已注册账号及其密码，再依次点击“登录账号 1”和“登录账号 2”。登录器不会停止已 READY 的服务端，而是分别认证输入账号、启动新进程并绑定该进程账号，在提示成功前确认新进程至少存活 5 秒。登录器不隐藏游戏窗口，任务管理器会正常显示两个进程。

## 三、日常管理

### 查看状态

双击：

```text
STATUS.bat
```

正常状态应显示：

```text
MySQL: RUNNING
MySQL Ready: true
Server: RUNNING
Ready: true
listen ports: accepting connections
```

### 正常停止

双击：

```text
STOP.bat
```

脚本会先停止 DNF90Server，再安全关闭本包 MySQL。角色数据不会删除。

不要直接结束 `mysqld.exe`，也不要在 MySQL 运行时复制数据目录。

### 再次启动

以后直接双击：

```text
LOGIN.bat
```

登录后控制器会复用原来的 MySQL 程序、数据库和对应账号的角色数据。

## 四、数据库备份与恢复

### 备份

先执行 `STOP.bat`，确认 MySQL 已停止，然后同时备份：

```text
runtime\mysql\data
runtime\config\instance.json
```

必须成对保存。前者是角色和业务数据，后者保存与数据目录匹配的安装 ID 和数据库配置。

### 恢复

1. 在目标目录执行 `STOP.bat`；
2. 确认 MySQL 已停止；
3. 恢复原来的 `runtime\mysql\data`；
4. 恢复与它配套的 `runtime\config\instance.json`；
5. 执行 `START.bat`；
6. 用 `STATUS.bat` 检查数据库和服务端。

不要把其他服务端的 SQLite 文件直接复制为本项目的 MySQL 数据目录。

## 五、修改源码后重新编译

源码发布包包含完整 Go 服务端源码。安装对应版本的 Go 后，双击：

```text
REBUILD.bat
```

重建前应先执行 `STOP.bat`。重建完成后再运行 `START.bat`。

客户端补丁源码位于：

```text
client-patch
```

`D:\DNF\client-patch` 是正式维护和发布来源；游戏目录中的 `Patch` 只视为旧开发副本，游戏根目录的 `90CN.dll` 只是运行安装件。

它是 Visual Studio 2019 `Release|Win32` 项目。修改后可双击：

```text
REBUILD_CLIENT_PATCH.bat
```

脚本会从正式源码重建并更新 `client-patch\bin\90CN.dll`。之后必须把脚本显示的新 SHA256 同步到：

```text
deploy\assets\client-compatibility.json
```

否则严格客户端校验会拒绝启动。

## 六、常见问题

### START 提示端口占用

先运行 `STATUS.bat`，确认是否已有本项目实例运行。不要同时启动两个使用相同端口的实例。

### START 提示 MySQL ZIP 或 PVF 校验失败

文件被修改、缺失或下载不完整。恢复同一个发布 ZIP 中的原文件，不要自行重压 MySQL ZIP。

### LAUNCH_CLIENT 提示哈希不匹配

客户端版本不兼容，或 `90CN.dll` 未安装。先关闭客户端，再重新运行 `INSTALL_CLIENT_PATCH.bat`。其他客户端文件必须使用兼容清单要求的版本。

### 修改 instance.json 后提示配置漂移

先执行 `STOP.bat`，确认服务端和 MySQL 都停止，再执行 `START.bat`。不要手工修改 `runtime\configs` 或 `runtime\config\mysql.ini`。

### 登录后无法连接频道

检查 `STATUS.bat` 是否显示全部游戏端口正在监听，并确认 `server.advertiseIp` 选择了当前活动物理网卡的私有 IPv4，而不是 VPN 或虚拟网卡地址。

### 可以把数据库开放给其他电脑吗

不可以。当前 profile 是单机本地测试端，MySQL、管理端和频道下载入口固定为 loopback。不要改成 `0.0.0.0` 或公网地址。
