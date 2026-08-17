package dnfbridge

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpet "longheng.io/server/internal/modules/dnf/pet"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

type bridgePetCatalogTestSource map[string]string

func (s bridgePetCatalogTestSource) ReadText(path string) (string, error) {
	if text, found := s[path]; found {
		return text, nil
	}
	return "", fmt.Errorf("missing %s", path)
}

func TestAlignedPetHatchResolverMapsTypedRuntimeCatalogOnlyForHatch(t *testing.T) {
	catalog, err := dnfpet.NewPVFCatalog(bridgePetCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{petCatalog: catalog}

	cached, err := service.currentPetPVFCatalog()
	if err != nil || cached != catalog {
		t.Fatalf("currentPetPVFCatalog catalog=%p want=%p err=%v", cached, catalog, err)
	}
	resolver, err := service.alignedPetHatchResolverForCommand(dnfenum.CmdPacketHatchCreature)
	if err != nil || resolver == nil {
		t.Fatalf("hatch resolver nil=%t err=%v", resolver == nil, err)
	}
	definition, err := resolver(63006)
	if err != nil {
		t.Fatal(err)
	}
	if definition != (alignedcmd.PetHatchResolution{
		EggItemID:      63006,
		HatchedItemID:  63000,
		EggPVFPath:     "equipment/creature/egg_faras.equ",
		HatchedPVFPath: "equipment/creature/faras.equ",
		MinimumLevel:   3,
	}) {
		t.Fatalf("definition=%+v", definition)
	}

	nonHatch, err := service.alignedPetHatchResolverForCommand(dnfenum.CmdPacketRequestHatchedCreature)
	if err != nil || nonHatch != nil {
		t.Fatalf("non-hatch resolver=%v err=%v", nonHatch, err)
	}
}

func TestHandleAlignedGameCommandInjectsTypedPetHatchResolver(t *testing.T) {
	catalog, err := dnfpet.NewPVFCatalog(bridgePetCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	capture := &bridgePetResolverCaptureHandler{}
	previousRegistry := gameAlignedRegistry
	gameAlignedRegistry = alignedcmd.NewRegistry(capture)
	defer func() { gameAlignedRegistry = previousRegistry }()

	repositories := dnfrepomemory.NewMemoryGroup()
	service := &Service{
		options:            options{accountID: "pet-runtime-account"},
		petCatalog:         catalog,
		repositoryProvider: func() (dnfrepo.Group, bool) { return repositories, true },
	}
	session := &gameSession{connID: "pet-runtime-1", selectedCharacterID: 91}
	handled, err := service.handleAlignedGameCommand(
		session,
		byte(dnfenum.GameCmdCommand),
		uint16(dnfenum.CmdPacketHatchCreature),
		[]byte{7, 5, 0},
	)
	if err != nil || !handled {
		t.Fatalf("handleAlignedGameCommand handled=%t err=%v", handled, err)
	}
	if capture.calls != 1 || capture.request.PetHatchResolver == nil {
		t.Fatalf("capture calls=%d resolver_nil=%t", capture.calls, capture.request.PetHatchResolver == nil)
	}
	definition, err := capture.request.PetHatchResolver(63006)
	if err != nil || definition.HatchedItemID != 63000 || definition.MinimumLevel != 3 {
		t.Fatalf("injected definition=%+v err=%v", definition, err)
	}
}

func TestCurrentCreatureStateEntriesProjectsEmptyNameFromPVFAsClientCodePage(t *testing.T) {
	catalog, err := dnfpet.NewPVFCatalog(bridgePetCatalogFixture())
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{petCatalog: catalog}
	input := []dnfrepo.PetEntry{
		{PetKey: "37", CreatureKey: 37, ItemID: 63000, Level: 1, Satiety: 100},
		{PetKey: "38", CreatureKey: 38, ItemID: 63000, Name: "custom", NameRaw: []byte("custom"), Level: 1, Satiety: 100},
	}
	got, projected := service.currentCreatureStateEntriesForWire(input)
	if projected != 1 || len(got) != 2 {
		t.Fatalf("projected=%d got=%+v", projected, got)
	}
	wantGB18030 := []byte{0xCC, 0xF8, 0xCE, 0xE8, 0xB5, 0xC4, 0xCD, 0xF4}
	if got[0].Name != "跳舞的汪" || !bytes.Equal(got[0].NameRaw, wantGB18030) {
		t.Fatalf("projected entry=%+v raw=% X", got[0], got[0].NameRaw)
	}
	if !bytes.Equal(got[1].NameRaw, []byte("custom")) {
		t.Fatalf("custom name was replaced: %+v", got[1])
	}
	if len(input[0].NameRaw) != 0 {
		t.Fatalf("input mutated: %+v", input[0])
	}
}

type bridgePetResolverCaptureHandler struct {
	calls   int
	request alignedcmd.Request
}

func (*bridgePetResolverCaptureHandler) Domain() dnfenum.AlignedDomain {
	return dnfenum.AlignedDomainPet
}

func (h *bridgePetResolverCaptureHandler) Handle(_ context.Context, request alignedcmd.Request) (alignedcmd.Result, error) {
	h.calls++
	h.request = request
	return alignedcmd.Result{
		Handled:         true,
		ResponseAllowed: false,
		Operation:       "capture_pet_hatch_resolver",
		Reason:          "test capture",
	}, nil
}

func bridgePetCatalogFixture() bridgePetCatalogTestSource {
	return bridgePetCatalogTestSource{
		"equipment/equipment.lst":          "63006 `creature/egg_faras.equ`\n63000 `creature/faras.equ`\n",
		"equipment/creature/egg_faras.equ": "[equipment type] `[creature]`\n[output index] 63000\n",
		"equipment/creature/faras.equ":     "[name] `跳舞的汪`\n[equipment type] `[creature]`\n[minimum level] 3\n",
		"creature/creature.lst":            "1 `faras/faras.cre`\n",
		"creature/faras/faras.cre":         "[evolution quest] 0\n[evolution creature id] 0\n[evolution level] 0\n",
		"creature/exptable.tbl": "2 5 9 15 23 33 44 57 71 88 106 126 148 171 196 223 252 283 315 350 " +
			"388 429 472 517 565 615 668 725 785 849 918 993 1073 1158 1245 1335 1430 1532 1642 1759 " +
			"1879 2011 2151 2301 2464 2639 2829 3049 3294 5579\n",
	}
}
