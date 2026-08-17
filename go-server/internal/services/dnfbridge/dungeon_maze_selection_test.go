package dnfbridge

import (
	"testing"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

func TestSelectDungeonMazeUsesPersistedQuestConnectionsBeforeDefaults(t *testing.T) {
	mazes := []worldmap.Maze{
		{Index: 0},
		{Index: 1, QuestConnection: []int64{0, 3145, 0}},
		{Index: 2, QuestConnection: []int64{1, 3144}},
	}
	selected, err := selectDungeonMaze(mazes, 0, dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{
		3145: {Status: "active"},
		3144: {Status: "completed"},
	}}, func(int) (int, error) {
		t.Fatal("single quest-connected maze must not invoke random chooser")
		return 0, nil
	})
	if err != nil || selected.Index != 1 || selected.QuestID != 3145 || selected.Reason != "active_quest_connection" {
		t.Fatalf("active quest selection=%+v err=%v", selected, err)
	}

	selected, err = selectDungeonMaze(mazes, 0, dnfrepo.QuestRecord{Progress: map[int64]dnfrepo.QuestState{
		3144: {Status: "cleared"},
	}}, nil)
	if err != nil || selected.Index != 2 || selected.QuestID != 3144 || selected.Reason != "cleared_quest_connection" {
		t.Fatalf("cleared quest selection=%+v err=%v", selected, err)
	}
}

func TestSelectDungeonMazeHonorsDifficultyAndNonQuestFallback(t *testing.T) {
	mazes := []worldmap.Maze{
		{Index: 0},
		{Index: 1, QuestConnection: []int64{0, 3145, 2}},
		{Index: 2},
	}
	record := dnfrepo.QuestRecord{States: map[int64]dnfrepo.QuestState{3145: {Status: "accepted"}}}
	selected, err := selectDungeonMaze(mazes, 1, record, func(limit int) (int, error) {
		if limit != 2 {
			t.Fatalf("default candidate count=%d", limit)
		}
		return 1, nil
	})
	if err != nil || selected.Index != 2 || selected.QuestID != 0 || selected.Reason != "pvf_non_quest_maze" {
		t.Fatalf("difficulty fallback selection=%+v err=%v", selected, err)
	}

	selected, err = selectDungeonMaze(mazes, 2, record, nil)
	if err != nil || selected.Index != 1 || selected.QuestID != 3145 || selected.Reason != "active_quest_connection" {
		t.Fatalf("difficulty match selection=%+v err=%v", selected, err)
	}
}

func TestSelectDungeonMazeUsesFirstOnlyWhenPVFHasNoDefault(t *testing.T) {
	selected, err := selectDungeonMaze([]worldmap.Maze{
		{Index: 0, QuestConnection: []int64{0, 100}},
		{Index: 1, QuestConnection: []int64{1, 101}},
	}, 0, dnfrepo.QuestRecord{}, nil)
	if err != nil || selected.Index != 0 || selected.Reason != "pvf_first_maze_no_default" {
		t.Fatalf("no-default selection=%+v err=%v", selected, err)
	}
}
