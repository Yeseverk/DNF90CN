package pvf

import "fmt"

func (a *Archive) readTextIndex(idx int) (string, error) {
	if cached, ok := a.texts.Load(idx); ok {
		return cached.(string), nil
	}
	raw, err := a.readRawIndex(idx)
	if err != nil {
		return "", err
	}
	var text string
	switch a.items[idx].dataType {
	case 1:
		text = a.decodeScript(raw)
	case 3:
		text = decodeUTF16LE(raw)
	default:
		text = ""
	}
	if stored, loaded := a.texts.LoadOrStore(idx, text); loaded {
		return stored.(string), nil
	}
	return text, nil
}

func (a *Archive) readRawIndex(idx int) ([]byte, error) {
	if idx < 0 || idx >= len(a.items) {
		return nil, fmt.Errorf("%w: file index %d", ErrFileNotFound, idx)
	}
	item := a.items[idx]
	chunk, err := a.chunk(item.chunkIndex)
	if err != nil {
		return nil, err
	}
	if item.dataOffset < 0 || item.dataSize < 0 || item.dataOffset+item.dataSize > len(chunk) {
		return nil, fmt.Errorf("%w: file %d data range is invalid", ErrInvalidArchive, idx)
	}
	out := make([]byte, item.dataSize)
	copy(out, chunk[item.dataOffset:item.dataOffset+item.dataSize])
	return out, nil
}

func (a *Archive) chunk(idx int) ([]byte, error) {
	if idx < 0 || idx >= len(a.groups) {
		return nil, fmt.Errorf("%w: chunk %d is out of range", ErrInvalidArchive, idx)
	}
	if cached, ok := a.chunks.Load(idx); ok {
		return cached.([]byte), nil
	}
	prev := 0
	if idx > 0 {
		prev = a.groups[idx-1].compressedSize
	}
	curr := a.groups[idx].compressedSize
	if curr <= prev {
		return nil, fmt.Errorf("%w: chunk %d compressed range is invalid", ErrInvalidArchive, idx)
	}
	start := a.bodyOff + prev
	size := curr - prev
	if start < 0 || size <= 0 || start+size > len(a.data) {
		return nil, fmt.Errorf("%w: chunk %d exceeds archive", ErrInvalidArchive, idx)
	}
	encrypted := append([]byte(nil), a.data[start:start+size]...)
	switch a.format {
	case FormatProtectedNKPI:
		decryptProtected("bODy", encrypted)
	default:
		decrypt("BodY", encrypted)
	}
	chunk, err := zlibBytes(encrypted)
	if err != nil {
		return nil, fmt.Errorf("%w: chunk %d decompress: %w", ErrInvalidArchive, idx, err)
	}
	if want := a.groups[idx].originalSize; want >= 0 && want != len(chunk) {
		return nil, fmt.Errorf("%w: chunk %d original size mismatch: want %d got %d", ErrInvalidArchive, idx, want, len(chunk))
	}
	if stored, loaded := a.chunks.LoadOrStore(idx, chunk); loaded {
		return stored.([]byte), nil
	}
	return chunk, nil
}
