package dnfbridge

import (
	"bytes"
	"testing"
)

func TestCurrentTownLocationNotificationBodiesMatchCurrentReaders(t *testing.T) {
	position := buildCurrentTownUserPositionNotificationBody(29, 474, 234, 5)
	if want := []byte{29, 0, 218, 1, 234, 0, 5, 100, 0}; !bytes.Equal(position, want) {
		t.Fatalf("noti16 body=%x want=%x", position, want)
	}
	area := buildCurrentTownUserAreaNotificationBody(29, 38, 1, 474, 234, 5, 3)
	if want := []byte{29, 0, 38, 1, 218, 1, 234, 0, 5, 3}; !bytes.Equal(area, want) {
		t.Fatalf("noti17 body=%x want=%x", area, want)
	}
}
