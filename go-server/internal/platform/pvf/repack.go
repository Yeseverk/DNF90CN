package pvf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"unicode/utf16"
)

// RawReplacement 描述一次 PVF 内已有文件的 raw 字节替换。
// 这里的 Data 是解压后的文件原始字节，文本类文件通常仍是 UTF-16LE 或 token 数据。
type RawReplacement struct {
	Path string
	Data []byte
}

// StringReplacement changes one existing shared-string value without moving
// its encoded offset. Old and New must therefore occupy exactly the same
// number of bytes in the target ANSI or UTF-16 pool.
type StringReplacement struct {
	MagicOffset int
	Old         string
	New         string
}

// RepackStrings rebuilds the protected NKPI string-pool section while keeping
// file-table offsets and every body chunk unchanged. It is intentionally
// limited to same-width replacements, which prevents unrelated script-token
// offsets from shifting.
func (a *Archive) RepackStrings(replacements []StringReplacement) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: archive is nil", ErrInvalidArchive)
	}
	if len(replacements) == 0 {
		return nil, fmt.Errorf("%w: string replacement is empty", ErrInvalidArchive)
	}
	if a.format != FormatProtectedNKPI {
		return nil, fmt.Errorf("%w: string repack requires %q, got %q", ErrInvalidArchive, FormatProtectedNKPI, a.format)
	}
	layout, err := a.layout()
	if err != nil {
		return nil, err
	}

	strA := append([]byte(nil), a.strA...)
	strW := append([]byte(nil), a.strW...)
	seen := make(map[int]StringReplacement, len(replacements))
	for _, replacement := range replacements {
		if replacement.MagicOffset < 0 {
			return nil, fmt.Errorf("%w: negative string offset %d", ErrInvalidArchive, replacement.MagicOffset)
		}
		if previous, ok := seen[replacement.MagicOffset]; ok {
			if previous.Old != replacement.Old || previous.New != replacement.New {
				return nil, fmt.Errorf("%w: conflicting string replacements at offset %d", ErrInvalidArchive, replacement.MagicOffset)
			}
			continue
		}
		seen[replacement.MagicOffset] = replacement
		if replacement.MagicOffset&1 != 0 {
			start := (replacement.MagicOffset >> 1) * 2
			oldBytes := utf16LEBytes(replacement.Old)
			newBytes := utf16LEBytes(replacement.New)
			if err := replaceStringBytes(strW, start, oldBytes, newBytes, 2); err != nil {
				return nil, fmt.Errorf("replace UTF-16 string offset %d: %w", replacement.MagicOffset, err)
			}
			continue
		}
		start := replacement.MagicOffset >> 1
		if err := replaceStringBytes(strA, start, []byte(replacement.Old), []byte(replacement.New), 1); err != nil {
			return nil, fmt.Errorf("replace ANSI string offset %d: %w", replacement.MagicOffset, err)
		}
	}

	nameSection, err := a.encodeProtectedNameSection(layout, strA, strW)
	if err != nil {
		return nil, err
	}
	header := a.header
	header.nameSize = len(nameSection)
	out := a.encodeHeader(header)
	out = append(out, encodeFileTable(a.items)...)
	out = append(out, a.data[layout.hashOffset:layout.hashOffset+layout.hashSize]...)
	out = append(out, nameSection...)
	oldNameEnd := layout.nameOffset + layout.nameSize
	out = append(out, a.data[oldNameEnd:layout.bodyOffset]...)
	out = append(out, a.data[layout.bodyOffset:layout.bodyOffset+layout.bodySize]...)
	return out, nil
}

func replaceStringBytes(pool []byte, start int, oldBytes, newBytes []byte, terminatorWidth int) error {
	if len(oldBytes) != len(newBytes) {
		return fmt.Errorf("replacement width changed from %d to %d", len(oldBytes), len(newBytes))
	}
	end := start + len(oldBytes)
	if start < 0 || end+terminatorWidth > len(pool) {
		return fmt.Errorf("string range %d:%d exceeds pool size %d", start, end, len(pool))
	}
	if !bytes.Equal(pool[start:end], oldBytes) {
		return fmt.Errorf("old string bytes do not match at %d", start)
	}
	for index := 0; index < terminatorWidth; index++ {
		if pool[end+index] != 0 {
			return fmt.Errorf("string at %d is not null terminated", start)
		}
	}
	copy(pool[start:end], newBytes)
	return nil
}

func utf16LEBytes(value string) []byte {
	units := utf16.Encode([]rune(value))
	out := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(out[index*2:index*2+2], unit)
	}
	return out
}

func (a *Archive) encodeProtectedNameSection(layout archiveLayout, strA, strW []byte) ([]byte, error) {
	original := a.data[layout.nameOffset : layout.nameOffset+layout.nameSize]
	if len(original) < 8 {
		return nil, fmt.Errorf("%w: protected name section is too short", ErrInvalidArchive)
	}
	index := 8
	for _, xorConst := range []uint32{0xAA74472E, 0x9A82F037} {
		if index+8 > len(original) {
			return nil, fmt.Errorf("%w: protected name section is missing a string buffer", ErrInvalidArchive)
		}
		compressedSize := int(binary.LittleEndian.Uint32(original[index:index+4]) ^ xorConst)
		if compressedSize < 0 || index+8+compressedSize > len(original) {
			return nil, fmt.Errorf("%w: protected string buffer exceeds name section", ErrInvalidArchive)
		}
		index += 8 + compressedSize
	}

	out := append([]byte(nil), original[:8]...)
	encodedA, err := encodeProtectedStringBuffer("StRa", 0xAA74472E, strA)
	if err != nil {
		return nil, err
	}
	out = append(out, encodedA...)
	encodedW, err := encodeProtectedStringBuffer("StRw", 0x9A82F037, strW)
	if err != nil {
		return nil, err
	}
	out = append(out, encodedW...)
	out = append(out, original[index:]...)
	return out, nil
}

func encodeProtectedStringBuffer(key string, xorConst uint32, raw []byte) ([]byte, error) {
	compressed, err := compressZlib(raw)
	if err != nil {
		return nil, fmt.Errorf("compress protected string buffer %s: %w", key, err)
	}
	encrypted := append([]byte(nil), compressed...)
	decryptPString(key, encrypted)
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], uint32(len(encrypted))^xorConst)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(raw))^uint32(len(encrypted)))
	out := append(header, encrypted...)
	return out, nil
}

type archiveLayout struct {
	hashOffset int
	hashSize   int
	nameOffset int
	nameSize   int
	bodyOffset int
	bodySize   int
}

// RepackRaw 只替换已有文件内容，不新增路径、不改字符串池和哈希表。
// 该能力用于离线修补 PVF 后重新封包，运行时查询仍通过启动期内存归档完成。
func (a *Archive) RepackRaw(replacements []RawReplacement) ([]byte, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: archive is nil", ErrInvalidArchive)
	}
	if len(replacements) == 0 {
		return nil, fmt.Errorf("%w: replacement is empty", ErrInvalidArchive)
	}
	if !a.CanReadFileData() {
		return nil, fmt.Errorf("%w: format %q cannot repack", ErrInvalidArchive, a.Format())
	}
	layout, err := a.layout()
	if err != nil {
		return nil, err
	}

	replacedByIndex := make(map[int][]byte, len(replacements))
	touchedChunks := make(map[int]bool, len(replacements))
	for _, replacement := range replacements {
		idx := a.FindFileIndex(replacement.Path)
		if idx < 0 {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, replacement.Path)
		}
		item := a.items[idx]
		if item.chunkIndex < 0 || item.chunkIndex >= len(a.groups) {
			return nil, fmt.Errorf("%w: file %d chunk %d is invalid", ErrInvalidArchive, idx, item.chunkIndex)
		}
		replacedByIndex[idx] = append([]byte(nil), replacement.Data...)
		touchedChunks[item.chunkIndex] = true
	}

	newItems := append([]fileItem(nil), a.items...)
	chunkFiles, err := a.filesByChunk()
	if err != nil {
		return nil, err
	}

	body, newGroups, err := a.repackBody(layout, chunkFiles, touchedChunks, replacedByIndex, newItems)
	if err != nil {
		return nil, err
	}
	if len(body) > math.MaxInt32 {
		return nil, fmt.Errorf("%w: body too large: %d", ErrInvalidArchive, len(body))
	}

	header := a.header
	header.bodySize = len(body)
	header.groupCount = len(newGroups)
	out := a.encodeHeader(header)
	out = append(out, encodeFileTable(newItems)...)
	out = append(out, a.data[layout.hashOffset:layout.hashOffset+layout.hashSize]...)
	out = append(out, a.data[layout.nameOffset:layout.nameOffset+layout.nameSize]...)
	out = append(out, a.encodeGroups(newGroups)...)
	out = append(out, body...)
	return out, nil
}

func (a *Archive) filesByChunk() (map[int][]int, error) {
	chunkFiles := make(map[int][]int, len(a.groups))
	for idx, item := range a.items {
		if item.chunkIndex < 0 || item.chunkIndex >= len(a.groups) {
			return nil, fmt.Errorf("%w: file %d chunk %d is invalid", ErrInvalidArchive, idx, item.chunkIndex)
		}
		chunkFiles[item.chunkIndex] = append(chunkFiles[item.chunkIndex], idx)
	}
	for chunkIndex := range chunkFiles {
		sort.SliceStable(chunkFiles[chunkIndex], func(left, right int) bool {
			leftItem := a.items[chunkFiles[chunkIndex][left]]
			rightItem := a.items[chunkFiles[chunkIndex][right]]
			if leftItem.dataOffset == rightItem.dataOffset {
				return chunkFiles[chunkIndex][left] < chunkFiles[chunkIndex][right]
			}
			return leftItem.dataOffset < rightItem.dataOffset
		})
	}
	return chunkFiles, nil
}

func (a *Archive) repackBody(layout archiveLayout, chunkFiles map[int][]int, touched map[int]bool, replacements map[int][]byte, items []fileItem) ([]byte, []groupItem, error) {
	body := make([]byte, 0, layout.bodySize)
	groups := make([]groupItem, len(a.groups))
	cumulative := 0
	for chunkIndex := range a.groups {
		var encrypted []byte
		originalSize := a.groups[chunkIndex].originalSize
		var err error
		if touched[chunkIndex] {
			chunk, err := a.rebuildChunk(chunkIndex, chunkFiles[chunkIndex], replacements, items)
			if err != nil {
				return nil, nil, err
			}
			originalSize = len(chunk)
			encrypted, err = a.encryptChunk(chunk)
			if err != nil {
				return nil, nil, err
			}
		} else {
			encrypted, err = a.rawChunk(layout, chunkIndex)
			if err != nil {
				return nil, nil, err
			}
		}
		cumulative += len(encrypted)
		if cumulative > math.MaxInt32 || originalSize > math.MaxInt32 {
			return nil, nil, fmt.Errorf("%w: rebuilt chunk %d is too large", ErrInvalidArchive, chunkIndex)
		}
		groups[chunkIndex] = groupItem{
			compressedSize: cumulative,
			originalSize:   originalSize,
		}
		body = append(body, encrypted...)
	}
	return body, groups, nil
}

func (a *Archive) rebuildChunk(chunkIndex int, fileIndexes []int, replacements map[int][]byte, items []fileItem) ([]byte, error) {
	originalChunk, err := a.chunk(chunkIndex)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(originalChunk))
	prevEnd := 0
	for _, fileIndex := range fileIndexes {
		item := a.items[fileIndex]
		if item.dataOffset < 0 || item.dataSize < 0 || item.dataOffset+item.dataSize > len(originalChunk) {
			return nil, fmt.Errorf("%w: file %d range is invalid", ErrInvalidArchive, fileIndex)
		}
		if item.dataOffset > prevEnd {
			out = append(out, originalChunk[prevEnd:item.dataOffset]...)
		}
		raw, ok := replacements[fileIndex]
		if !ok {
			raw, err = a.readRawIndex(fileIndex)
			if err != nil {
				return nil, err
			}
		}
		if len(out) > math.MaxInt32 || len(raw) > math.MaxInt32 {
			return nil, fmt.Errorf("%w: replacement %d is too large", ErrInvalidArchive, fileIndex)
		}
		items[fileIndex].dataOffset = len(out)
		items[fileIndex].dataSize = len(raw)
		out = append(out, raw...)
		if end := item.dataOffset + item.dataSize; end > prevEnd {
			prevEnd = end
		}
	}
	if prevEnd < len(originalChunk) {
		out = append(out, originalChunk[prevEnd:]...)
	}
	return out, nil
}

func (a *Archive) encryptChunk(raw []byte) ([]byte, error) {
	encrypted, err := compressZlib(raw)
	if err != nil {
		return nil, err
	}
	if a.format == FormatProtectedNKPI {
		decryptProtected("bODy", encrypted)
	} else {
		decrypt("BodY", encrypted)
	}
	return encrypted, nil
}

func (a *Archive) rawChunk(layout archiveLayout, chunkIndex int) ([]byte, error) {
	prev := 0
	if chunkIndex > 0 {
		prev = a.groups[chunkIndex-1].compressedSize
	}
	curr := a.groups[chunkIndex].compressedSize
	if curr < prev {
		return nil, fmt.Errorf("%w: chunk %d compressed size is invalid", ErrInvalidArchive, chunkIndex)
	}
	start := layout.bodyOffset + prev
	size := curr - prev
	if start < 0 || size < 0 || start+size > layout.bodyOffset+layout.bodySize {
		return nil, fmt.Errorf("%w: chunk %d exceeds body", ErrInvalidArchive, chunkIndex)
	}
	return append([]byte(nil), a.data[start:start+size]...), nil
}

func (a *Archive) layout() (archiveLayout, error) {
	tableSize, err := checkedMul(a.header.fileCount, fileItemSize)
	if err != nil {
		return archiveLayout{}, err
	}
	hashOffset := headerSize + tableSize
	nameOffset := hashOffset + a.header.hashSize
	groupSize, err := checkedMul(a.header.groupCount, groupItemSize)
	if err != nil {
		return archiveLayout{}, err
	}
	bodyOffset := nameOffset + a.header.nameSize + groupSize
	if hashOffset < 0 || nameOffset < 0 || bodyOffset < 0 || bodyOffset+a.header.bodySize > len(a.data) {
		return archiveLayout{}, fmt.Errorf("%w: archive layout exceeds data", ErrInvalidArchive)
	}
	return archiveLayout{
		hashOffset: hashOffset,
		hashSize:   a.header.hashSize,
		nameOffset: nameOffset,
		nameSize:   a.header.nameSize,
		bodyOffset: bodyOffset,
		bodySize:   a.header.bodySize,
	}, nil
}

func (a *Archive) encodeHeader(header pvfHeader) []byte {
	out := append([]byte(nil), header.plain[:]...)
	binary.LittleEndian.PutUint32(out[24:28], uint32(header.fileCount))
	binary.LittleEndian.PutUint32(out[32:36], uint32(header.bodySize))
	binary.LittleEndian.PutUint32(out[36:40], uint32(header.groupCount))
	binary.LittleEndian.PutUint32(out[40:44], uint32(header.hashSize))
	binary.LittleEndian.PutUint32(out[44:48], uint32(header.nameSize))
	if a.format == FormatProtectedNKPI {
		decryptProtected("hEAd", out)
		return out
	}
	decrypt("HeaD", out)
	decryptGuard(out)
	return out
}

func encodeFileTable(items []fileItem) []byte {
	out := make([]byte, len(items)*fileItemSize)
	for idx, item := range items {
		off := idx * fileItemSize
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(item.nameOffset))
		binary.LittleEndian.PutUint32(out[off+4:off+8], uint32(item.pathOffset))
		binary.LittleEndian.PutUint32(out[off+8:off+12], uint32(item.chunkIndex))
		binary.LittleEndian.PutUint32(out[off+12:off+16], uint32(item.dataOffset))
		binary.LittleEndian.PutUint32(out[off+16:off+20], uint32(item.dataSize))
		binary.LittleEndian.PutUint32(out[off+20:off+24], uint32(item.dataType))
	}
	return out
}

func (a *Archive) encodeGroups(groups []groupItem) []byte {
	out := make([]byte, len(groups)*groupItemSize)
	for idx, group := range groups {
		off := idx * groupItemSize
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(group.compressedSize))
		binary.LittleEndian.PutUint32(out[off+4:off+8], uint32(group.originalSize))
	}
	if a.format == FormatProtectedNKPI {
		decryptProtected("grpi", out)
		return out
	}
	decrypt("GRPI", out)
	return out
}
