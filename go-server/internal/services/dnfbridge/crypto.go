package dnfbridge

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"errors"
	"io"
)

var errAESBlockSize = errors.New("dnf aes data is not block aligned")

func encryptChannelData(data []byte, key string, compress bool) ([]byte, error) {
	padded := padZero(data, aes.BlockSize)
	encrypted, err := aesECBEncrypt(padded, key)
	if err != nil {
		return nil, err
	}
	if !compress {
		return encrypted, nil
	}
	return zlibCompress(encrypted)
}

func padZero(data []byte, align int) []byte {
	if align <= 0 {
		return append([]byte(nil), data...)
	}
	padding := (align - len(data)%align) % align
	out := make([]byte, len(data)+padding)
	copy(out, data)
	return out
}

func aesECBEncrypt(data []byte, key string) ([]byte, error) {
	if len(data)%aes.BlockSize != 0 {
		return nil, errAESBlockSize
	}
	keyBytes := make([]byte, aes.BlockSize)
	copy(keyBytes, []byte(key))
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(data))
	for offset := 0; offset < len(data); offset += aes.BlockSize {
		block.Encrypt(out[offset:offset+aes.BlockSize], data[offset:offset+aes.BlockSize])
	}
	return out, nil
}

func zlibCompress(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func zlibDecompress(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
