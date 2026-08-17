// Package alignedcmd 负责把当前 EXE 已确认协议号一致的 C2S 命令归入业务模块。
//
// 本包只做协议迁移边界判断，不直接读写角色、物品、金币或任务状态。
// 具体业务必须在对应 owner/repository/handler 补齐 EXE 包体证据后再实现。
package alignedcmd
