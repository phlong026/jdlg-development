package report

import (
	"encoding/json"
	"fmt"
)

func normalizeHouseRequirements(input DecisionInput) (*HouseRequirements, error) {
	if input.House == nil || input.Floors == nil || input.Bedrooms == nil || *input.Floors > 3 || *input.Bedrooms > 12 {
		return nil, ErrInvalidInput
	}
	h := *input.House
	h.Style = collapseInputWhitespace(h.Style)
	h.RoadSide = collapseInputWhitespace(h.RoadSide)
	h.Orientation = collapseInputWhitespace(h.Orientation)
	h.NeighborNotes = collapseInputWhitespace(h.NeighborNotes)
	h.StartWindow = collapseInputWhitespace(h.StartWindow)
	if !boundedCleanText(h.Style, 1, 40) || !optionalBoundedText(h.RoadSide, 120) || !optionalBoundedText(h.Orientation, 120) || !optionalBoundedText(h.NeighborNotes, 240) || !optionalBoundedText(h.StartWindow, 100) || !oneOf(h.Courtyard, "yes", "no", "unknown") || !oneOf(h.Parking, "yes", "no", "unknown") || len(h.Members) > 8 || len(h.Rooms) > 12 {
		return nil, ErrInvalidInput
	}
	h.Members = append([]HouseMember(nil), h.Members...)
	permanent, returning := 0, 0
	for i := range h.Members {
		m := &h.Members[i]
		m.Role = collapseInputWhitespace(m.Role)
		if !boundedCleanText(m.Role, 1, 40) || m.Count < 1 || m.Count > 30 || !oneOf(m.Residence, "permanent", "returning") {
			return nil, ErrInvalidInput
		}
		var err error
		if len(m.Needs) > 0 {
			m.Needs, err = normalizeUniqueTexts(m.Needs, 1, 5, 1, 80)
			if err != nil {
				return nil, err
			}
		}
		// The frozen fact has the same bound enforced by both renderers.
		encoded, err := json.Marshal(m)
		if err != nil || len([]rune(string(encoded))) > 500 {
			return nil, ErrInvalidInput
		}
		if m.Residence == "permanent" {
			permanent += m.Count
		} else {
			returning += m.Count
		}
	}
	if len(h.Members) > 0 && (input.PermanentCount == nil || input.ReturningCount == nil || permanent != *input.PermanentCount || returning != *input.ReturningCount) {
		return nil, ErrInvalidInput
	}
	h.Rooms = append([]HouseRoomRequirement(nil), h.Rooms...)
	seen := map[string]bool{}
	for i := range h.Rooms {
		r := &h.Rooms[i]
		r.Name = collapseInputWhitespace(r.Name)
		if !validIdentifier(r.ID, 40) || seen[r.ID] || !boundedCleanText(r.Name, 1, 40) || r.Count < 1 || r.Count > 12 || r.PreferredFloor < 0 || r.PreferredFloor > *input.Floors {
			return nil, ErrInvalidInput
		}
		seen[r.ID] = true
	}
	return &h, nil
}

func addHouseFacts(input DecisionInput, facts map[string]string) {
	if input.House == nil {
		return
	}
	h := input.House
	facts["house.style"] = h.Style
	facts["house.road_side"] = valueOrUnknown(h.RoadSide)
	facts["house.orientation"] = valueOrUnknown(h.Orientation)
	facts["house.neighbors"] = valueOrUnknown(h.NeighborNotes)
	facts["house.courtyard"] = h.Courtyard
	facts["house.parking"] = h.Parking
	facts["house.start_window"] = valueOrUnknown(h.StartWindow)
	facts["house.first_floor_elder_room"] = "unknown"
	if h.ElderRoom != nil {
		if *h.ElderRoom {
			facts["house.first_floor_elder_room"] = "yes"
		} else {
			facts["house.first_floor_elder_room"] = "no"
		}
	}
	for i, m := range h.Members {
		data, err := json.Marshal(m)
		if err == nil {
			facts[fmt.Sprintf("house.member.%02d", i+1)] = string(data)
		}
	}
	for _, r := range h.Rooms {
		data, err := json.Marshal(r)
		if err == nil {
			facts["house.room."+r.ID] = string(data)
		}
	}
}
