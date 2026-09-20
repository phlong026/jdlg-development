package report

import (
	"context"
	"errors"
)

const (
	ProtocolHouseV1      = "report-house-v1"
	SchemaHouseV1        = "house-report-v1"
	HouseTemplateVersion = "house-a4-v1"
	HouseSource          = "house-agent-v1"
)

var (
	ErrHousePending   = errors.New("report: house stage checkpointed; continue in next lease")
	ErrImageUncertain = errors.New("report: illustration submission requires reconciliation")
)

func IsDecisionProtocol(protocol string) bool {
	return protocol == ProtocolDecisionV5 || protocol == ProtocolHouseV1
}

type HouseRequirements struct {
	Style         string                 `json:"style"`
	ElderRoom     *bool                  `json:"first_floor_elder_room,omitempty"`
	RoadSide      string                 `json:"road_side,omitempty"`
	Orientation   string                 `json:"orientation,omitempty"`
	NeighborNotes string                 `json:"neighbor_notes,omitempty"`
	Courtyard     string                 `json:"courtyard"`
	Parking       string                 `json:"parking"`
	StartWindow   string                 `json:"start_window,omitempty"`
	Members       []HouseMember          `json:"members,omitempty"`
	Rooms         []HouseRoomRequirement `json:"rooms,omitempty"`
}

type HouseMember struct {
	Role      string   `json:"role"`
	Count     int      `json:"count"`
	Residence string   `json:"residence"`
	Needs     []string `json:"needs,omitempty"`
}

type HouseRoomRequirement struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Count          int    `json:"count"`
	PreferredFloor int    `json:"preferred_floor,omitempty"`
	Required       bool   `json:"required"`
}

type HousePlan struct {
	SchemeCount int      `json:"scheme_count"`
	Focus       []string `json:"focus"`
}

type HouseScheme struct {
	ID            string       `json:"id"`
	Version       string       `json:"version"`
	Name          string       `json:"name"`
	Strategy      string       `json:"strategy"`
	Gain          string       `json:"gain"`
	Tradeoff      string       `json:"tradeoff"`
	Facade        string       `json:"facade"`
	CostSensitive []string     `json:"cost_sensitive"`
	Assumptions   []string     `json:"assumptions"`
	EvidenceRefs  []string     `json:"evidence_refs"`
	Floors        []HouseFloor `json:"floors"`
}

type HouseFloor struct {
	Number        int         `json:"number"`
	Name          string      `json:"name"`
	Summary       string      `json:"summary"`
	Rooms         []HouseRoom `json:"rooms"`
	Circulation   string      `json:"circulation"`
	StairPosition string      `json:"stair_position"`
}

type HouseRoom struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	Count         int    `json:"count"`
	RequirementID string `json:"requirement_id,omitempty"`
}

type HouseAction struct {
	Task         string `json:"task"`
	Owner        string `json:"owner"`
	Prerequisite string `json:"prerequisite"`
	Evidence     string `json:"evidence"`
}

type HouseContent struct {
	Plan          HousePlan         `json:"plan"`
	Schemes       []HouseScheme     `json:"schemes"`
	Comparison    [][]string        `json:"comparison"`
	Budget        []ContentBlock    `json:"budget"`
	DesignerBrief []ContentBlock    `json:"designer_brief"`
	Actions       []HouseAction     `json:"actions"`
	Images        []HouseImageAsset `json:"images"`
}

type ReportIllustration struct {
	Asset   string `json:"asset"`
	Title   string `json:"title"`
	Caption string `json:"caption"`
}

type HouseImageTask struct {
	ID            string `json:"id"`
	SchemeID      string `json:"scheme_id"`
	SchemeVersion string `json:"scheme_version"`
	Kind          string `json:"kind"`
	Floor         int    `json:"floor,omitempty"`
	Prompt        string `json:"prompt"`
	ReferenceID   string `json:"reference_id,omitempty"`
}

type HouseImageAsset struct {
	ID            string `json:"id"`
	Asset         string `json:"asset"`
	SchemeID      string `json:"scheme_id"`
	SchemeVersion string `json:"scheme_version"`
	Kind          string `json:"kind"`
	Floor         int    `json:"floor,omitempty"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Model         string `json:"model"`
	SHA256        string `json:"sha256"`
	ReferenceID   string `json:"reference_id,omitempty"`
}

// Ensure persists a submission before contacting the provider. created is true
// only when this lease generated an asset; callers yield to bound each job.
type HouseImageService interface {
	Ensure(context.Context, *Report, HouseImageTask) (asset HouseImageAsset, created bool, err error)
	Read(context.Context, *Report, HouseImageAsset) ([]byte, error)
}

type AgentImage struct {
	MIMEType string
	Data     []byte
}
