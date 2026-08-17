package dnfbridge

import (
	"context"
	"os"
	"testing"

	dnfhonor "longheng.io/server/internal/modules/dnf/honor"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	dnfrepomemory "longheng.io/server/internal/modules/dnf/repository/memory"
)

func TestCurrentAccountHonorProgressUsesPersistedOrdinaryHonorOnly(t *testing.T) {
	tables, err := dnfhonor.LoadTables(honorServiceTestSource{dnfhonor.TablePath: honorServiceTestDocument()})
	if err != nil {
		t.Fatal(err)
	}
	repositories := dnfrepomemory.NewMemoryGroup()
	if err := repositories.Account.Save(context.Background(), dnfrepo.AccountRecord{
		AccountID: "dnf:1",
		HonorExp:  15,
	}); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		options:    options{accountID: "dnf:1"},
		honorTable: tables,
		repositoryProvider: func() (dnfrepo.Group, bool) {
			return repositories, true
		},
	}
	progress, err := service.currentAccountHonorProgress(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if progress.TotalExperience != 15 || progress.Level != 2 || progress.CurrentLevelExperience != 5 || progress.Grade != 1 {
		t.Fatalf("honor progress = %+v", progress)
	}
}

type honorServiceTestSource map[string]string

func (source honorServiceTestSource) ReadText(path string) (string, error) {
	text, found := source[path]
	if !found {
		return "", os.ErrNotExist
	}
	return text, nil
}

func honorServiceTestDocument() string {
	return `[grade]
1 ` + "`effect/one.ani` `medal.img` 0 `icon.img` 0" + ` 1 0 2 10
[/grade]
[grade]
2 ` + "`effect/two.ani` `medal.img` 2 `icon.img` 1" + ` 3 20
[/grade]
[maxexp on maxlevel]
29
[expert info]
[grade info]
0 ` + "`challenger`" + `
[min lv]
0
[max lv]
0
[/grade info]
[grade info]
1 ` + "`veteran`" + `
[medal img]
` + "`expert-medal.img`" + ` 2
[icon img]
` + "`expert-icon.img`" + ` 3
[min lv]
1
[max lv]
-1
[/grade info]
[/expert info]
[honor expert exp table]
1 ` + "`100`" + ` 2 ` + "`204`" + ` 3 ` + "`9999999999`" + `
[/honor expert exp table]
`
}
