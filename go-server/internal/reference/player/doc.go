// 玩家模块 player 是蓝图自带的示例账号模块。
//
// 本包演示游戏模块如何把通用 Profile 运行时、生成后的 Profile Schema、
// 生成后的读模型、存储和 handler 串起来。货币、背包、任务、邮箱逻辑只用于本地
// 本地 smoke 测试，真实项目应替换为自己的模块和 Schema。
//
// 共享框架代码应放在 internal/platform 下，不能依赖本包或本包的示例数据字段。
package player
