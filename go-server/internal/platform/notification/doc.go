// Package notification 定义离线通知和邮件通道接入边界。
//
// Provider 实现必须可被并发调用，并且必须尊重传入 context 和 IdempotencyKey。
// Receipt.Accepted=false 表示供应商明确拒收；网络失败、限流、鉴权失败和可重试错误必须返回 error，调用方据此决定重试或写入补偿队列。
// 适配层不得在同步关键写路径里阻塞等待第三方最终投递结果。
package notification
