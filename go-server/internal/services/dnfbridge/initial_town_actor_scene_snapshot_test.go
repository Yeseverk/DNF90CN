package dnfbridge

import (
	"bytes"
	"testing"
)

func TestCurrentTownActorSceneSnapshotBodyMatchesCurrentReader(t *testing.T) {
	body := buildCurrentTownActorSceneSnapshotBody(29)
	if want := []byte{1, 29, 0, 0}; !bytes.Equal(body, want) {
		t.Fatalf("noti0x320 body=%x want=%x", body, want)
	}
}
