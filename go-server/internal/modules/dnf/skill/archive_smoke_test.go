package skill

import (
	"context"
	"os"
	"testing"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestArchiveSmoke(t *testing.T) {
	archivePath := os.Getenv("DNF_SKILL_PVF")
	if archivePath == "" {
		t.Skip("set DNF_SKILL_PVF to run the real archive smoke")
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: archivePath})
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	index, err := dnfpvf.Build(context.Background(), archive, dnfpvf.BuildOptions{Lists: []string{DefaultList}})
	if err != nil {
		t.Fatalf("build skill index: %v", err)
	}
	table, err := Load(context.Background(), index, Options{})
	if err != nil {
		t.Fatalf("load skill catalog: %v", err)
	}
	snapshot := table.Snapshot()
	t.Logf("real skill catalog: jobs=%d skills=%d", snapshot.Jobs, snapshot.Skills)
	if snapshot.Jobs != 16 || snapshot.Skills == 0 {
		t.Fatalf("unexpected real catalog snapshot: %+v", snapshot)
	}
	upperSlash, ok := table.Find(0, 46)
	if !ok || upperSlash.RequiredLevel != 1 || upperSlash.MaximumLevel <= 0 || !upperSlash.Active {
		t.Fatalf("unexpected swordman skill 46: %+v ok=%t", upperSlash, ok)
	}
}
