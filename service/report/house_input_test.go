package report

import (
	"encoding/json"
	"testing"
)

func TestHouseRequirementsParticipateInFrozenHash(t *testing.T) {
	input := seasonalSpaceInput()
	raw, _ := json.Marshal(input)
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["house"] = map[string]any{"style": "新闽派", "courtyard": "yes", "parking": "no"}
	first, _ := json.Marshal(fields)
	if err := json.Unmarshal(first, &input); err != nil {
		t.Fatal(err)
	}
	_, firstHash, err := MarshalDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	fields["house"].(map[string]any)["style"] = "现代简约"
	second, _ := json.Marshal(fields)
	if err := json.Unmarshal(second, &input); err != nil {
		t.Fatal(err)
	}
	_, secondHash, err := MarshalDecisionInput(input)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("changing whole-house requirements did not change frozen input hash")
	}
}

func TestHouseInputRejectsContradictoryOrIncompleteRequirements(t *testing.T) {
	for name, mutate := range map[string]func(*DecisionInput){
		"missing floors":       func(v *DecisionInput) { v.Floors = nil },
		"four floors":          func(v *DecisionInput) { v.Floors = intPointer(4) },
		"missing style":        func(v *DecisionInput) { v.House.Style = "" },
		"unknown parking enum": func(v *DecisionInput) { v.House.Parking = "maybe" },
		"wrong member total":   func(v *DecisionInput) { v.House.Members[0].Count = 7 },
		"room beyond building": func(v *DecisionInput) { v.House.Rooms[0].PreferredFloor = 3 },
		"duplicate room id":    func(v *DecisionInput) { v.House.Rooms = append(v.House.Rooms, v.House.Rooms[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			input := houseDecisionInput(2, 4, FlexibleUseNo)
			mutate(&input)
			if _, err := NormalizeDecisionInput(input); err == nil {
				t.Fatal("invalid house accepted")
			}
		})
	}
	input := houseDecisionInput(2, 4, FlexibleUseNo)
	choice := DecisionConfirmation{DiagnosisID: PlanHouseDecision(input).ID, InputHash: "hash", Choice: ChoiceOptionA, Priorities: input.Priorities}
	if _, err := NormalizeDecisionConfirmation(input, choice); err == nil {
		t.Fatal("legacy option A is not whole-house confirmation")
	}
}

func TestHouseConfigRejectsPerpetualYieldTimeout(t *testing.T) {
	t.Setenv("REPORT_ENABLED", "true")
	t.Setenv("REPORT_PAYMENT_ENABLED", "false")
	t.Setenv("REPORT_MULTI_AGENT_ENABLED", "true")
	t.Setenv("REPORT_DECISION_ENABLED", "true")
	t.Setenv("REPORT_HOUSE_ENABLED", "true")
	t.Setenv("REPORT_JOB_TIMEOUT", "1m")
	t.Setenv("REPORT_LEASE_DURATION", "330s")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("too short timeout would requeue every image forever")
	}
}
