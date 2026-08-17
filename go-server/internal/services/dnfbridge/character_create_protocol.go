package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

var errCharacterRepositoryMissing = errors.New("dnf character repository is missing")

type createCharacterRequest struct {
	job     byte
	nameRaw []byte
	options []byte
}

func parseCreateCharacter(body []byte, maxNameLen int) (createCharacterRequest, error) {
	if len(body) < 6 {
		return createCharacterRequest{}, fmt.Errorf("body too short: %d", len(body))
	}
	nameLen := int(binary.LittleEndian.Uint32(body[1:5]))
	if nameLen < 2 || nameLen > maxNameLen {
		return createCharacterRequest{}, fmt.Errorf("invalid name length: %d", nameLen)
	}
	nameEnd := 5 + nameLen
	if nameEnd > len(body) {
		return createCharacterRequest{}, fmt.Errorf("name exceeds body: end=%d len=%d", nameEnd, len(body))
	}
	return createCharacterRequest{
		job:     body[0],
		nameRaw: append([]byte(nil), body[5:nameEnd]...),
		options: append([]byte(nil), body[nameEnd:]...),
	}, nil
}

func looksOpaqueNoPackUpperCreate(body []byte) bool {
	if len(body) != 24 && len(body) != 32 {
		return false
	}
	for _, value := range body {
		if value != 0 {
			return true
		}
	}
	return false
}

func parseCheckName(body []byte) (string, byte, bool) {
	if len(body) < 5 {
		return "", 0x02, false
	}
	nameLen := int(binary.LittleEndian.Uint32(body[:4]))
	// The current op692 writers emit exactly one DSTR. Do not accept a second
	// field or transport residue after the declared name bytes.
	if nameLen <= 0 || nameLen > 30 || 4+nameLen != len(body) {
		return "", 0x14, false
	}
	name, err := decodeCharacterName(body[4 : 4+nameLen])
	if err != nil {
		return "", 0x14, false
	}
	return name, 0, true
}

func decodeCharacterName(raw []byte) (string, error) {
	for len(raw) > 0 && raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("empty character name")
	}
	// Current NoPack create/name-check DSTR bytes use the Windows local code
	// page, even when that byte sequence also happens to be valid UTF-8. For
	// example D2 A1 D2 BB D2 A1 is the GB18030 name "摇一摇", but treating it
	// as UTF-8 produces three Cyrillic characters that render as question
	// marks after the roster encodes them back to GB18030.
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
	if err != nil {
		return "", fmt.Errorf("decode gb18030 name: %w", err)
	}
	if !utf8.Valid(decoded) {
		return "", fmt.Errorf("decoded name is not utf8")
	}
	return string(decoded), nil
}

func looksNoPackCheckName(body []byte, code byte) bool {
	if code != 0x14 || (len(body) != 8 && len(body) != 16) {
		return false
	}
	for _, value := range body {
		if value != 0 {
			return true
		}
	}
	return false
}
