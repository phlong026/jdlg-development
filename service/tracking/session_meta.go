package tracking

import (
	"encoding/json"
	"sort"
)

// sessionMetaChunk bounds one IN list; record IDs are 32-128 characters.
const sessionMetaChunk = 500

// sessionMeta holds the per-session lookups that every fact reduction needs:
// the identity link, the deletion tombstone and the projection state. They used
// to be point reads, three per session, inside a transaction holding the lock.
type sessionMeta struct {
	identity map[string]Record
	deleted  map[string]bool
	state    map[string]summaryState
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// loadSessionMeta reads the requested kinds for every session in a few batched,
// ID-bounded queries. The read budget counts only rows that exist, exactly as
// the point reads did. A session with no row simply has no entry in the maps.
func loadSessionMeta(r Repository, sessions []string, identity, deleted, state bool) (sessionMeta, error) {
	meta := sessionMeta{identity: map[string]Record{}, deleted: map[string]bool{}, state: map[string]summaryState{}}
	sessions = uniqueSorted(sessions)
	for len(sessions) > 0 {
		n := sessionMetaChunk
		if n > len(sessions) {
			n = len(sessions)
		}
		chunk := sessions[:n]
		sessions = sessions[n:]
		if identity {
			rows, err := scopedRecords(r, "identity", RecordQuery{IDs: chunk})
			if err != nil {
				return meta, err
			}
			for _, row := range rows {
				meta.identity[row.ID] = row
			}
		}
		if deleted {
			rows, err := scopedRecords(r, "deletion_session", RecordQuery{IDs: chunk})
			if err != nil {
				return meta, err
			}
			for _, row := range rows {
				meta.deleted[row.ID] = true
			}
		}
		if state {
			rows, err := scopedRecords(r, "summary_state", RecordQuery{IDs: chunk})
			if err != nil {
				return meta, err
			}
			for _, row := range rows {
				var st summaryState
				if json.Unmarshal(row.Payload, &st) != nil {
					return meta, ErrUnavailable
				}
				meta.state[row.ID] = st
			}
		}
	}
	return meta, nil
}
