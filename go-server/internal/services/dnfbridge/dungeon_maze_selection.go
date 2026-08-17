package dnfbridge

import (
	"fmt"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

type dungeonMazeSelection struct {
	Index   int
	Reason  string
	QuestID int64
}

// selectDungeonMaze mirrors the PVF domain rule used by df_game_r's
// GetAppropriateMaze flow: an active quest connection wins first, then a
// cleared-quest connection, then one of the non-quest mazes. The current op16
// writer does not carry a proved maze index, so the choice is owned by the
// server's real PVF and persisted quest state rather than by an opaque request
// field.
func selectDungeonMaze(
	mazes []worldmap.Maze,
	difficulty byte,
	record dnfrepo.QuestRecord,
	choose func(int) (int, error),
) (dungeonMazeSelection, error) {
	if len(mazes) == 0 {
		return dungeonMazeSelection{}, worldmap.ErrMazeNotFound
	}
	active, cleared := dungeonQuestStateSets(record)
	if selected, ok, err := selectQuestConnectedMaze(mazes, active, 0, difficulty, choose); ok || err != nil {
		selected.Reason = "active_quest_connection"
		return selected, err
	}
	if selected, ok, err := selectQuestConnectedMaze(mazes, cleared, 1, difficulty, choose); ok || err != nil {
		selected.Reason = "cleared_quest_connection"
		return selected, err
	}

	defaults := make([]int, 0, len(mazes))
	for index := range mazes {
		if len(mazes[index].QuestConnection) == 0 {
			defaults = append(defaults, index)
		}
	}
	if len(defaults) == 0 {
		return dungeonMazeSelection{Index: 0, Reason: "pvf_first_maze_no_default"}, nil
	}
	index, err := chooseDungeonCandidate(defaults, choose)
	if err != nil {
		return dungeonMazeSelection{}, fmt.Errorf("choose non-quest maze: %w", err)
	}
	return dungeonMazeSelection{Index: index, Reason: "pvf_non_quest_maze"}, nil
}

func selectQuestConnectedMaze(
	mazes []worldmap.Maze,
	questIDs map[int64]struct{},
	requiredType int64,
	difficulty byte,
	choose func(int) (int, error),
) (dungeonMazeSelection, bool, error) {
	if len(questIDs) == 0 {
		return dungeonMazeSelection{}, false, nil
	}
	candidates := make([]int, 0, 1)
	for index := range mazes {
		connection := mazes[index].QuestConnection
		if len(connection) < 2 || connection[0] != requiredType {
			continue
		}
		questID := connection[1]
		if _, exists := questIDs[questID]; !exists {
			continue
		}
		if requiredType == 0 && len(connection) >= 3 && connection[2] >= 0 && int64(difficulty) < connection[2] {
			continue
		}
		candidates = append(candidates, index)
	}
	if len(candidates) == 0 {
		return dungeonMazeSelection{}, false, nil
	}
	index, err := chooseDungeonCandidate(candidates, choose)
	if err != nil {
		return dungeonMazeSelection{}, true, fmt.Errorf("choose quest-connected maze: %w", err)
	}
	return dungeonMazeSelection{Index: index, QuestID: mazes[index].QuestConnection[1]}, true, nil
}

func chooseDungeonCandidate(candidates []int, choose func(int) (int, error)) (int, error) {
	if len(candidates) == 0 {
		return 0, worldmap.ErrMazeNotFound
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if choose == nil {
		return 0, fmt.Errorf("%w: candidates=%d", errDungeonChoiceRequired, len(candidates))
	}
	position, err := choose(len(candidates))
	if err != nil {
		return 0, err
	}
	if position < 0 || position >= len(candidates) {
		return 0, fmt.Errorf("dungeon candidate position %d outside [0,%d)", position, len(candidates))
	}
	return candidates[position], nil
}

func dungeonQuestStateSets(record dnfrepo.QuestRecord) (active map[int64]struct{}, cleared map[int64]struct{}) {
	active = make(map[int64]struct{})
	cleared = make(map[int64]struct{})
	collect := func(states map[int64]dnfrepo.QuestState) {
		for questID, state := range states {
			if questID <= 0 {
				continue
			}
			switch normalizeDungeonQuestStatus(state.Status) {
			case "active", "accepted", "inprogress", "progress":
				active[questID] = struct{}{}
			case "complete", "completed", "cleared", "finished", "done":
				cleared[questID] = struct{}{}
			}
		}
	}
	collect(record.States)
	collect(record.Progress)
	return active, cleared
}

func normalizeDungeonQuestStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
