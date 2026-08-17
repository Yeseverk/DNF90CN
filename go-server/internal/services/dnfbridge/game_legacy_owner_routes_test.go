package dnfbridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"longheng.io/server/internal/modules/dnf/channelcatalog"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestLegacyStoryDigestUpdateUsesPersistentOwner(t *testing.T) {
	service, repositories := newStoryDigestTestService(t, dnfrepo.CharacterRecord{
		CharacterID: "81",
		AccountID:   defaultAccountPrefix + "1",
		Level:       35,
		Stats:       map[string]int64{},
	})
	session := newStoryDigestTestSession()
	session.selectedCharacterID = 81

	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketStoryDigestUpdate),
		nil,
	); err != nil {
		t.Fatal(err)
	}
	record, found, err := repositories.Character.Load(context.Background(), "81")
	if err != nil || !found {
		t.Fatalf("load advanced character found=%t err=%v", found, err)
	}
	if record.Stats[dnfrepo.CharacterStoryDigestLastLevelStatKey] != 35 ||
		record.Stats[dnfrepo.CharacterStoryDigestMigrationVersionStatKey] != int64(dnfrepo.CurrentCharacterStoryDigestMigrationVersion) {
		t.Fatalf("legacy op1445 did not advance persistent story state=%#v", record.Stats)
	}
	if got := session.conn.(*bufferConn).write.Bytes(); len(got) != 0 {
		t.Fatalf("legacy bodyless op1445 fabricated an ACK=%x", got)
	}
}

func TestLegacyFinishQuestReachesCurrentOwnerBeforeAlignedFallback(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "packet.log")
	logger, err := openPacketLogger(logPath)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{packetLog: logger}
	session := &gameSession{
		conn:                &bufferConn{},
		connID:              "legacy-finish-route",
		channel:             channelcatalog.Channel{ID: 19},
		selectedCharacterID: 19,
	}

	// Nine bytes are deliberately one byte short of the exact current-EXE
	// request. The current quest owner must reject this precise boundary; the
	// generic aligned fallback would instead emit its pending-module marker.
	if err := service.handleGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketFinishQuest),
		make([]byte, 9),
	); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "kind=game-upper-finish-quest-blocked") ||
		!strings.Contains(text, "reason=current_exe_op34_requires_exact_plain_10_byte_body") {
		t.Fatalf("legacy op34 did not reach current owner log=%q", text)
	}
	if strings.Contains(text, "kind=game-aligned-command-pending-module") {
		t.Fatalf("legacy op34 leaked to aligned fallback log=%q", text)
	}
}
