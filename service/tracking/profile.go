package tracking

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// VisitorProfile is account-supplied presentation, never proof of a person's
// real-world identity. Resolvers receive only IDs already present in scoped facts.
type VisitorProfile struct {
	Nickname  string
	AvatarURL string
	// IdentityType labels the account audience (乡墅业主/设计师/…). It drives
	// presentation badges and lead-conversion gating; it is not authorization.
	IdentityType string
}
type VisitorProfileResolver func(context.Context, []int64) (map[int64]VisitorProfile, error)

const visitorProfileBatchSize = 200

func visitorAccount(key string) (int64, bool) {
	if !strings.HasPrefix(key, "u:") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(key, "u:"), 10, 64)
	return id, err == nil && id > 0 && key == "u:"+strconv.FormatInt(id, 10)
}
func visitorPresentation(key string) Visitor {
	v := Visitor{Key: key, State: "identified", Nickname: "已识别账号"}
	if strings.HasPrefix(key, "a:") {
		suffix := key
		if len(suffix) > 6 {
			suffix = suffix[len(suffix)-6:]
		}
		v.State, v.Nickname = "anonymous", "匿名访客 "+suffix
	}
	return v
}

// Do not turn a stored local path, script, obvious private-network URL or credentialed
// URL into a management browser request. Public profile adapters may sanitize
// further; tracking deliberately supports HTTPS images only.
func safeProfileAvatar(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 || !safeURL(raw) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	// WHATWG URL parsing can normalize Unicode digits or hexadecimal IPv4
	// differently from net/url. Require ASCII host presentation and reject
	// numeric hex spellings rather than letting browsers reinterpret them.
	for _, r := range host {
		if r > 127 || r == '%' {
			return ""
		}
	}
	allNumericParts, hasHexPart := true, false
	for _, part := range strings.Split(host, ".") {
		hex := strings.HasPrefix(part, "0x")
		if hex {
			hasHexPart = true
			part = strings.TrimPrefix(part, "0x")
		}
		if part == "" && !hex {
			allNumericParts = false
			break
		}
		for _, r := range part {
			if !(r >= '0' && r <= '9') && !(hex && r >= 'a' && r <= 'f') {
				allNumericParts = false
				break
			}
		}
	}
	if allNumericParts && hasHexPart {
		return ""
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()) {
		return ""
	}
	// Browsers may reinterpret shortened/octal numeric hosts as loopback IPv4.
	numeric := true
	for _, r := range host {
		if r != '.' && (r < '0' || r > '9') {
			numeric = false
			break
		}
	}
	if numeric && net.ParseIP(host) == nil {
		return ""
	}
	return raw
}

func (s *Service) queryProfiles(ctx context.Context, r Repository, facts []factView) (map[string]Visitor, string, error) {
	by := map[string]Visitor{}
	ids := []int64{}
	for _, fact := range facts {
		if _, exists := by[fact.Visitor]; exists {
			continue
		}
		by[fact.Visitor] = visitorPresentation(fact.Visitor)
		if id, ok := visitorAccount(fact.Visitor); ok {
			ids = append(ids, id)
		}
	}
	if s.ResolveVisitorProfiles == nil {
		return by, "", nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > maxQueryRecords {
		return nil, "", ErrQueryTooLarge
	}
	ctx = sourceReadContext(ctx, r)
	for start := 0; start < len(ids); start += visitorProfileBatchSize {
		end := start + visitorProfileBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := append([]int64{}, ids[start:end]...)
		profiles, err := s.ResolveVisitorProfiles(ctx, batch)
		if err != nil {
			return nil, "", err
		}
		// Ignore any unrequested resolver rows. Missing profiles retain placeholders.
		for _, id := range batch {
			key := "u:" + strconv.FormatInt(id, 10)
			v := by[key]
			p, exists := profiles[id]
			if exists {
				if len(p.Nickname) > 1024 || len(p.AvatarURL) > 4096 || len(p.IdentityType) > 64 {
					return nil, "", ErrUnavailable
				}
				if name := strings.TrimSpace(p.Nickname); name != "" {
					v.Nickname = name
				}
				v.AvatarURL = safeProfileAvatar(p.AvatarURL)
				v.IdentityType = strings.TrimSpace(p.IdentityType)
			}
			if bounded, ok := r.(*queryRepository); ok {
				if err := bounded.budget.consume(Record{Payload: marshal(p)}); err != nil {
					return nil, "", err
				}
			}
			by[key] = v
		}
	}
	// Hash the complete candidate set, before matching nickname filters. A renamed
	// nonmatching visitor must invalidate page two just like a renamed matching one.
	keys := make([]string, 0, len(by))
	for key := range by {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	presentation := make([]Visitor, 0, len(keys))
	for _, key := range keys {
		presentation = append(presentation, by[key])
	}
	return by, hash(string(marshal(presentation))), nil
}
func visitorMatches(v Visitor, f Filter) bool {
	if f.Identity != "" && f.Identity != "all" && v.State != f.Identity {
		return false
	}
	if f.VisitorSearch != "" && !strings.Contains(strings.ToLower(v.Nickname+" "+v.Key), strings.ToLower(f.VisitorSearch)) {
		return false
	}
	return f.Search == "" || strings.Contains(strings.ToLower(v.Nickname+" "+v.CRMLabel), strings.ToLower(f.Search))
}
func hasVisitorFilter(f Filter) bool {
	return f.VisitorSearch != "" || f.Identity != "" && f.Identity != "all"
}

// Add a revision to the existing cursor JSON rather than replacing its format.
// In particular the immutable manual-intent revision and first_cursor survive.
func withProfileRevision(cursor, revision string) string {
	if cursor == "" || revision == "" {
		return cursor
	}
	var c map[string]json.RawMessage
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return cursor
	}
	c["profile_revision"] = marshal(revision)
	return base64.RawURLEncoding.EncodeToString(marshal(c))
}
func checkProfileRevision(f Filter, revision string) error {
	if f.Cursor == "" || revision == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(f.Cursor)
	var c struct {
		Revision string `json:"profile_revision"`
	}
	if err != nil || json.Unmarshal(b, &c) != nil {
		return fail(400, "游标无效")
	}
	if c.Revision != revision {
		return fail(409, "访客资料或素材搜索范围已变化，请刷新")
	}
	return nil
}
