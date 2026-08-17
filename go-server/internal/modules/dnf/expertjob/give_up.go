package expertjob

import "math"

// GiveUpPlan is the PVF-backed wallet and escalation result of abandoning an
// expert job. State is the cost tier used by the next abandonment.
type GiveUpPlan struct {
	FinalGold  int64
	Cost       int64
	FinalState byte
}

func PlanGiveUp(gold, currentState int64, costs []int64) (GiveUpPlan, error) {
	if gold < 0 || currentState < 0 || currentState > math.MaxUint8 || len(costs) == 0 {
		return GiveUpPlan{}, ErrMachineInvalid
	}
	index := int(currentState)
	if index >= len(costs) {
		index = len(costs) - 1
	}
	cost := costs[index]
	if cost < 0 {
		return GiveUpPlan{}, ErrMachineInvalid
	}
	if gold < cost {
		return GiveUpPlan{}, ErrInsufficientGold
	}
	finalState := index
	if index+1 < len(costs) {
		finalState++
	}
	if finalState > math.MaxUint8 {
		finalState = math.MaxUint8
	}
	return GiveUpPlan{FinalGold: gold - cost, Cost: cost, FinalState: byte(finalState)}, nil
}
