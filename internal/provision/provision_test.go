package provision

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	want := File{
		Hostname:             "ssh.example.com",
		Port:                 2222,
		LocalhostOnly:        true,
		TunnelToken:          "tok-abc",
		TunnelID:             "tid",
		TunnelName:           "cfssh",
		ServiceTokenClientID: "cid",
		ServiceTokenSecret:   "sec",
		ZoneName:             "example.com",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadRejectsIncomplete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	// Missing tunnel token must fail validation on load.
	if err := Save(path, File{Hostname: "ssh.example.com", Port: 2222}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error loading bundle without tunnel token")
	}
}

func TestValidate(t *testing.T) {
	ok := File{Hostname: "h", Port: 22, TunnelToken: "t"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	bad := []File{
		{Hostname: "h", Port: 22},                  // no token
		{TunnelToken: "t", Port: 22},               // no host
		{TunnelToken: "t", Hostname: "h", Port: 0}, // bad port
	}
	for i, f := range bad {
		if err := f.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestExeCandidatesMacBundle(t *testing.T) {
	exe := filepath.Join("/Users/u/Downloads", "CF SSH.app", "Contents", "MacOS", "cfssh")
	got := exeCandidates(exe)
	want := []string{
		filepath.Join("/Users/u/Downloads", "CF SSH.app", "Contents", "MacOS", Name),
		filepath.Join("/Users/u/Downloads", Name),
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExeCandidatesPlainBinary(t *testing.T) {
	exe := filepath.Join("/opt", "cfssh", "cfssh")
	got := exeCandidates(exe)
	if len(got) != 1 || got[0] != filepath.Join("/opt", "cfssh", Name) {
		t.Fatalf("got %q", got)
	}
}
