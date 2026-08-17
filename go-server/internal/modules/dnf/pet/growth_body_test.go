package pet

import (
	"bytes"
	"errors"
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func TestBuildCreatureGrowthBodyFollowsCurrentEXESceneOp102(t *testing.T) {
	normal, err := BuildCreatureGrowthBody(dnfrepo.PetEntry{Level: 7, Exp: 0x12345678})
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{7, 0, 0x78, 0x56, 0x34, 0x12}; !bytes.Equal(normal, want) {
		t.Fatalf("normal=%x want=%x", normal, want)
	}

	mode1, err := BuildCreatureGrowthBody(dnfrepo.PetEntry{
		Level: 9, Exp: 37, ModeFlag: 1, Mode1Field0A: 2, Mode1Field0B: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{9, 1, 37, 0, 0, 0, 2, 3}; !bytes.Equal(mode1, want) {
		t.Fatalf("mode1=%x want=%x", mode1, want)
	}

	if _, err := BuildCreatureGrowthBody(dnfrepo.PetEntry{Level: 0}); !errors.Is(err, errCreatureGrowthInvalid) {
		t.Fatalf("invalid growth error=%v", err)
	}
}
