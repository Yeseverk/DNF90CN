package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// DecodeStrictJSON 读取单个 JSON 请求体，统一限制大小、拒绝未知字段并检查尾部多余 JSON。
func DecodeStrictJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, out any) error {
	if r == nil || r.Body == nil {
		return errors.New("request body is required")
	}
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	defer func() {
		_ = body.Close()
	}()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("request body must contain a single JSON object: %w", err)
	}
	return errors.New("request body must contain a single JSON object")
}

// DecodeStrictJSONReader 从任意 reader 读取单个 JSON 对象；适合已经自行处理空 body 的 handler helper。
func DecodeStrictJSONReader(reader io.Reader, maxBytes int64, out any) error {
	if reader == nil {
		return errors.New("request body is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("request body exceeds %d bytes", maxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return fmt.Errorf("request body must contain a single JSON object: %w", err)
	}
	return errors.New("request body must contain a single JSON object")
}
