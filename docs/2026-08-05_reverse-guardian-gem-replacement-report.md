# 守护珠替换无响应排障记录

> 日期：2026-08-05
> 范围：本地 DNF90 服务端的守护珠替换事务与当前客户端交互。
> 结论：已修复源代码并生成候选服务端；本记录不代表候选程序已经部署。

## 范围

仅检查了本地服务端、运行日志和用户提供的客户端界面。未修改数据库、PVF、运行时生成配置或客户端文件。本机没有可用的 IDA 安装路径，因而未能对 `DNF.exe` 执行新的静态反编译；该限制不影响本次由实时请求日志和既有精确 `op829` 解析共同确定的服务端故障结论。

## Evidence

### E-001

- title: 客户端显示替换确认而非空槽位限制
- observed_at: 2026-08-05
- source_type: screenshot
- source_ref: `C:/Users/ADMINI~1/AppData/Local/Temp/codex-clipboard-e79f4c4f-f20b-41ac-93f0-4e08598a4f6a.png`
- content_hash: n/a
- repro_command: |
    在已装备守护珠的公会徽章中选择一个新守护珠，并确认“确定要替换吗”。
- raw_excerpt: |
    确认框说明：装备的守护珠无法再回收，确定要替换吗？
- linked_workitem: n/a
- supersedes: none

### E-002

- title: 替换请求到达服务端后被空槽位保护拒绝
- observed_at: 2026-08-05 18:16:11 +08:00
- source_type: log
- source_ref: `runtime/logs/packet_log.txt`
- content_hash: n/a
- repro_command: |
    rg -n -i "guardian-gem-use-blocked" runtime/logs/packet_log.txt
- raw_excerpt: |
    target_medal_item_id=100380058 guardian_gem_source_slot=57 guardian_gem_item_id=90083 socket_index=2 reason=guardian gem target socket is already occupied: socket=2
- linked_workitem: n/a
- supersedes: none

### E-003

- title: 替换事务与完整桥接层回归通过
- observed_at: 2026-08-05
- source_type: command
- source_ref: `go-server/internal/services/dnfbridge/guild_medal_test.go`
- content_hash: n/a
- repro_command: |
    cd go-server
    go test -buildvcs=false -count=1 ./internal/modules/dnf/...
    go test -buildvcs=false -count=1 ./internal/services/dnfbridge
- raw_excerpt: |
    internal/modules/dnf/guardiangem: ok
    internal/services/dnfbridge: ok
- linked_workitem: n/a
- supersedes: none

## Findings

### F-001

- title: 已装备守护珠不能替换
- severity: n/a_re
- category: design
- status: validated
- evidence_ids: [E-001, E-002, E-003]
- location: `go-server/internal/services/dnfbridge/guardian_gem_projection.go`
- impact: 有珠子的目标槽位收到有效 `op829` 请求后没有任何客户端状态更新；新珠不消耗，旧珠也未变更。
- confidence: high
- repro_steps:
  1. 给已装备的公会徽章任一守护珠槽位选择新的 list-38 守护珠。
  2. 在客户端替换确认框中确认。
  3. 旧实现将非零原始槽位值视为错误并静默结束处理。
- remediation: 写入新守护珠的原始槽位值，仍在同一角色物品事务中消耗请求指定的新珠；不创建或返还被替换的旧珠。
- optional_attack:

## Path

### P-001

- title: 守护珠替换调用路径
- path_type: callflow
- start: 客户端确认替换并发送 `op829`
- goal: 服务端持久化新守护珠并刷新徽章及 list-38 页面
- steps:
  1. action: `decodeCurrentGuardianGemUseRequest` 解析目标徽章、源槽位、新珠和 socket index。 evidence: E-002 finding: F-001
  2. action: `commitCurrentGuardianGemUse` 通过角色物品工作单元执行原子投影。 evidence: E-003 finding: F-001
  3. action: `currentGuardianGemWriteRawSocket` 覆盖目标槽位，随后只消耗新珠。 evidence: E-001 finding: F-001
  4. action: 发送 list-3 徽章更新和 class-0/list-38 页面重建。 evidence: E-003 finding: F-001
- residual_risks: 当前本机无可用 IDA，因此未新增客户端静态反编译证据；候选程序尚未部署，运行中的服务仍保留旧行为。

## 时间线

- 18:16:11：运行日志记录替换请求因已占用 socket 2 被拒绝。
- 随后：将非空槽位从拒绝改为覆盖，新珠消费与徽章写入保持在同一原子事务中。
- 验证后：构建候选 `runtime/tmp/DNF90Server-guardian-gem-replace-candidate.exe`，SHA-256 为 `9E55B146CB17833F84CC548197B8EE788D4ED6FE2407F9F7316203A7EAFDB240`。
