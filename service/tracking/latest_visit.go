package tracking

// latestCountedVisit consumes only facts that already passed the caller's
// authorization, snapshot and active filters. Carry-over visible time does not
// invent a visit in the window. Heartbeats never determine which visit is latest.
// Equal start times use the smaller stable page_view_id, independent of map order.
func latestCountedVisit(current *LatestVisit, fact factView) *LatestVisit {
	if !fact.Count {
		return current
	}
	if current != nil && (fact.Started.Before(current.Started) || fact.Started.Equal(current.Started) && fact.ID >= current.ID) {
		return current
	}
	return &LatestVisit{ID: fact.ID, Started: fact.Started, Visible: fact.Visible, Content: fact.Content, Version: fact.Version}
}
