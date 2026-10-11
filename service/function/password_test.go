package function

import "testing"

func TestManagementPasswordKeepsLegacyCompatibility(t *testing.T) {
	password := "LocalCRM!2026"
	legacy := HashPassword(password)
	if !VerifyPassword(password, legacy) || VerifyPassword("wrong", legacy) {
		t.Fatal("legacy verification changed")
	}
	first, err := HashManagementPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashManagementPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !VerifyPassword(password, first) || VerifyPassword("wrong", first) {
		t.Fatal("strong password verification failed")
	}
}
