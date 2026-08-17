package pvf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// ScriptStringPatchReport 记录一次脚本字符串引用补丁的命中统计。
type ScriptStringPatchReport struct {
	FilesChanged  int `json:"files_changed"`
	TokensChanged int `json:"tokens_changed"`
	ChunksChanged int `json:"chunks_changed"`
}

// PatchScriptString 把选定 type-1 脚本中对共享字符串池某一值的引用改写为
// 池中另一个已存在的值。它只重写脚本 token 里的 4 字节池偏移，不扩建、不重排
// 字符串池，因此引用旧值的其他脚本保持不变（格式依据：type-1 脚本以 5 字节
// token 编码，token 类型 3/6 的负载为字符串池 magic 偏移，见 decode.go 的
// decodeScript；该写路径与参考项目 pvf/patch.go 的已验证行为一致）。
//
// oldValue 与 newValue 都必须已存在于 ANSI 字符串池（strA）中；UTF-16 池
// 字符串的引用改写尚未被参考实现证明，故不予承诺。返回值为重建后的完整 PVF
// 字节；函数会在返回前回开归档并逐文件验证新值生效、旧值消失。
func (a *Archive) PatchScriptString(paths []string, oldValue, newValue string) ([]byte, ScriptStringPatchReport, error) {
	var report ScriptStringPatchReport
	if a == nil || len(paths) == 0 || oldValue == "" || newValue == "" || oldValue == newValue {
		return nil, report, fmt.Errorf("%w: invalid script string patch", ErrInvalidArchive)
	}
	oldMagic, ok := a.findStringMagic(oldValue)
	if !ok {
		return nil, report, fmt.Errorf("old string %q is absent from PVF string pool", oldValue)
	}
	newMagic, ok := a.findStringMagic(newValue)
	if !ok {
		return nil, report, fmt.Errorf("new string %q is absent from PVF string pool", newValue)
	}

	modifiedChunks := make(map[int][]byte)
	seenPaths := make(map[string]struct{}, len(paths))
	for _, relativePath := range paths {
		key := pathKey(relativePath)
		if _, duplicate := seenPaths[key]; duplicate {
			continue
		}
		seenPaths[key] = struct{}{}
		fileIndex, found := a.pathIdx[key]
		if !found {
			return nil, report, fmt.Errorf("%w: %s", ErrFileNotFound, relativePath)
		}
		item := a.items[fileIndex]
		if item.dataType != 1 {
			return nil, report, fmt.Errorf("file %s has data type %d, want script type 1", relativePath, item.dataType)
		}
		chunk, exists := modifiedChunks[item.chunkIndex]
		if !exists {
			plain, err := a.chunk(item.chunkIndex)
			if err != nil {
				return nil, report, err
			}
			// chunk 缓存被归档共享，改写前必须复制，避免污染运行期查询。
			chunk = append([]byte(nil), plain...)
			modifiedChunks[item.chunkIndex] = chunk
		}
		if item.dataOffset < 0 || item.dataSize < 0 || item.dataOffset+item.dataSize > len(chunk) {
			return nil, report, fmt.Errorf("%w: file %s data range is invalid", ErrInvalidArchive, relativePath)
		}
		changed := replaceScriptStringMagic(chunk[item.dataOffset:item.dataOffset+item.dataSize], oldMagic, newMagic)
		if changed == 0 {
			return nil, report, fmt.Errorf("file %s does not reference %q", relativePath, oldValue)
		}
		report.FilesChanged++
		report.TokensChanged += changed
	}
	report.ChunksChanged = len(modifiedChunks)

	patched, err := a.rebuildWithChunks(modifiedChunks)
	if err != nil {
		return nil, report, err
	}
	verified, err := OpenBytes(patched)
	if err != nil {
		return nil, report, fmt.Errorf("reopen patched PVF: %w", err)
	}
	for relativePath := range seenPaths {
		text, err := verified.ReadText(relativePath)
		if err != nil {
			return nil, report, fmt.Errorf("verify patched file %s: %w", relativePath, err)
		}
		if !bytes.Contains([]byte(text), []byte(newValue)) || bytes.Contains([]byte(text), []byte(oldValue)) {
			return nil, report, fmt.Errorf("patched file %s failed string verification", relativePath)
		}
	}
	return patched, report, nil
}

// findStringMagic 在 ANSI 字符串池中定位完整值（前后均以 NUL 结尾），返回
// 脚本 token 使用的 magic 偏移（池内字节偏移左移 1 位，偶数表示 ANSI 池，
// 见 decode.go 的 resolveString）。
func (a *Archive) findStringMagic(value string) (int, bool) {
	needle := append([]byte(value), 0)
	for offset := 0; offset < len(a.strA); {
		index := bytes.Index(a.strA[offset:], needle)
		if index < 0 {
			break
		}
		index += offset
		if index == 0 || a.strA[index-1] == 0 {
			return index << 1, true
		}
		offset = index + 1
	}
	return 0, false
}

// replaceScriptStringMagic 逐 token 扫描脚本原始字节，把引用 oldMagic 的
// 字符串 token（类型 3 标签、类型 6 行内字符串）改写为 newMagic。只触碰
// 4 字节负载，脚本长度不变。
func replaceScriptStringMagic(raw []byte, oldMagic, newMagic int) int {
	changed := 0
	for offset := 0; offset+5 <= len(raw); offset += 5 {
		if raw[offset] != 3 && raw[offset] != 6 {
			continue
		}
		if readInt32(raw[offset+1:offset+5]) != oldMagic {
			continue
		}
		binary.LittleEndian.PutUint32(raw[offset+1:offset+5], uint32(int32(newMagic)))
		changed++
	}
	return changed
}

// rebuildWithChunks 用改写后的 chunk 重建归档：逐 chunk 重新 zlib 压缩并按
// 当前格式重新加密（bODy/BodY），重建 GRPI 组表与 header 的 bodySize；未改动
// 的 chunk 直接搬运原始密文，文件表、哈希段、字符串池保持字节级不变。
func (a *Archive) rebuildWithChunks(modified map[int][]byte) ([]byte, error) {
	layout, err := a.layout()
	if err != nil {
		return nil, err
	}
	groupTableSize, err := checkedMul(len(a.groups), groupItemSize)
	if err != nil {
		return nil, err
	}
	groupOffset := layout.bodyOffset - groupTableSize
	if groupOffset < headerSize || groupOffset > len(a.data) {
		return nil, fmt.Errorf("%w: group table offset is invalid", ErrInvalidArchive)
	}

	body := make([]byte, 0, layout.bodySize)
	groups := make([]groupItem, len(a.groups))
	for index, group := range a.groups {
		var encrypted []byte
		if chunk, changed := modified[index]; changed {
			// 引用改写是等长替换，chunk 解压尺寸必须与原尺寸一致。
			if len(chunk) != group.originalSize {
				return nil, fmt.Errorf("%w: modified chunk %d size changed", ErrInvalidArchive, index)
			}
			encrypted, err = a.encryptChunk(chunk)
			if err != nil {
				return nil, fmt.Errorf("encrypt modified chunk %d: %w", index, err)
			}
		} else {
			encrypted, err = a.rawChunk(layout, index)
			if err != nil {
				return nil, err
			}
		}
		body = append(body, encrypted...)
		if len(body) > math.MaxInt32 {
			return nil, fmt.Errorf("%w: rebuilt body is too large: %d", ErrInvalidArchive, len(body))
		}
		groups[index] = groupItem{compressedSize: len(body), originalSize: group.originalSize}
	}

	result := make([]byte, 0, groupOffset+groupTableSize+len(body))
	result = append(result, a.data[:groupOffset]...)
	result = append(result, a.encodeGroups(groups)...)
	result = append(result, body...)
	header := a.header
	header.bodySize = len(body)
	copy(result[:headerSize], a.encodeHeader(header))
	return result, nil
}
