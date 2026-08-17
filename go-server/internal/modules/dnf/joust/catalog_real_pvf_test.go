package joust

import (
	"context"
	"os"
	"reflect"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestRealPVFJoustCatalogMatchesCurrentClientRiders(t *testing.T) {
	pvfPath := os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE")
	if pvfPath == "" {
		t.Skip("set DNFBRIDGE_REAL_PVF_SMOKE to verify the live joust catalog")
	}
	archive, err := platformpvf.Open(pvfPath)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := LoadCatalog(context.Background(), archive)
	if err != nil {
		t.Fatal(err)
	}
	riders := catalog.Riders()
	if len(riders) != 12 {
		t.Fatalf("riders=%d want=12", len(riders))
	}
	ids := make([]byte, len(riders))
	attackTypes := make([]byte, len(riders))
	names := make([]string, len(riders))
	for index, rider := range riders {
		ids[index] = rider.ID
		attackTypes[index] = rider.AttackType
		names[index] = rider.Name
	}
	if !reflect.DeepEqual(ids, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}) ||
		!reflect.DeepEqual(attackTypes, []byte{1, 1, 0, 0, 1, 27, 27, 0, 1, 0, 28, 28}) ||
		!reflect.DeepEqual(names, []string{"爱德华", "理查德", "罗兰", "贝奥武夫", "莱奥", "伊萨尔", "吉利特", "席恩", "湖上骑士兰斯洛特", "无双飞将吕布", "骷髅骑士", "机械骑士格弗雷"}) {
		t.Fatalf("ids=%v attack_types=%v names=%v", ids, attackTypes, names)
	}
	tournament, err := catalog.TournamentFor(4321)
	if err != nil {
		t.Fatal(err)
	}
	if tournament.Champion() > 11 {
		t.Fatalf("champion=%d matches=%+v", tournament.Champion(), tournament.Matches)
	}
	crashRound, err := catalog.TournamentFor(51420)
	if err != nil {
		t.Fatal(err)
	}
	if crashRound.Matches[0].Winner == 8 || crashRound.Matches[0].Loser == 8 {
		t.Fatalf("round 51420 included dungeon lord Lancelot: %+v", crashRound.Matches[0])
	}
	for index, match := range crashRound.Matches {
		winner, winnerOK := catalog.rider(match.Winner)
		loser, loserOK := catalog.rider(match.Loser)
		winnerOffset, winnerTypeOK := attackTypeTableOffset(loser.AttackType)
		loserOffset, loserTypeOK := attackTypeTableOffset(winner.AttackType)
		if !winnerOK || !loserOK || !winnerTypeOK || !loserTypeOK ||
			match.WinnerAction != byte(winnerOffset/7) || match.LoserAction != byte(loserOffset/7) {
			t.Fatalf("round 51420 match[%d]=%+v winner=%+v loser=%+v", index, match, winner, loser)
		}
	}
}
