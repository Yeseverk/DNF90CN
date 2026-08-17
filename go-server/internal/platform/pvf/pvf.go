// Package pvf 提供 DNF 类 PVF 资源的启动期内存加载能力。
//
// 框架只负责把 PVF 文件作为不可变资源装入内存并暴露元信息，不在平台层解释
// DNF 玩法字段；解析、索引和业务校验应放在游戏项目层。
package pvf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const DefaultMaxBytes int64 = 512 * 1024 * 1024
const rfc3339Nano = time.RFC3339Nano

var (
	ErrPathRequired = errors.New("pvf path is required")
	ErrTooLarge     = errors.New("pvf exceeds max bytes")
)

type Options struct {
	Path     string
	MaxBytes int64
}

type Snapshot struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Checksum string    `json:"checksum"`
	LoadedAt time.Time `json:"loaded_at"`
}

type Bundle struct {
	snapshot Snapshot
	data     []byte
}

func Load(options Options) (*Bundle, error) {
	options.Path = strings.TrimSpace(options.Path)
	if options.Path == "" {
		return nil, ErrPathRequired
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("pvf max bytes must be positive")
	}
	info, err := os.Stat(options.Path)
	if err != nil {
		return nil, fmt.Errorf("stat pvf %q: %w", options.Path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("pvf path %q is not a regular file", options.Path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%w: got %d want <= %d", ErrTooLarge, info.Size(), maxBytes)
	}
	data, err := os.ReadFile(options.Path)
	if err != nil {
		return nil, fmt.Errorf("read pvf %q: %w", options.Path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: got %d want <= %d", ErrTooLarge, len(data), maxBytes)
	}
	sum := sha256.Sum256(data)
	return &Bundle{
		snapshot: Snapshot{
			Path:     options.Path,
			Size:     int64(len(data)),
			Checksum: hex.EncodeToString(sum[:]),
			LoadedAt: time.Now().UTC(),
		},
		data: data,
	}, nil
}

func (b *Bundle) Snapshot() Snapshot {
	if b == nil {
		return Snapshot{}
	}
	return b.snapshot
}

func (b *Bundle) Bytes() []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b.data))
	copy(out, b.data)
	return out
}

func (b *Bundle) Reader() *bytes.Reader {
	if b == nil {
		return bytes.NewReader(nil)
	}
	return bytes.NewReader(b.data)
}
