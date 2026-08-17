// Package integrity 定义动作合法性、回放摘要和风险分接入边界。
//
// Validator 和 Provider 实现必须可被并发调用，并且必须尊重传入 context。
// Allowed=false 是业务拒绝，error 是校验链路不可用；两者不能混用，否则调用方无法区分外挂命中和风控系统故障。
// 适配层应把第三方 panic 转换为 error，并把风险分归一到 0..100。
package integrity
