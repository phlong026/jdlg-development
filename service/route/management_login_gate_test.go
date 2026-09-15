package route

import "testing"

func TestManagementLoginGateCannotBeBypassedByAccountCase(t *testing.T) {
	gate := newManagementLoginGate()
	for i := 0; i < 20; i++ {
		if !gate.allow("Admin") {
			t.Fatal("blocked too soon")
		}
	}
	if gate.allow(" admin ") {
		t.Fatal("account gate bypass")
	}
	if !gate.allow("another") {
		t.Fatal("unrelated account blocked")
	}
}
