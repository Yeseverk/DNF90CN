package protocol

import "errors"

const checksumPolynomial uint32 = 1303941417

var (
	errChecksumRange = errors.New("dnf checksum range invalid")
	checksumTable    = buildChecksumTable()
)

// Checksum 计算 DNF 最新包校验值。
//
// 对应 IDA：NoPack.exe 0x34545B0 使用表计算，0x3457610 使用
// 0x0D080109 + 0x40B09020 生成多项式 0x4DB89129。
func Checksum(data []byte) uint32 {
	sum, _ := ChecksumRange(data, 0, len(data))
	return sum
}

// ChecksumRange 计算指定区间的 DNF 校验值。
func ChecksumRange(data []byte, offset int, count int) (uint32, error) {
	if count < 0 || offset < 0 || offset > len(data)-count {
		return 0, errChecksumRange
	}
	crc := uint32(4294967295)
	for index := 0; index < count; index++ {
		crc = checksumTable[(crc^uint32(data[offset+index]))&255] ^ (crc >> 8)
	}
	return ^crc, nil
}

func buildChecksumTable() [256]uint32 {
	var table [256]uint32
	bitValue := uint32(1)
	for bit := 128; bit != 0; bit >>= 1 {
		if bitValue&1 != 0 {
			bitValue = checksumPolynomial ^ (bitValue >> 1)
		} else {
			bitValue >>= 1
		}
		for left := 0; left < 256; left += bit << 1 {
			for right := 0; right < bit; right++ {
				table[left+bit+right] = table[left+right] ^ bitValue
			}
		}
	}
	return table
}
