// Package dnfbridge 提供 DNF 最新客户端的独立 TCP bridge 服务。
//
// 服务只负责最新客户端登录频道服和 game transport 的网络入口。
// PVF/channel_info.etc 只作为只读资源索引，玩家数据和玩法逻辑后续接 repository/owner。
package dnfbridge
