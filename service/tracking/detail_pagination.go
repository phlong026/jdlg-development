package tracking

import (
	"encoding/base64"
	"encoding/json"
	"sort"
)

type detailCursor struct {
	Kind      string `json:"kind"`
	Offset    int    `json:"offset"`
	Signature string `json:"signature"`
}

func detailSignature(f Filter, kind, target string, scope Scope) string {
	f.Cursor = ""
	scope.Owners = append([]string{}, scope.Owners...)
	sort.Strings(scope.Owners)
	return hash(string(marshal(struct {
		Filter       Filter
		Kind, Target string
		Scope        Scope
	}{f, kind, target, scope})))
}

// A cursor is a consistency token, not authorization. Every request still reads
// its currently authorized facts. Bind the endpoint, target and complete scope.
func paginateDetail(f Filter, total int, kind, target string, scope Scope) (int, int, string, string, error) {
	sig := detailSignature(f, kind, target, scope)
	encode := func(offset int) string {
		return base64.RawURLEncoding.EncodeToString(marshal(detailCursor{kind, offset, sig}))
	}
	start := 0
	if f.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(f.Cursor)
		var cursor detailCursor
		if e != nil || json.Unmarshal(raw, &cursor) != nil {
			return 0, 0, "", "", fail(400, "详情游标无效，请刷新")
		}
		if cursor.Kind == "" {
			return 0, 0, "", "", fail(409, "详情分页游标已升级，请刷新")
		}
		if cursor.Kind != kind || cursor.Signature != sig || cursor.Offset < 0 || cursor.Offset > 1000000 {
			return 0, 0, "", "", fail(400, "游标与详情对象或权限范围不一致，请刷新")
		}
		start = cursor.Offset
	}
	if start > total {
		start = total
	}
	end := start + f.Limit
	if end > total {
		end = total
	}
	next := ""
	if end < total {
		next = encode(end)
	}
	return start, end, next, encode(0), nil
}
