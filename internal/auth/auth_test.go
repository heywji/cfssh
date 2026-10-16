package auth

import "testing"

func TestRejectsEmptyPassword(t *testing.T) {
	if err := VerifyCurrentUser(""); err == nil {
		t.Fatal("empty password")
	}
	if err := VerifyCurrentUser("   "); err == nil {
		t.Fatal("blank password")
	}
}
