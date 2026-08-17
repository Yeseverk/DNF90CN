package onlinepush

import "context"

// Store 定义在线推送 receipt 和离线消息状态存储。
type Store interface {
	ReserveReceipt(context.Context, Receipt) (Receipt, bool, error)
	UpdateReceipt(context.Context, Receipt) error
	SaveOffline(context.Context, OfflineMessage) error
	ListOffline(context.Context, string, int) ([]OfflineMessage, error)
	DeleteOffline(context.Context, string) error
	Snapshot(context.Context) Snapshot
}
