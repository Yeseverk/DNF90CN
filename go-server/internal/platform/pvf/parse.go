package pvf

import (
	"encoding/binary"
	"fmt"
)

type fileItem struct {
	nameOffset int
	pathOffset int
	chunkIndex int
	dataOffset int
	dataSize   int
	dataType   int
}

type groupItem struct {
	compressedSize int
	originalSize   int
}

type pvfHeader struct {
	plain      [headerSize]byte
	fileCount  int
	bodySize   int
	groupCount int
	hashSize   int
	nameSize   int
}

func (a *Archive) parse() error {
	if len(a.data) < headerSize {
		return fmt.Errorf("%w: header too short", ErrInvalidArchive)
	}
	if err := a.parseNKPI(); err == nil {
		return nil
	}
	if err := a.parseProtectedNKPI(); err == nil {
		return nil
	}
	return fmt.Errorf("%w: unsupported archive format", ErrInvalidArchive)
}

func (a *Archive) parseNKPI() error {
	a.resetParseState()
	header := append([]byte(nil), a.data[:headerSize]...)
	decryptGuard(header)
	decrypt("HeaD", header)
	if binary.LittleEndian.Uint32(header[0:4]) != magicSignature {
		return fmt.Errorf("%w: bad signature", ErrInvalidArchive)
	}
	pvfHeader := parseHeaderSizes(header)
	if err := validateHeader(pvfHeader); err != nil {
		return err
	}
	return a.parseNKPITables(pvfHeader, false)
}

func (a *Archive) parseProtectedNKPI() error {
	a.resetParseState()
	header := append([]byte(nil), a.data[:headerSize]...)
	decryptProtected("hEAd", header)
	if binary.LittleEndian.Uint32(header[0:4]) != magicSignature {
		return fmt.Errorf("%w: bad protected signature", ErrInvalidArchive)
	}
	pvfHeader := parseHeaderSizes(header)
	if err := validateHeader(pvfHeader); err != nil {
		return err
	}
	return a.parseNKPITables(pvfHeader, true)
}

func (a *Archive) parseNKPITables(header pvfHeader, protected bool) error {
	pos := headerSize
	tableOffset := pos
	tableSize, err := checkedMul(header.fileCount, fileItemSize)
	if err != nil {
		return err
	}
	pos += tableSize
	pos += header.hashSize
	nameOffset := pos
	pos += header.nameSize
	groupOffset := pos
	groupSize, err := checkedMul(header.groupCount, groupItemSize)
	if err != nil {
		return err
	}
	pos += groupSize
	if pos < 0 || pos > len(a.data) || header.bodySize > len(a.data)-pos {
		return fmt.Errorf("%w: section sizes exceed archive", ErrInvalidArchive)
	}
	a.header = header
	a.bodyOff = pos

	nameBytes := append([]byte(nil), a.data[nameOffset:nameOffset+header.nameSize]...)
	if protected {
		if err := a.buildPStringBuffers(nameBytes); err != nil {
			return err
		}
		a.format = FormatProtectedNKPI
	} else {
		a.buildStringBuffers(nameBytes)
		a.format = FormatNKPI
	}

	groupBytes := append([]byte(nil), a.data[groupOffset:groupOffset+groupSize]...)
	if protected {
		decryptProtected("grpi", groupBytes)
	} else {
		decrypt("GRPI", groupBytes)
	}
	if err := a.parseGroups(groupBytes, header.groupCount); err != nil {
		return err
	}
	return a.parseFiles(tableOffset, header.fileCount)
}

func (a *Archive) resetParseState() {
	a.format = ""
	a.files = nil
	a.items = nil
	a.groups = nil
	a.header = pvfHeader{}
	a.pathIdx = make(map[string]int)
	a.bodyOff = 0
	a.strA = nil
	a.strW = nil
}

func parseHeaderSizes(header []byte) pvfHeader {
	var plain [headerSize]byte
	copy(plain[:], header)
	return pvfHeader{
		plain:      plain,
		fileCount:  readInt32(header[24:28]),
		bodySize:   readInt32(header[32:36]),
		groupCount: readInt32(header[36:40]),
		hashSize:   readInt32(header[40:44]),
		nameSize:   readInt32(header[44:48]),
	}
}

func validateHeader(header pvfHeader) error {
	if header.fileCount < 0 || header.bodySize < 0 || header.groupCount < 0 || header.hashSize < 0 || header.nameSize < 0 {
		return fmt.Errorf("%w: negative header size", ErrInvalidArchive)
	}
	return nil
}

func (a *Archive) parseGroups(data []byte, count int) error {
	a.groups = make([]groupItem, 0, count)
	for idx := 0; idx < count; idx++ {
		off := idx * groupItemSize
		compressedSize := readInt32(data[off : off+4])
		originalSize := readInt32(data[off+4 : off+8])
		if compressedSize < 0 || originalSize < 0 {
			return fmt.Errorf("%w: negative group size", ErrInvalidArchive)
		}
		a.groups = append(a.groups, groupItem{
			compressedSize: compressedSize,
			originalSize:   originalSize,
		})
	}
	return nil
}

func (a *Archive) parseFiles(offset, count int) error {
	a.items = make([]fileItem, 0, count)
	a.files = make([]File, 0, count)
	for idx := 0; idx < count; idx++ {
		off := offset + idx*fileItemSize
		if off+fileItemSize > len(a.data) {
			return fmt.Errorf("%w: file table exceeds archive", ErrInvalidArchive)
		}
		item := fileItem{
			nameOffset: readInt32(a.data[off : off+4]),
			pathOffset: readInt32(a.data[off+4 : off+8]),
			chunkIndex: readInt32(a.data[off+8 : off+12]),
			dataOffset: readInt32(a.data[off+12 : off+16]),
			dataSize:   readInt32(a.data[off+16 : off+20]),
			dataType:   readInt32(a.data[off+20 : off+24]),
		}
		name := a.resolveString(item.nameOffset)
		dir := a.resolveString(item.pathOffset)
		archivePath := joinArchivePath(dir, name)
		a.items = append(a.items, item)
		a.files = append(a.files, File{
			Index:       idx,
			Path:        dir,
			Name:        name,
			ArchivePath: archivePath,
			DataType:    item.dataType,
			Size:        item.dataSize,
		})
		if archivePath != "" {
			key := pathKey(archivePath)
			if _, exists := a.pathIdx[key]; !exists {
				a.pathIdx[key] = idx
			}
		}
	}
	return nil
}

func (a *Archive) buildStringBuffers(nameBytes []byte) {
	if len(nameBytes) < 16 {
		return
	}
	idx := 8
	a.strA = decryptStringBuffer(nameBytes, &idx, "sTrA", 0xAA74472E)
	a.strW = decryptStringBuffer(nameBytes, &idx, "sTrW", 0x9A82F037)
}

func (a *Archive) buildPStringBuffers(nameBytes []byte) error {
	if len(nameBytes) < 16 {
		return nil
	}
	idx := 8
	strA, err := decryptPStringBuffer(nameBytes, &idx, "StRa", 0xAA74472E)
	if err != nil {
		return fmt.Errorf("decode protected pvf strA: %w", err)
	}
	strW, err := decryptPStringBuffer(nameBytes, &idx, "StRw", 0x9A82F037)
	if err != nil {
		return fmt.Errorf("decode protected pvf strW: %w", err)
	}
	a.strA = strA
	a.strW = strW
	return nil
}

func decryptStringBuffer(data []byte, idx *int, key string, xorConst uint32) []byte {
	if *idx+8 > len(data) {
		return nil
	}
	cnt1 := binary.LittleEndian.Uint32(data[*idx : *idx+4])
	*idx += 4
	*idx += 4
	encSize := int(cnt1 ^ xorConst)
	if encSize <= 0 || *idx+encSize > len(data) {
		return nil
	}
	encrypted := append([]byte(nil), data[*idx:*idx+encSize]...)
	*idx += encSize
	decrypt2(key, encrypted)
	out, err := zlibBytes(encrypted)
	if err != nil {
		return nil
	}
	return out
}

func decryptPStringBuffer(data []byte, idx *int, key string, xorConst uint32) ([]byte, error) {
	if *idx+8 > len(data) {
		return nil, nil
	}
	encodedSize := binary.LittleEndian.Uint32(data[*idx : *idx+4])
	encodedOriginalSize := binary.LittleEndian.Uint32(data[*idx+4 : *idx+8])
	*idx += 8
	encSize := int(encodedSize ^ xorConst)
	if encSize <= 0 || *idx+encSize > len(data) {
		return nil, fmt.Errorf("%w: invalid protected string buffer size %d", ErrInvalidArchive, encSize)
	}
	encrypted := append([]byte(nil), data[*idx:*idx+encSize]...)
	*idx += encSize
	decryptPString(key, encrypted)
	out, err := zlibBytes(encrypted)
	if err != nil {
		return nil, err
	}
	if want := int(encodedOriginalSize ^ uint32(encSize)); want >= 0 && want != len(out) {
		return nil, fmt.Errorf("%w: protected string buffer size mismatch: want %d got %d", ErrInvalidArchive, want, len(out))
	}
	return out, nil
}

func readInt32(data []byte) int {
	return int(int32(binary.LittleEndian.Uint32(data)))
}

func checkedMul(left, right int) (int, error) {
	maxInt := int(^uint(0) >> 1)
	if left < 0 || right < 0 || (right != 0 && left > maxInt/right) {
		return 0, fmt.Errorf("%w: section size overflow", ErrInvalidArchive)
	}
	return left * right, nil
}
