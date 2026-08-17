package pvf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func (a *Archive) decodeScript(raw []byte) string {
	lineCount := len(raw) / 5
	if lineCount == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw) * 2)
	for idx := 0; idx < lineCount; idx++ {
		off := idx * 5
		tokenType := raw[off]
		value := readInt32(raw[off+1 : off+5])
		switch tokenType {
		case 0:
			fmt.Fprintf(&b, "%d ", value)
		case 2:
			fmt.Fprintf(&b, "%.2f ", math.Float32frombits(uint32(value)))
		case 3:
			b.WriteByte('\n')
			b.WriteString(a.resolveString(value))
			b.WriteByte('\n')
		case 5:
			b.WriteByte('\n')
			b.WriteString("{5=``}")
		case 6:
			b.WriteByte('`')
			b.WriteString(a.resolveString(value))
			b.WriteString("` ")
		case 7:
			b.WriteByte('\n')
			b.WriteString("{7=``}")
		}
	}
	return b.String()
}

func (a *Archive) resolveString(magicOffset int) string {
	if magicOffset < 0 {
		return ""
	}
	if magicOffset&1 != 0 {
		return readUTF16String(a.strW, (magicOffset>>1)*2)
	}
	return readUTF8String(a.strA, magicOffset>>1)
}

func readUTF8String(buf []byte, start int) string {
	if start < 0 || start >= len(buf) {
		return ""
	}
	end := bytes.IndexByte(buf[start:], 0)
	if end < 0 {
		return ""
	}
	rawBytes := buf[start : start+end]
	if utf8.Valid(rawBytes) {
		return string(rawBytes)
	}
	raw := string(rawBytes)
	if decoded, err := simplifiedchinese.GB18030.NewDecoder().String(raw); err == nil {
		return decoded
	}
	return raw
}

func readUTF16String(buf []byte, start int) string {
	if start < 0 || start >= len(buf) {
		return ""
	}
	end := start
	for end+1 < len(buf) {
		if buf[end] == 0 && buf[end+1] == 0 {
			break
		}
		end += 2
	}
	if end <= start {
		return ""
	}
	return decodeUTF16LE(buf[start:end])
}

func decodeUTF16LE(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	units := make([]uint16, 0, len(data)/2)
	for idx := 0; idx+1 < len(data); idx += 2 {
		units = append(units, binary.LittleEndian.Uint16(data[idx:idx+2]))
	}
	return string(utf16.Decode(units))
}

func zlibBytes(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func compressZlib(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := zlib.NewWriter(&buf)
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
