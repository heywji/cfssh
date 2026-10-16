package sshd

import (
	"strings"
	"testing"
)

func TestUnixConfigLocalhost(t *testing.T) {
	cfg := UnixConfig(UnixOptions{
		Port:         2222,
		Listen:       "127.0.0.1",
		HostKeys:     []string{"/etc/ssh/cfssh_host_ed25519_key"},
		SFTP:         "/usr/lib/openssh/sftp-server",
		AllowUser:    "ada",
		PasswordAuth: true,
	})
	for _, want := range []string{
		"Port 2222",
		"ListenAddress 127.0.0.1",
		"AllowUsers ada",
		"PasswordAuthentication yes",
		"PermitRootLogin no",
		"Subsystem sftp /usr/lib/openssh/sftp-server",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("missing %q in\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "Protocol ") {
		t.Fatal("obsolete Protocol directive")
	}
}

func TestPatchWindowsKeepsManagedBlockBeforeMatch(t *testing.T) {
	original := "" +
		"#Port 22\n" +
		"AuthorizedKeysFile\t.ssh/authorized_keys\n" +
		"Subsystem\tsftp\tsftp-server.exe\n" +
		"\n" +
		"Match Group administrators\n" +
		"       AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys\n"
	for i, got := range []string{
		PatchWindows(original, 2222, "127.0.0.1", "ada", true),
		PatchWindows(PatchWindows(original, 2222, "127.0.0.1", "ada", true), 2222, "127.0.0.1", "ada", true),
	} {
		begin := strings.Index(got, markerBegin)
		match := strings.Index(got, "Match Group administrators")
		if begin < 0 || match < 0 || begin > match {
			t.Fatalf("pass %d: managed block must precede Match\n%s", i, got)
		}
		if !strings.HasSuffix(got, "Match Group administrators\n       AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys\n") {
			t.Fatalf("pass %d: Match block must stay last and intact\n%s", i, got)
		}
		if strings.Count(got, markerBegin) != 1 {
			t.Fatalf("pass %d: duplicated managed block\n%s", i, got)
		}
	}
}

func TestPatchWindowsIdempotent(t *testing.T) {
	original := "" +
		"Port 22\n" +
		"# a comment\n" +
		"ListenAddress 0.0.0.0\n" +
		"Subsystem sftp sftp-server.exe\n" +
		"Match Group administrators\n" +
		"       AuthorizedKeysFile __PROGRAMDATA__/ssh/administrators_authorized_keys\n"
	once := PatchWindows(original, 2222, "127.0.0.1", "ada", true)
	twice := PatchWindows(once, 2222, "127.0.0.1", "ada", false)
	if strings.Count(twice, "Port ") != 1 {
		t.Fatalf("port count\n%s", twice)
	}
	if strings.Contains(twice, "Port 22\n") || strings.Contains(twice, "ListenAddress 0.0.0.0") {
		t.Fatalf("old listen leaked\n%s", twice)
	}
	if !strings.Contains(twice, "PasswordAuthentication no") {
		t.Fatal(twice)
	}
	if !strings.Contains(twice, "Subsystem sftp sftp-server.exe") {
		t.Fatal(twice)
	}
	if !strings.Contains(twice, "Match Group administrators") {
		t.Fatal(twice)
	}
	if strings.Count(twice, markerBegin) != 1 {
		t.Fatal(twice)
	}
}
