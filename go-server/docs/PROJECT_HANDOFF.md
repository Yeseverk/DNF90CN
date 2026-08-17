# DNF90CN 服务端开发交接文档

> 文档性质：权威开发交接入口
> 覆盖基线：125a469（Redesign launcher and stabilize first character flow）
> 适用范围：本仓库的 Go 服务端、Windows 本地控制器/登录器，以及与其配套的 90CN 客户端兼容层

本文件面向已经了解 Windows、Go、TCP 和 DNF 资源格式的开发者或 AI。内容只记录当前代码能证明的边界、入口、状态和修复；完整历史仍在仓库根目录 changlog.md。

## 1. 接手顺序

从仓库根目录开始：

~~~text
1. 读 AGENTS.md
2. 读 changlog.md 顶部最近条目
3. 读本文件
4. git status --short --branch
5. 查询 CodeGraph：codegraph_status，再用 codegraph_context / codegraph_explore 定位符号
6. 先看真实日志和现有回归测试，再改协议或状态机
~~~

当前 CodeGraph 索引已建立且健康。若未来换了工作区，先确认索引状态；不要用 grep 加大量逐文件阅读替代结构化查询。

## 2. 项目定位与边界

这是一个面向 90CN 当前客户端的本地单机 DNF 服务端。服务端负责：

- 频道启动、频道目录、游戏 TCP/UDP 监听和会话绑定；
- 当前客户端的上下行封包、分类、时序和兼容性；
- 以 Script.pvf/频道资源为输入的目录、装备、任务、副本、掉落和活动规则；
- 以 MySQL 为唯一权威存储的账号、角色、背包、装备、任务和活动状态。

客户端商业程序本体不在本项目的源码责任内。90CN.dll 只做已证实的原生兼容 Hook/协议桥接，不能替代服务端的业务权威；未知协议不能靠伪造角色、物品、地图或 PVF 数据先绕过。

## 3. 目录与代码入口

| 路径 | 作用 |
| --- | --- |
| go-server/cmd/server/dnf90 | 服务端进程入口 |
| go-server/cmd/server/control | BAT 调用的本地安装、配置、MySQL、进程和资产控制器 |
| go-server/cmd/server/launcher | 原生 Win32 登录器；只编排控制器和客户端，不直接访问业务仓储 |
| go-server/cmd/server/doctor | 启动前/启动后的资产、数据库和监听预检 |
| go-server/cmd/server/release | 发布包白名单和运行时构建 |
| go-server/internal/app/dnf90 | 唯一组合根；装配仓储与 dnfbridge |
| go-server/internal/services/dnfbridge | 传输、会话、封包、场景状态机和当前 EXE 兼容逻辑 |
| go-server/internal/modules/dnf | 可测试的领域规则、PVF 解析、事务和协议值对象 |
| go-server/internal/services/logic/dnf | MySQL 仓储组、配置和运行时资源装配 |
| go-server/internal/platform | 构建闭包所需的通用配置、进程、PVF 和服务框架 |
| client-patch/90CN.cpp | 生产 90CN.dll 的兼容层源码；构建说明见同目录 README |
| deploy/assets | 资源与客户端兼容清单 |
| deploy/templates/instance.example.json | 新实例配置模板 |
| runtime/data/dnf | 本地运行时 Script.pvf、channel_info.etc（大文件被 Git 忽略） |
| runtime/config、runtime/configs、runtime/state、runtime/logs | 本机生成配置、所有权状态和诊断输出 |

不要为目录整洁拆分大型 dnfbridge 包；先用当前 EXE 证据和测试确定边界，再做小步提取。

## 4. 客户端兼容单元与资源入口

日常客户端目录必须使用同一套兼容文件：

| 文件 | 当前责任 |
| --- | --- |
| DNF.exe（或 NoPack.exe） | 游戏主程序、原生协议解析、场景和 UI |
| 90CN.dll | 原生 Hook、协议编解码兼容、有限的客户端行为修补；业务状态仍由服务端拥有 |
| 90CNLua.dll | 可选 Lua 桥接/侧边功能；不是服务端仓储 |
| ijl15.dll + ijl15_real.dll | 客户端图像库配对文件 |

兼容档案由以下文件共同定义，不要只替换其中一个：

- deploy/assets/client-compatibility.json：客户端五文件及 90cn-decode-bypass-v1 profile；
- deploy/assets/manifest.json：Script.pvf 与 channel_info.etc 的大小和 SHA256；
- client-patch/README.md：生产 DLL Hook 边界和构建方式；
- deploy/windows/runtime.version：源码行为变化后触发本地忽略二进制更新的版本号。

资源变更的证据入口是 deploy/assets/manifest.json 和 runtime/data/dnf/Script.pvf。新增副本、装备或脚本入口时，排查链必须覆盖：资源解析/目录 -> 服务端 handler 或 world-map/dungeon owner -> 场景路由/状态持久化 -> 当前客户端响应测试。

## 5. 当前启动与登录流程

正常入口只有根目录 LOGIN.bat：

~~~text
LOGIN.bat
  -> 检查 runtime.version 与 runtime/bin/DNF90Build.version
  -> 必要时停止旧服务并通过 control.bat build 更新忽略的 EXE
  -> DNF90Launcher.exe
       -> 选择并校验 DNF.exe/NoPack.exe 所在目录
       -> configure-client（只原子更新 client.directory）
       -> control start（MySQL、资产预检、DNF90Server、ready 检查）
       -> account register 或 account login
       -> launch-client，并把客户端 PID 绑定到认证账号
~~~

首次使用：在登录器选择客户端目录，填账号密码；新账号用“注册并进入”，已有账号用“进入游戏”。登录器使用 Windows Credential Manager 保存可选的两组凭据，数据库只保存 bcrypt 哈希。注册成功后直接启动客户端，不再要求手工编辑 JSON、先点 STATUS.bat 或依赖第二个角色绕过问题。

诊断入口仍保留：

~~~text
START.bat       # 启动/复用 MySQL 与 DNF90Server
STATUS.bat      # 查看 ready、资产、PID、监听端口和账号绑定状态
STOP.bat        # 只停止本安装拥有的服务进程，不删除数据
~~~

控制器命令帮助：

~~~text
DNF90Control.exe start [--rebuild]
DNF90Control.exe stop [--keep-database]
DNF90Control.exe status
DNF90Control.exe build [--force=true]
DNF90Control.exe check [--skip-database] [--skip-ports] [--client]
DNF90Control.exe configure-client --directory PATH
DNF90Control.exe launch-client [--client-directory PATH] [--multi-instance] [--username NAME --password-stdin]
DNF90Control.exe account register --username NAME --password-stdin
DNF90Control.exe account login --username NAME --password-stdin
~~~

## 6. 配置和提交边界

普通开发只应修改源码、模板和清单。实例运行时的唯一人工配置入口是停服后的 runtime/config/instance.json，尤其是 client.directory。控制器会生成 runtime/config/mysql.ini、runtime/configs/*.toml 和服务端状态文件。

以下内容不应提交：

~~~text
runtime/config/instance.json
runtime/configs/
runtime/state/
runtime/logs/
runtime/mysql/
runtime/bin/        # 生成的 EXE
runtime/data/dnf/Script.pvf
runtime/data/dnf/channel_info.etc
数据库导出、密码、管理口令、凭据和临时诊断文件
~~~

本地 profile 的地址约束：启动入口、管理口和 MySQL 只监听 loopback；动态频道目录及游戏端口只绑定 server.advertiseIp 指定的本机私有 IPv4。配置模板位于 deploy/templates/instance.example.json，不要手改生成的 TOML。

## 7. 首角色/选角/场景状态机

这是目前最容易被回归的链路。相关状态集中在 gameSession，不要只看单个 opcode：

~~~text
频道连接
  -> 空选择器的 op2/31 可能暂时 arm channelReconnect（只是猜测）
  -> GET_USERINFO op8（权威角色列表）
  -> 普通角色创建或 op4 选角
  -> 初始城镇/副本场景初始化
~~~

关键规则：

1. GET_USERINFO 收到角色列表时，只清除“未绑定”的推测性 channelReconnect；已有 selectedCharacterID、真实城镇场景或活动副本的重连状态不能被清掉。
2. 首次创建成功的 ACK 后必须紧接着发送非空角色列表。sendCreateSuccess 随后设置 rosterRequested，清理 pendingCharacterRosterBootstrap、emptyRosterSlotProbePending 和无角色绑定的 channelReconnect。
3. 后续 op4 只能进入普通选角；不能因为空列表阶段遗留的探测状态被误判为频道重连。
4. 普通场景先完成房间对象放置（当前 op120 对象/房间状态），再发送依赖对象管理器的用户状态 op3。教程副本把 op3 延迟到 FINISH_LOADING 边界；提前发送会让当前客户端访问未初始化对象管理器。
5. scene_post_start_map.go、scene_finish_loading.go 和 deferred scene tail 共同维护场景尾部时序。修包时必须保留 ACK、对象、actor、玩家状态和 finish-loading 的阶段门，不要凭感觉追加一组“保险包”。

首角色相关入口与回归：

~~~text
go-server/internal/services/dnfbridge/initial_town_route_state.go
go-server/internal/services/dnfbridge/game_bootstrap.go
go-server/internal/services/dnfbridge/character_create_transport.go
go-server/internal/services/dnfbridge/scene_post_start_map.go
go-server/internal/services/dnfbridge/scene_finish_loading.go
go-server/internal/services/dnfbridge/channel_reconnect_test.go
go-server/internal/services/dnfbridge/service_test.go
go-server/internal/services/dnfbridge/dungeon_entry_packets_test.go
go-server/internal/services/dnfbridge/scene_finish_loading_test.go
~~~

若 Windows 真客户端仍在创建第一个角色后掉线，先收集 runtime/logs/server-*.stdout.log、server-*.stderr.log 以及最后一个客户端请求/服务端响应，再判断是 roster、选角、对象顺序还是数据库写入问题。

## 8. 已完成的关键修复

| 提交/状态 | 症状 | 修复落点 | 回归证据 |
| --- | --- | --- | --- |
| df58f6b | 空角色选择器的短探测把后续普通 op4 误判为频道重连；真实重连也可能被误清 | clearUnboundChannelReconnectForRoster；GET_USERINFO op8 只清未绑定猜测，保留已选角色/场景中的重连 | channel_reconnect_test.go 覆盖空选择器和已绑定重连 |
| 125a469 | 创建第一个角色后，非空列表之后的立即选角仍可能走旧重连分支；登录步骤繁琐、旧 EXE 不自动更新 | sendCreateSuccess 清理三类陈旧状态；重做原生登录器和 LOGIN.bat 版本刷新流程 | TestFirstCreatedCharacterClearsSpeculativeReconnectBeforeSelection；launcher/control/release 测试 |
| 当前基线 | 首角色场景中 op3 早于房间对象管理器会崩溃/掉线 | 普通场景把对象放置置于用户状态；教程场景延后到 finish-loading | dungeon_entry_packets_test.go、scene_finish_loading_test.go 及场景时序测试 |

其他玩法修复（任务、背包/装备、PVF 掉落、副本、活动等）以 changlog.md 顶部条目和各模块测试为准；不要根据旧归档或旧二进制推断当前行为。

## 9. 标准排查方法

协议或掉线问题按以下顺序取证：

1. git status，确认不是旧的 runtime/bin 或旧客户端文件；用控制器/清单核对五文件和 PVF。
2. STATUS.bat，确认 MySQL、server ready、频道入口和全部游戏端口。
3. 保留同一时间窗口的 server stdout/stderr、必要时开启受控 packet log；不要把密码或管理口令打进日志。
4. 记录最后一条请求的方向、class、opcode、body length/hex，以及服务端最后一条响应；未知 body 先拒绝或记录，不要猜字段。
5. 用 CodeGraph 查 context/trace/callers，定位实际 handler 和状态写入点；然后阅读已有测试 fixture。
6. 先写最小失败回归，再修改；验证 ACK、数据库事务、后续刷新顺序和失败回滚。
7. Windows 真机复现后再决定是否需要客户端 DLL 变更。服务端能证明的状态不要迁移到 DLL。

首角色日志事件包括：

~~~text
game-getuserinfo-cleared-unbound-channel-reconnect
game-create-roster-established-ordinary-selection
game-upper-select-character-scene-tail-deferred
game-upper-post-start-map-player-state-send
game-main-finish-loading-state-send
~~~

## 10. 构建、测试与部署

从 go-server 执行：

~~~text
go list -buildvcs=false ./...
go test -buildvcs=false -count=1 ./internal/modules/dnf/...
go test -buildvcs=false -count=1 ./internal/services/dnfbridge
go test -buildvcs=false -count=1 ./cmd/server/control
go test -buildvcs=false -count=1 ./cmd/server/launcher
go test -buildvcs=false -count=1 ./cmd/server/release
go vet ./cmd/server/control ./cmd/server/dnf90 ./cmd/server/doctor ./cmd/server/launcher ./internal/app/dnf90 ./internal/services/dnfbridge
go build -buildvcs=false -mod=readonly -trimpath -o ..\runtime\bin\DNF90Server.exe ./cmd/server/dnf90
~~~

完整 dnfbridge 测试在 macOS 上可能无法执行依赖 127.0.0.2 的 UDP 所有权测试；Windows 路径语义测试也应在 Windows 跑。带真实资源的 smoke test 通过环境变量显式启用，例如：

~~~text
DNFBRIDGE_REAL_PVF_SMOKE=D:\DNF\runtime\data\dnf\Script.pvf
DNF_WORLDMAP_REAL_PVF_SMOKE=D:\DNF\runtime\data\dnf\Script.pvf
~~~

macOS 能做源码测试、静态检查和 Windows/amd64 交叉构建，但不能替代 Win32 登录器、真实 DNF 客户端、DLL Hook、真实 PVF 场景和首角色现场验收。

Windows 本地验收顺序：

~~~text
1. 安装/替换五个客户端文件
2. LOGIN.bat
3. 注册或登录账号，创建第一个角色并选中进入城镇
4. 退出并重新登录，确认角色、背包和场景状态仍由 MySQL 恢复
5. STATUS.bat 查看 ready、PID、端口和日志
~~~

## 11. 未完成项与风险

- 当前提交已通过源码和模拟协议回归，但首角色完整链路仍需要 Windows 真客户端验收；macOS 不代表客户端行为已通过。
- 如果现场仍掉线，下一步是日志/最后协议证据，不是继续堆叠响应包。
- runtime/bin 被 Git 忽略；涉及服务端或登录器二进制行为的改动必须递增 deploy/windows/runtime.version，否则旧本地 EXE 可能继续运行。
- 客户端兼容 profile、五个文件、PVF 和频道清单是一个版本单元；任何一项改变都要同步清单、测试和 changlog。

## 12. 变更规则

每个有行为影响的提交都必须：

- 在同一提交更新 changlog.md 和本文件对应章节；
- 增加或更新最小回归测试，并记录实际运行命令/平台；
- 需要替换运行时 EXE 时递增 deploy/windows/runtime.version；
- 保留当前兼容 profile 的证据和清单，不提交运行时数据库、凭据、日志、PVF 大文件或生成 EXE；
- 提交前执行 gofmt（Go 改动）、git diff --check 和必要的构建/测试；
- 若修复依赖 Windows 现场，明确写“源码通过、现场未验收”，不要把候选构建写成已部署。

文档自身若与代码冲突，以当前源码、当前客户端实测和回归测试为准；修复完成后立即把本文件更新到新的行为基线。
