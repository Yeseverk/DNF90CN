package protocol

import "testing"

func TestChecksumMatchesLatestClient(t *testing.T) {
	if got := Checksum([]byte("123456789")); got != 3740255209 {
		t.Fatalf("checksum mismatch for text: got %#x", got)
	}

	data := make([]byte, 16)
	for index := range data {
		data[index] = byte(index)
	}
	if got := Checksum(data); got != 2264570445 {
		t.Fatalf("checksum mismatch for bytes: got %#x", got)
	}
}

func TestChecksumRangeRejectsBadRange(t *testing.T) {
	if _, err := ChecksumRange([]byte{1, 2, 3}, 2, 2); err == nil {
		t.Fatal("expected invalid range error")
	}
}
