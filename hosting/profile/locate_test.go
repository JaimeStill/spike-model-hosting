package profile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// moduleRoot is the module root, from this package's directory.
const moduleRoot = "../.."

// TestResolveFromAnyDirectory shows a host-class name resolves to its device directory's profile
// from the module root and from below it, relative to the working directory, and that a path
// resolves to itself.
func TestResolveFromAnyDirectory(t *testing.T) {
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dir, want string }{
		{".", "devices/strix-halo/profile.toml"},
		{"evaluation", "../devices/strix-halo/profile.toml"},
		{"hosting/profile/testdata", "../../../devices/strix-halo/profile.toml"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			t.Chdir(filepath.Join(root, tc.dir))
			p, path, err := profile.Open(profile.HostStrixHalo)
			if err != nil {
				t.Fatal(err)
			}
			if path != tc.want || p.HostClass != profile.HostStrixHalo {
				t.Errorf("Open(%q) = %s, host class %q; want %s, %q", profile.HostStrixHalo, path, p.HostClass, tc.want, profile.HostStrixHalo)
			}
		})
	}
	for _, arg := range []string{"skeleton.toml", "testdata/skeleton"} {
		if got, err := profile.Resolve(arg); err != nil || got != arg {
			t.Errorf("Resolve(%q) = %q, %v; want the path itself", arg, got, err)
		}
	}
}

// TestModuleRootNone shows a directory with no go.mod at or above it has no module root.
func TestModuleRootNone(t *testing.T) {
	// The temporary directory sits outside every module, unless its parents hold a go.mod.
	dir := t.TempDir()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			t.Skipf("%s holds a go.mod", d)
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	if root, err := profile.ModuleRoot(dir); err == nil {
		t.Errorf("ModuleRoot(%s) = %s; want an error", dir, root)
	}
}

// TestPaths shows the devices directory lists each host class's profile, named for its directory.
func TestPaths(t *testing.T) {
	paths, err := profile.Paths(filepath.Join(moduleRoot, profile.DevicesDir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range paths {
		names = append(names, profile.Name(p))
	}
	if len(names) != 1 || names[0] != profile.HostStrixHalo {
		t.Errorf("Paths names %q; want [%s]", names, profile.HostStrixHalo)
	}
	if got := profile.Name("elsewhere/custom.toml"); got != "custom" {
		t.Errorf("Name(elsewhere/custom.toml) = %q; want custom", got)
	}
}
