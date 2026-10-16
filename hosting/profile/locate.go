package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DevicesDir is the directory, under the module root, that holds one directory per host class,
// each named for its class and holding that class's profile in FileName.
const DevicesDir = "devices"

// FileName is the name of a host class's profile file in its device directory.
const FileName = "profile.toml"

// ModuleRoot returns the module root at or above dir: the nearest directory holding a go.mod.
// The commands find a profile by host-class name from it, so they work from any directory of the
// repository.
func ModuleRoot(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("profile: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("profile: %w", err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("profile: no go.mod at or above the working directory to find the module root by")
		}
		dir = parent
	}
}

// IsName reports whether a profile argument is a host-class name, such as strix-halo, rather
// than a path: it names no TOML file and holds no path separator.
func IsName(arg string) bool {
	return !strings.HasSuffix(arg, ".toml") && !strings.ContainsRune(arg, filepath.Separator)
}

// Resolve returns the path a profile argument names: the argument itself when it is a path, or
// for a host-class name, <module root>/DevicesDir/<name>/FileName, the module root found from
// the working directory. A name's path is relative to the working directory, as a path argument
// would be.
func Resolve(arg string) (string, error) {
	if !IsName(arg) {
		return arg, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("profile: %w", err)
	}
	root, err := ModuleRoot(wd)
	if err != nil {
		return "", err
	}
	return relative(wd, filepath.Join(root, DevicesDir, arg, FileName)), nil
}

// Paths returns every host class's profile under devices, a DevicesDir: devices/*/FileName,
// in name order.
func Paths(devices string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(devices, "*", FileName))
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	return paths, nil
}

// Name returns the host-class name a profile's path carries: its device directory's name for a
// FileName, or else the file's name without .toml.
func Name(path string) string {
	if filepath.Base(path) == FileName {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(filepath.Base(path), ".toml")
}

// relative returns path relative to wd, or path itself when it has no relative form.
func relative(wd, path string) string {
	if rel, err := filepath.Rel(wd, path); err == nil {
		return rel
	}
	return path
}

// Open loads and validates the profile a profile argument names, as Resolve finds it: a path, or
// a host-class name. It returns the path it loaded.
func Open(arg string) (*Profile, string, error) {
	path, err := Resolve(arg)
	if err != nil {
		return nil, "", err
	}
	p, err := Load(path)
	return p, path, err
}
