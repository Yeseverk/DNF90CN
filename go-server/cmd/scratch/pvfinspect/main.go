package main

import (
	"context"
	"fmt"

	"longheng.io/server/internal/modules/dnf/adventuregroup"
	platformpvf "longheng.io/server/internal/platform/pvf"
)

func main() {
	archive, err := platformpvf.Open(`D:/DNF/runtime/data/dnf/Script.pvf`)
	if err != nil {
		panic(err)
	}
	tables, err := adventuregroup.LoadComplete(context.Background(), archive)
	if err != nil {
		panic(err)
	}
	runtime := tables.Runtime()
	fmt.Printf("snapshot=%+v shops=%+v capsule=%+v areas=%+v attribute_ids=%d groups=%d character_attributes=%d\n",
		tables.Snapshot(), runtime.ShopCategories, runtime.Capsule, runtime.ExpeditionAreas,
		len(runtime.AttributeIDs), len(runtime.AttributeGroups), len(runtime.CharacterAttributes))
}
