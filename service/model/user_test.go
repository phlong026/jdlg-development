package model

import (
	"fmt"
	"hash/crc32"
	"testing"
)

func TestBuildMiniprogramUsernameCandidatesUsesFullOpenID(t *testing.T) {
	openid := "o_gjx6yZabcdefghijklmnop123456"
	candidates := buildMiniprogramUsernameCandidates(openid)
	if len(candidates) != 2 {
		t.Fatalf("buildMiniprogramUsernameCandidates(%q) returned %d candidates, want 2", openid, len(candidates))
	}
	wantPrimary := "wx_" + openid
	if candidates[0] != wantPrimary {
		t.Fatalf("primary username = %q, want %q", candidates[0], wantPrimary)
	}
	wantSecondary := fmt.Sprintf("wx_%s_%08x", openid, crc32.ChecksumIEEE([]byte(openid)))
	if candidates[1] != wantSecondary {
		t.Fatalf("secondary username = %q, want %q", candidates[1], wantSecondary)
	}
}

func TestBuildMiniprogramUsernameCandidatesForLongOpenID(t *testing.T) {
	openid := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890_extra_chars"
	candidates := buildMiniprogramUsernameCandidates(openid)
	if len(candidates) != 2 {
		t.Fatalf("buildMiniprogramUsernameCandidates(long openid) returned %d candidates, want 2", len(candidates))
	}
	for index, candidate := range candidates {
		if len(candidate) > 64 {
			t.Fatalf("candidate[%d] length = %d, want <= 64", index, len(candidate))
		}
		if candidate[:3] != "wx_" {
			t.Fatalf("candidate[%d] = %q, want prefix wx_", index, candidate)
		}
	}
	if candidates[0] == candidates[1] {
		t.Fatalf("expected different username candidates for long openid, got %q", candidates[0])
	}
}

func TestBuildMiniprogramUsernameCandidatesEmptyOpenID(t *testing.T) {
	candidates := buildMiniprogramUsernameCandidates("   ")
	if candidates != nil {
		t.Fatalf("expected nil candidates for empty openid, got %#v", candidates)
	}
}
