// Command profile lists, validates and renders the spike's host-class profiles.
//
// Usage:
//
//	profile engines [-dir profiles] [profile ...]
//	profile validate <profile>
//	profile render presets <profile>
//	profile render unit [-user name] [-hf-home dir] [-models-dir dir] <profile>
//
// A profile is a path to its TOML file, or a name, such as strix-halo, for <dir>/<name>.toml
// under the profiles directory. engines lists each profile's engine, one per line, as
// "<profile> <engine> <owner/repo> <minimum build>"; given no profile, it lists every profile
// in the directory. render writes to standard output.
//
// The unit's host values are the machine's, never a profile's, and are never committed: each
// comes from its flag, or else from SPIKE_SERVICE_USER, SPIKE_HF_HOME or SPIKE_MODELS_DIR in
// the environment.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JaimeStill/spike-model-hosting/profile"
)

// profilesDir is the directory a profile name resolves in, relative to the working directory.
const profilesDir = "profiles"

// errUsage marks an error in how the tool was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run runs the tool with args, reading the environment through getenv, and returns its exit
// status: 0 on success, 1 when a profile fails to load or render, and 2 on a usage error.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	err := dispatch(args, getenv, stdout)
	// A failed write to stderr has nowhere left to be reported, so its errors are dropped.
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "profile: %v\n%s", err, usage)
		return 2
	default:
		// The profile package's errors already start "profile".
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
}

// usage is the tool's usage, printed on -h and after a usage error.
const usage = `usage:
  profile engines [-dir profiles] [profile ...]
  profile validate <profile>
  profile render presets <profile>
  profile render unit [-user name] [-hf-home dir] [-models-dir dir] <profile>
`

// dispatch runs the command args name, writing its output to stdout.
func dispatch(args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: no command", errUsage)
	}
	switch cmd, rest := args[0], args[1:]; cmd {
	case "engines":
		return engines(rest, stdout)
	case "validate":
		return validate(rest, stdout)
	case "render":
		if len(rest) == 0 {
			return fmt.Errorf("%w: render needs presets or unit", errUsage)
		}
		switch what := rest[0]; what {
		case "presets":
			return renderPresets(rest[1:], stdout)
		case "unit":
			return renderUnit(rest[1:], getenv, stdout)
		default:
			return fmt.Errorf("%w: render %q: not presets or unit", errUsage, what)
		}
	default:
		return fmt.Errorf("%w: %q is not a command", errUsage, cmd)
	}
}

// engines lists each profile's engine, its upstream repository and its minimum build.
func engines(args []string, stdout io.Writer) error {
	fs := newFlagSet("engines")
	dir := fs.String("dir", profilesDir, "the profiles directory")
	if err := parse(fs, args); err != nil {
		return err
	}
	paths := fs.Args()
	if len(paths) == 0 {
		var err error
		paths, err = filepath.Glob(filepath.Join(*dir, "*.toml"))
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return fmt.Errorf("profile: no profiles in %s", *dir)
		}
	}
	for _, arg := range paths {
		path := resolve(*dir, arg)
		p, err := profile.Load(path)
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.Base(path), ".toml")
		if _, err := fmt.Fprintf(stdout, "%s %s %s %s\n", name, p.Engine.Name, p.Engine.Repository(), p.Engine.MinBuild); err != nil {
			return err
		}
	}
	return nil
}

// validate loads one profile, which validates it, and says so.
func validate(args []string, stdout io.Writer) error {
	_, path, err := loadOne("validate", args)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s: valid\n", path)
	return err
}

// renderPresets writes the presets of the one profile args name.
func renderPresets(args []string, stdout io.Writer) error {
	p, _, err := loadOne("render presets", args)
	if err != nil {
		return err
	}
	return p.RenderPresets(stdout)
}

// renderUnit writes the unit of the profile args name, with the host values from its flags or
// else the environment.
func renderUnit(args []string, getenv func(string) string, stdout io.Writer) error {
	fs := newFlagSet("render unit")
	var h profile.Host
	fs.StringVar(&h.ServiceUser, "user", getenv("SPIKE_SERVICE_USER"), "the service user the router runs as (SPIKE_SERVICE_USER)")
	fs.StringVar(&h.HFHome, "hf-home", getenv("SPIKE_HF_HOME"), "the router's HF_HOME (SPIKE_HF_HOME)")
	fs.StringVar(&h.ModelsDir, "models-dir", getenv("SPIKE_MODELS_DIR"), "the router's models directory (SPIKE_MODELS_DIR)")
	if err := parse(fs, args); err != nil {
		return err
	}
	p, _, err := loadOne("render unit", fs.Args())
	if err != nil {
		return err
	}
	return p.RenderUnit(stdout, h)
}

// loadOne loads the one profile args name, after any flags.
func loadOne(cmd string, args []string) (*profile.Profile, string, error) {
	fs := newFlagSet(cmd)
	if err := parse(fs, args); err != nil {
		return nil, "", err
	}
	if fs.NArg() != 1 {
		return nil, "", fmt.Errorf("%w: %s takes one profile", errUsage, cmd)
	}
	path := resolve(profilesDir, fs.Arg(0))
	p, err := profile.Load(path)
	return p, path, err
}

// resolve returns the path a profile argument names: the argument itself when it is a TOML
// file's path, or <dir>/<name>.toml for a bare name.
func resolve(dir, arg string) string {
	if strings.HasSuffix(arg, ".toml") || strings.ContainsRune(arg, filepath.Separator) {
		return arg
	}
	return filepath.Join(dir, arg+".toml")
}

// newFlagSet returns a flag set that returns its errors rather than printing them or exiting:
// run reports them, with the tool's usage.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("profile "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parse parses args into fs, marking an error in them as a usage error.
func parse(fs *flag.FlagSet, args []string) error {
	err := fs.Parse(args)
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		return fmt.Errorf("%w: %s: %v", errUsage, fs.Name(), err)
	}
	return err
}
