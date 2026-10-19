package gate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
)

// fakeSSH writes an ssh that runs its command locally, as the host's shell would, and returns its
// path.
func fakeSSH(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "ssh")
	// ssh -o BatchMode=yes <dest> <command>
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nshift 3\nexec sh -c \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

// TestSSHPresets shows SSHPresets replaces the file whole, by a temporary file in its directory
// renamed over it, mode 0644, leaving nothing else in the directory, and reads its SHA-256; an
// install that can't write leaves the file as it was.
func TestSSHPresets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "models.ini")
	if err := os.WriteFile(file, []byte("[*]\nc = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := gate.SSHPresets{Dest: "host", Path: file, SSH: fakeSSH(t)}
	data := []byte("[*]\nc = 2\n")
	if err := p.Install(t.Context(), data); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != string(data) {
		t.Fatalf("the file holds %q (%v); want %q", got, err, data)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o644 {
		t.Errorf("the file's mode %v; want 0644", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the directory holds %d entries after the install; want the file alone", len(entries))
	}
	if s, err := p.Sum(t.Context()); err != nil || s != sha(data) {
		t.Errorf("Sum = %s, %v; want %s", s, err, sha(data))
	}

	bad := gate.SSHPresets{Dest: "host", Path: filepath.Join(dir, "none", "models.ini"), SSH: p.SSH}
	if err := bad.Install(t.Context(), data); err == nil {
		t.Error("an install into a missing directory: no error")
	}
	if _, err := bad.Sum(t.Context()); err == nil {
		t.Error("Sum of a missing file: no error")
	}
	if got, _ := os.ReadFile(file); string(got) != string(data) {
		t.Errorf("a failed install elsewhere changed the file to %q", got)
	}
}
