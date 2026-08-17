// Package logic 负责已认证玩家流量的后端命令分发。
//
// Logic handler 负责加载和修改 Profile 状态、执行幂等保护、追加 EventLog 并更新投影。
// 传输细节留在 gateway，可复用玩法系统放在 internal/runtime。
package logic
