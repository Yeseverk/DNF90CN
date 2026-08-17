package dnfbridge

import (
	"strconv"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func rowStatU8(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback byte) byte {
	return rowU8(row, column, statU8(character, column, fallback))
}

func rowStatU16(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback uint16) uint16 {
	return rowU16(row, column, statU16(character, column, fallback))
}

func rowStatU32(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback uint32) uint32 {
	return rowU32(row, column, statU32(character, column, fallback))
}

func rowStatInt(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback int) int {
	return rowInt(row, column, statInt(character, column, fallback))
}

func rowStatU16NonZero(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback uint16) uint16 {
	if value, ok := rowUintOK(row, column, 16); ok && value != 0 {
		return uint16(value)
	}
	if value, ok := statInt64OK(character, column); ok && value > 0 {
		if value > 0xffff {
			return 0xffff
		}
		return uint16(value)
	}
	return fallback
}

func rowStatU32NonZero(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback uint32) uint32 {
	if value, ok := rowUintOK(row, column, 32); ok && value != 0 {
		return uint32(value)
	}
	if value, ok := statInt64OK(character, column); ok && value > 0 {
		if value > 0xffffffff {
			return 0xffffffff
		}
		return uint32(value)
	}
	return fallback
}

func rowStatIntNonZero(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord, column string, fallback int) int {
	if value, ok := rowIntOK(row, column); ok && value != 0 {
		return value
	}
	if value, ok := statInt64OK(character, column); ok && value > 0 {
		if value > int64(^uint(0)>>1) {
			return int(^uint(0) >> 1)
		}
		return int(value)
	}
	return fallback
}

func csharpUserInfoStatLevel(row dnfrepo.LegacyUserInfoRow, character dnfrepo.CharacterRecord) byte {
	if value, ok := rowUintOK(row, "stat_level", 8); ok && value != 0 {
		return byte(value)
	}
	if value, ok := statInt64OK(character, "stat_level"); ok && value != 0 {
		if character.Level > 0 && value == int64(character.Level) && value != csharpSubtype1ProtocolStatLevel {
			return csharpSubtype1ProtocolStatLevel
		}
		if value < 0 {
			return 0
		}
		if value > 0xff {
			return 0xff
		}
		return byte(value)
	}
	return csharpSubtype1ProtocolStatLevel
}

func statU8(character dnfrepo.CharacterRecord, key string, fallback byte) byte {
	value, ok := character.Stats[key]
	if !ok {
		return fallback
	}
	if value < 0 {
		return 0
	}
	if value > 0xff {
		return 0xff
	}
	return byte(value)
}

func statU16(character dnfrepo.CharacterRecord, key string, fallback uint16) uint16 {
	value, ok := character.Stats[key]
	if !ok {
		return fallback
	}
	if value < 0 {
		return 0
	}
	if value > 0xffff {
		return 0xffff
	}
	return uint16(value)
}

func statU32(character dnfrepo.CharacterRecord, key string, fallback uint32) uint32 {
	value, ok := character.Stats[key]
	if !ok {
		return fallback
	}
	if value < 0 {
		return 0
	}
	if value > 0xffffffff {
		return 0xffffffff
	}
	return uint32(value)
}

func statInt(character dnfrepo.CharacterRecord, key string, fallback int) int {
	value, ok := character.Stats[key]
	if !ok {
		return fallback
	}
	if value < 0 {
		return 0
	}
	if value > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(value)
}

func statInt64OK(character dnfrepo.CharacterRecord, key string) (int64, bool) {
	if character.Stats == nil {
		return 0, false
	}
	value, ok := character.Stats[key]
	return value, ok
}

func rowUintOK(row dnfrepo.LegacyUserInfoRow, column string, bits int) (uint64, bool) {
	if row == nil {
		return 0, false
	}
	value := strings.TrimSpace(row[column])
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseUint(value, 10, bits)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func rowIntOK(row dnfrepo.LegacyUserInfoRow, column string) (int, bool) {
	if row == nil {
		return 0, false
	}
	value := strings.TrimSpace(row[column])
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return int(parsed), true
}

func twoDigit(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}
