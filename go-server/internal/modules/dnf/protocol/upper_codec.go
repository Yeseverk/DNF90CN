// 本文件负责 DNF latest raw upper 服务端包体加密兼容。
// 这里只处理 wire codec，不写登录、角色或持久化业务状态。
package protocol

import "golang.org/x/crypto/cast5"

var latestUpperOpcode1Key = []byte("2D@+(vhxnw8h!xvb")

// EncodeLatestUpperServerBody 按 NoPack.exe 的 raw upper S2C codec 编码服务端包体。
// 当前只确认 opcode 1 使用 CAST5；其他 opcode 必须重新追 key 后再接入。
func EncodeLatestUpperServerBody(msgID uint16, body []byte) ([]byte, bool, error) {
	if msgID != 1 {
		return append([]byte(nil), body...), false, nil
	}
	encoded, err := encodeLatestUpperCAST5(body, latestUpperOpcode1Key)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
}

func encodeLatestUpperCAST5(body []byte, key []byte) ([]byte, error) {
	block, err := cast5.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blockSize := block.BlockSize()
	size := len(body)
	if rem := size % blockSize; rem != 0 {
		size += blockSize - rem
	}

	padded := make([]byte, size)
	copy(padded, body)
	out := make([]byte, size)
	for offset := 0; offset < size; offset += blockSize {
		block.Encrypt(out[offset:offset+blockSize], padded[offset:offset+blockSize])
	}
	return out, nil
}
