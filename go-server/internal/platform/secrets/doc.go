// Package secrets 定义密钥来源接入边界。
//
// Provider 实现必须可被并发调用，并且必须尊重传入 context。
// GetSecret 的 ok=false 只表示密钥不存在；后端不可达、解密失败和权限不足必须返回 error，避免生产配置被误判为“未配置”。
// 返回的 Secret.Value 必须是调用方独占副本，适配层不得暴露内部缓存或 SDK 复用缓冲区。
package secrets
