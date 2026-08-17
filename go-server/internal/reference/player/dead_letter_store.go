package player

import "longheng.io/server/internal/platform/db"

// AsyncStoreDeadLetter 是玩家异步写入失败后保留的死信记录。
type AsyncStoreDeadLetter = db.AsyncStoreDeadLetter[Profile, ProfileField]

// DeadLetterStore 定义玩家异步存储死信的持久化接口。
type DeadLetterStore = db.DeadLetterStore[Profile, ProfileField]

// FileDeadLetterStore 是保存玩家异步死信的文件型存储。
type FileDeadLetterStore = db.FileDeadLetterStore[Profile, ProfileField]

// NewFileDeadLetterStore 创建文件型玩家异步死信存储。
func NewFileDeadLetterStore(dir string) *FileDeadLetterStore {
	return db.NewFileDeadLetterStore[Profile, ProfileField](db.FileDeadLetterStoreOptions{
		Directory:        dir,
		DefaultDirectory: "data/player_dead_letters",
	})
}
