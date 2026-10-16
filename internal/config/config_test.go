package config

import "testing"

func TestValidSubdomain(t *testing.T) {
	ok := []string{"a", "ssh", "my-pc", "a1"}
	bad := []string{"", "-a", "a-", "A", "my_pc", "has space", "a.b"}
	for _, s := range ok {
		if !ValidSubdomain(s) {
			t.Fatalf("want valid %q", s)
		}
	}
	for _, s := range bad {
		if ValidSubdomain(s) {
			t.Fatalf("want invalid %q", s)
		}
	}
}

func TestParseEmails(t *testing.T) {
	got, err := ParseEmails(" A@B.com, a@b.com; c@d.com ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "a@b.com" || got[1] != "c@d.com" {
		t.Fatalf("%v", got)
	}
	if _, err := ParseEmails("not-an-email"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParseEmails("  "); err == nil {
		t.Fatal("expected error")
	}
}

func TestHostname(t *testing.T) {
	if Hostname("ssh", "example.com") != "ssh.example.com" {
		t.Fatal(Hostname("ssh", "example.com"))
	}
}
