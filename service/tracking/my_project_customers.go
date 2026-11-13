package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const myProjectActivityLimit = 100

// Only metadata goes into the unified customer timeline. Private正文, feedback
// text, media IDs and share credentials stay behind the project API.
type MyProjectCustomerActivity struct {
	ID          string    `json:"activity_id"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	Kind        string    `json:"kind"`
	At          time.Time `json:"occurred_at"`
}

func emptyMyCustomerAccumulator() *myVisitorAccumulator {
	return &myVisitorAccumulator{sessions: map[string]bool{}, shares: map[string]bool{}, contents: map[string]bool{}, entries: map[string]*MyVisitEntry{}, entryCovered: map[string][]interval{}}
}

func (s *Service) myProjectCustomerActivities(ctx context.Context, r Repository, scope Scope, actor int64, f Filter) (map[string][]MyProjectCustomerActivity, bool, error) {
	out := map[string][]MyProjectCustomerActivity{}
	if s.IsContentOperator == nil {
		return out, false, nil
	} // Existing accounts with no project capability still see public traffic.
	projects := map[string]ProjectNoteProject{}
	denied := map[string]bool{}
	truncated := false
	for _, kind := range []string{projectNoteOpenKind, projectNoteFeedbackKind, projectNoteConfirmKind} {
		query := RecordQuery{ProjectDesigner: actor, Owners: permittedOwners(scope, f.Owner), AtFrom: f.Start, AtBefore: f.End, AtThrough: f.AsOf, Descending: true, Limit: myProjectActivityLimit + 1}
		if f.Visitor != "" {
			query.VisitorKeys = []string{f.Visitor}
			query.ExactVisitors = true
		}
		rows, err := pageRecords(r, kind, query)
		if err != nil {
			return nil, false, err
		}
		if len(rows) > myProjectActivityLimit {
			truncated = true
			rows = rows[:myProjectActivityLimit]
		}
		for _, row := range rows {
			if strings.HasPrefix(row.ID, "request:") {
				continue
			}
			var fact struct {
				ProjectID string `json:"project_id"`
				Actor     int64  `json:"actor_id"`
				Confirmer int64  `json:"confirmer_id"`
			}
			if json.Unmarshal(row.Payload, &fact) != nil {
				return nil, false, ErrUnavailable
			}
			customer := fact.Actor
			if kind == projectNoteConfirmKind {
				customer = fact.Confirmer
			}
			if customer <= 0 || customer == actor || fact.ProjectID == "" || denied[fact.ProjectID] {
				continue
			}
			project, found := projects[fact.ProjectID]
			if !found {
				project, err = projectNoteLoadProject(r, fact.ProjectID)
				if err != nil {
					var te *Error
					if errors.As(err, &te) && te.Status == 404 {
						denied[fact.ProjectID] = true
						continue
					}
					return nil, false, err
				}
				if project.DesignerID != actor || project.Owner != row.Owner || !scope.Allows(project.Owner) {
					denied[fact.ProjectID] = true
					continue
				}
				if err = s.projectNoteGrant(ctx, r, project); err != nil {
					var te *Error
					if errors.As(err, &te) && te.Status == 404 {
						denied[fact.ProjectID] = true
						continue
					}
					return nil, false, err
				}
				projects[fact.ProjectID] = project
			}
			key := projectNoteActorKey(customer)
			if row.Visitor != key || project.Owner != row.Owner {
				continue
			}
			if _, err = r.Get("deletion", key); err == nil {
				continue
			} else if !errors.Is(err, ErrNotFound) {
				return nil, false, err
			}
			out[key] = append(out[key], MyProjectCustomerActivity{ID: row.ID, ProjectID: project.ID, ProjectName: project.Name, Kind: kind, At: row.At})
		}
	}
	// A trusted association is a separate fact, never a fabricated page view.
	linkedRows, err := pageRecords(r, projectNoteProjectKind, RecordQuery{Owners: permittedOwners(scope, f.Owner), VisitorKeys: []string{projectNoteActorKey(actor)}, ExactVisitors: true, Descending: true, Limit: myProjectActivityLimit + 1})
	if err != nil {
		return nil, false, err
	}
	if len(linkedRows) > myProjectActivityLimit {
		truncated = true
		linkedRows = linkedRows[:myProjectActivityLimit]
	}
	for _, row := range linkedRows {
		var project ProjectNoteProject
		if json.Unmarshal(row.Payload, &project) != nil {
			return nil, false, ErrUnavailable
		}
		if project.DesignerID != actor || project.Owner != row.Owner || !scope.Allows(project.Owner) {
			continue
		}
		customer, valid := visitorAccount(project.CustomerRef)
		if !valid || customer == actor || (f.Visitor != "" && f.Visitor != project.CustomerRef) {
			continue
		}
		if err = projectNoteRequireProjectActive(r, project.ID); err != nil {
			var te *Error
			if errors.As(err, &te) && te.Status == 404 {
				continue
			}
			return nil, false, err
		}
		if err = s.projectNoteGrant(ctx, r, project); err != nil {
			var te *Error
			if errors.As(err, &te) && te.Status == 404 {
				continue
			}
			return nil, false, err
		}
		if _, err = r.Get("deletion", project.CustomerRef); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return nil, false, err
		}
		out[project.CustomerRef] = append(out[project.CustomerRef], MyProjectCustomerActivity{ID: "linked:" + project.ID, ProjectID: project.ID, ProjectName: project.Name, Kind: "project_linked", At: project.CreatedAt})
	}
	for key := range out {
		sort.Slice(out[key], func(i, j int) bool {
			a, b := out[key][i], out[key][j]
			if a.At.Equal(b.At) {
				return a.ID < b.ID
			}
			return a.At.After(b.At)
		})
	}
	return out, truncated, nil
}

func includeProjectCustomers(aggregates map[string]*myVisitorAccumulator, activities map[string][]MyProjectCustomerActivity) {
	for key, items := range activities {
		a := aggregates[key]
		if a == nil {
			a = emptyMyCustomerAccumulator()
			aggregates[key] = a
		}
		for _, item := range items {
			if a.first.IsZero() || item.At.Before(a.first) {
				a.first = item.At
			}
			if item.At.After(a.last) {
				a.last = item.At
			}
		}
	}
}
