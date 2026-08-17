// Package module 定义可装配运行时模块的生命周期边界。
//
// Module 实现必须可重复 Stop，并且 Start/AfterStart 失败后要能接受回滚 Stop。
// Preflight 只能验证配置和依赖可达性，不得改写业务状态；BeforeStop 不应无限等待外部流量自然耗尽。
// 所有生命周期方法必须尊重传入 context，panic 应在模块内转换为 error。
package module
