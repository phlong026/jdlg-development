package tracking

import (
	"context"
	"errors"
	"time"
)

// A page is one short write transaction. Each query in it returns at most this
// many rows, so a page stays far below the 20,000-row read budget even after
// related and session expansion reads. Variables only so tests can shrink them.
var (
	deletionPageRows    = 500
	deletionSessionPage = 100
)

// Kinds are deleted in this order. Identity links go last: they are what ties a
// formerly anonymous session's rows to the account, so a run interrupted before
// the end can still find every remaining row when it is resumed.
var deletionVisitorKinds = []string{"event", "daily_summary", "business", "share", "crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation", "summary_state", "entry", "session", "identity"}
var deletionSessionKinds = []string{"event", "daily_summary", "business", "share", "crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation", "summary_state", "view_binding", "entry", "session", "identity"}
var deletionAnnotationKinds = []string{"crm", "intent_override", "intent_override_history", "intent_override_boundary", "intent_evaluation"}

// sessionTombstone records that the session was deleted on behalf of key. The
// visitor column makes the set of a deletion's sessions recoverable, so an
// interrupted run can resume after the rows that first revealed those sessions
// (entries, session records) have already been removed.
func sessionTombstone(session, key string, now time.Time) Record {
	rec := record("deletion_session", session, "", now, map[string]any{"deleted_at": now})
	rec.Visitor, rec.Session = key, session
	return rec
}

// tombstonedSessions returns the sessions already tombstoned for key.
func (s *Service) tombstonedSessions(ctx context.Context, key string) (map[string]bool, error) {
	out := map[string]bool{}
	_, err := s.stagedPages(ctx, false, "deletion_session", RecordQuery{VisitorKeys: []string{key}}, func(_ Repository, rows []Record) (bool, error) {
		for _, row := range rows {
			if row.Visitor == key {
				out[row.ID] = true
			}
		}
		return true, nil
	})
	return out, err
}

func cursorAfter(rows []Record, q RecordQuery) RecordQuery {
	last := rows[len(rows)-1]
	q.AfterAt, q.AfterID = last.At, last.ID
	return q
}

// stagedPages walks every page of a query in order, one transaction per page.
// visit returns false to stop early (page budget exhausted).
func (s *Service) stagedPages(ctx context.Context, write bool, kind string, q RecordQuery, visit func(Repository, []Record) (bool, error)) (bool, error) {
	q.Limit = deletionPageRows
	run := s.readQueryTransaction
	if write {
		run = s.queryTransaction
	}
	for {
		var page []Record
		more := true
		err := run(ctx, func(r Repository) error {
			rows, e := pageRecords(r, kind, q)
			if e != nil {
				return e
			}
			page = rows
			if len(rows) == 0 {
				return nil
			}
			more, e = visit(r, rows)
			return e
		})
		if err != nil {
			return false, err
		}
		if !more {
			return false, nil
		}
		if len(page) < q.Limit {
			return true, nil
		}
		q = cursorAfter(page, q)
	}
}

func sessionChunks(sessions map[string]bool) [][]string {
	all := deletionKeys(sessions)
	out := [][]string{}
	for len(all) > 0 {
		n := deletionSessionPage
		if n > len(all) {
			n = len(all)
		}
		out = append(out, all[:n])
		all = all[n:]
	}
	return out
}

// verifyDeletionScope mutates nothing. It requires every row of the visitor and
// of the visitor's verified sessions to belong to an authorized owner, exactly as
// the atomic plan does, and returns those sessions and how many rows were found.
func (s *Service) verifyDeletionScope(ctx context.Context, scope Scope, key string, seed map[string]bool) (map[string]bool, int, error) {
	sessions, found := map[string]bool{}, 0
	for session := range seed {
		sessions[session] = true
	}
	check := func(row Record) error {
		row, err := privacyRecord(row)
		if err != nil {
			return err
		}
		if !scope.Allows(row.Owner) {
			return fail(403, "删除需覆盖该账号全部授权范围")
		}
		if row.Session != "" {
			sessions[row.Session] = true
		}
		found++
		return nil
	}
	for _, kind := range deletionVisitorKinds {
		_, err := s.stagedPages(ctx, false, kind, RecordQuery{VisitorKeys: []string{key}}, func(r Repository, rows []Record) (bool, error) {
			for _, row := range rows {
				visitor, err := privacyVisitor(r, row)
				if err != nil {
					return false, err
				}
				if visitor != key {
					continue
				}
				if err = check(row); err != nil {
					return false, err
				}
			}
			return true, nil
		})
		if err != nil {
			return nil, 0, err
		}
	}
	err := s.readQueryTransaction(ctx, func(r Repository) error {
		for _, kind := range deletionAnnotationKinds {
			row, e := r.Get(kind, key)
			if errors.Is(e, ErrNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			if e = check(row); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	direct := found
	for _, chunk := range sessionChunks(sessions) {
		for _, kind := range deletionSessionKinds {
			_, err := s.stagedPages(ctx, false, kind, RecordQuery{Sessions: chunk}, func(_ Repository, rows []Record) (bool, error) {
				for _, row := range rows {
					if err := check(row); err != nil {
						return false, err
					}
				}
				return true, nil
			})
			if err != nil {
				return nil, 0, err
			}
		}
	}
	return sessions, direct, nil
}

// removeDeletionPage deletes one page of rows and everything that depends on
// them. Dependencies of this page are found by ID, never by scanning a kind.
func (s *Service) removeDeletionPage(r Repository, scope Scope, key string, rows []Record, visitorOnly bool, allSessions map[string]bool) error {
	p := &visitorDeletion{rows: map[string]Record{}, sessions: map[string]bool{}, events: map[string]bool{}, views: map[string]bool{}, sequences: map[string]bool{}, shares: map[string]bool{}}
	for _, row := range rows {
		if visitorOnly {
			visitor, err := privacyVisitor(r, row)
			if err != nil {
				return err
			}
			if visitor != key {
				continue
			}
		}
		if err := p.add(scope, row); err != nil {
			return err
		}
	}
	if len(p.rows) == 0 {
		return nil
	}
	// Only sessions of rows that are really deleted get a tombstone. A row that
	// resolves to another account (an anonymous device later logged in elsewhere)
	// was skipped above and its session must stay readable for that account.
	for session := range p.sessions {
		if _, e := r.Get("deletion_session", session); errors.Is(e, ErrNotFound) {
			if e = r.Put(sessionTombstone(session, key, s.Now())); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
	}
	// Legacy-share evidence must be judged against the whole deletion graph, as
	// the atomic plan does, not only against this page's sessions.
	for session := range allSessions {
		p.sessions[session] = true
	}
	// Anonymous legacy shares are tied to this account only through verified
	// share_intent events, which are in this page while the events still exist.
	if err := p.addLegacyShares(r, scope); err != nil {
		return err
	}
	for _, related := range []struct {
		kind string
		q    RecordQuery
	}{
		{"event_hash", RecordQuery{IDs: deletionKeys(p.events)}},
		{"sequence", RecordQuery{IDs: deletionKeys(p.sequences)}},
		{"view_binding", RecordQuery{IDs: deletionKeys(p.views)}},
		{"share_request", RecordQuery{Shares: deletionKeys(p.shares)}},
	} {
		found, err := scopedRecords(r, related.kind, related.q)
		if err != nil {
			return err
		}
		for _, row := range found {
			if err = p.add(scope, row); err != nil {
				return err
			}
		}
	}
	for _, row := range p.rows {
		if err := r.Remove(row.Kind, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// deleteVisitorStaged is used when the atomic plan exceeds the read budget.
func (s *Service) deleteVisitorStaged(ctx context.Context, scope Scope, key, reason string, maxPages int) (bool, error) {
	known, err := s.tombstonedSessions(ctx, key)
	if err != nil {
		return false, err
	}
	sessions, found, err := s.verifyDeletionScope(ctx, scope, key, known)
	if err != nil {
		return false, err
	}
	var resuming bool
	if err = s.readQueryTransaction(ctx, func(r Repository) error {
		var e error
		_, resuming, e = readDeletionMark(r, key)
		return e
	}); err != nil {
		return false, err
	}
	if found == 0 && !resuming {
		return false, fail(404, "访客不可访问")
	}
	// Tombstones first: from here the data is unreadable and cannot grow.
	err = s.transaction(ctx, func(r Repository) error {
		if _, exists, e := readDeletionMark(r, key); e != nil || exists {
			return e
		}
		now := s.Now()
		if e := r.Put(record("deletion", key, "", now, deletionMark{DeletedAt: now, InProgress: true})); e != nil {
			return e
		}
		return audit(r, "", "delete_visitor_started", scope.Actor, hash(key), now, map[string]string{"reason_hash": hash(reason), "mode": "staged"})
	})
	if err != nil {
		return false, err
	}
	for _, chunk := range sessionChunks(sessions) {
		err = s.transaction(ctx, func(r Repository) error {
			for _, session := range chunk {
				if _, e := r.Get("deletion_session", session); e == nil {
					continue
				} else if !errors.Is(e, ErrNotFound) {
					return e
				}
				if e := r.Put(sessionTombstone(session, key, s.Now())); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			return false, err
		}
	}
	pages := 0
	budgetLeft := func() bool { return maxPages <= 0 || pages < maxPages }
	remove := func(visitorOnly bool) func(Repository, []Record) (bool, error) {
		return func(r Repository, rows []Record) (bool, error) {
			if !budgetLeft() {
				return false, nil
			}
			pages++
			return true, s.removeDeletionPage(r, scope, key, rows, visitorOnly, sessions)
		}
	}
	finished := true
	for _, kind := range deletionVisitorKinds {
		done, err := s.stagedPages(ctx, true, kind, RecordQuery{VisitorKeys: []string{key}}, remove(true))
		if err != nil {
			return false, err
		}
		if !done {
			finished = false
			break
		}
	}
	if finished {
		for _, chunk := range sessionChunks(sessions) {
			for _, kind := range deletionSessionKinds {
				done, err := s.stagedPages(ctx, true, kind, RecordQuery{Sessions: chunk}, remove(false))
				if err != nil {
					return false, err
				}
				if !done {
					finished = false
					break
				}
			}
			if !finished {
				break
			}
		}
	}
	if !finished {
		return false, nil
	}
	// Pre-owner-scoped private annotations used the identity itself as ID.
	err = s.transaction(ctx, func(r Repository) error {
		for _, kind := range deletionAnnotationKinds {
			if _, e := r.Get(kind, key); errors.Is(e, ErrNotFound) {
				continue
			} else if e != nil {
				return e
			}
			if e := r.Remove(kind, key); e != nil {
				return e
			}
		}
		now := s.Now()
		if e := r.Put(record("deletion", key, "", now, deletionMark{DeletedAt: now})); e != nil {
			return e
		}
		return audit(r, "", "delete_visitor", scope.Actor, hash(key), now, map[string]string{"reason_hash": hash(reason), "backup_policy": "restore_must_reapply_tombstones", "mode": "staged"})
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
