package dnfbridge

import (
	"testing"

	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestCurrentDungeonQuestEnemyTypeDoesNotConfuseActorRankWithQuestDomain(t *testing.T) {
	tests := []struct {
		name  string
		spawn worldmap.MonsterSpawn
		want  int64
	}{
		{name: "normal defaults to quest enemy type one", spawn: worldmap.MonsterSpawn{Rank: "[normal]"}, want: 1},
		{name: "dummy defaults to quest enemy type one", spawn: worldmap.MonsterSpawn{Rank: "[dummy]"}, want: 1},
		{name: "champion keeps explicit type one", spawn: worldmap.MonsterSpawn{Rank: "[champion]"}, want: 1},
		{name: "super champion remains quest monster type one", spawn: worldmap.MonsterSpawn{Rank: "[super champion]"}, want: 1},
		{name: "boss remains quest monster type one", spawn: worldmap.MonsterSpawn{Rank: "[boss]"}, want: 1},
		{name: "boss suffix remains quest monster type one", spawn: worldmap.MonsterSpawn{Rank: "[dummy]", SuffixMarker: "[boss]"}, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := currentDungeonQuestEnemyTypeForMonster(runtimeDungeonMonster{Spawn: test.spawn})
			if got != test.want {
				t.Fatalf("enemy type=%d want=%d spawn=%+v", got, test.want, test.spawn)
			}
		})
	}
}
