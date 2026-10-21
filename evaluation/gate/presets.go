package gate

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"path"
	"strings"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// Presets is the router's presets file, on the host that serves the router: the one file a sweep
// writes. It must be safe for concurrent use.
type Presets interface {
	// Install replaces the file with data, whole: written beside it and renamed over it, so the
	// router never reads a half-written file.
	Install(ctx context.Context, data []byte) error
	// Sum returns the installed file's SHA-256, in hex.
	Sum(ctx context.Context) (string, error)
}

// SSHPresets is the presets file over ssh, as the runbook's "The preset file's write access"
// grants it: the service user writes a temporary file in the file's directory and renames it over
// the file. It runs no command but those on the file and its temporary one, and no sudo.
type SSHPresets struct {
	// Dest is the ssh destination, such as framework; Path, the file, profile.PresetsPath when
	// empty.
	Dest, Path string
	// SSH is the ssh executable, ssh when empty.
	SSH string
}

// file is the presets file's path.
func (s SSHPresets) file() string { return cmp.Or(s.Path, profile.PresetsPath) }

// Install writes data to a temporary file in the file's directory, .<name>.XXXXXX, makes it
// 0644, as an install leaves the file, and renames it over the file; the temporary file is
// removed when any step before the rename fails.
func (s SSHPresets) Install(ctx context.Context, data []byte) error {
	file := s.file()
	tmp := path.Join(path.Dir(file), "."+path.Base(file)+".XXXXXX")
	script := fmt.Sprintf(`set -e; f=$(mktemp %s); trap 'rm -f "$f"' EXIT; cat > "$f"; chmod 0644 "$f"; mv -f "$f" %s; trap - EXIT`,
		shellQuote(tmp), shellQuote(file))
	if _, err := s.run(ctx, script, data); err != nil {
		return fmt.Errorf("installing %s on %s: %w", file, s.Dest, err)
	}
	return nil
}

// Sum runs sha256sum on the file.
func (s SSHPresets) Sum(ctx context.Context) (string, error) {
	out, err := s.run(ctx, "sha256sum -- "+shellQuote(s.file()), nil)
	if err != nil {
		return "", fmt.Errorf("sha256sum %s on %s: %w", s.file(), s.Dest, err)
	}
	sum, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if len(sum) != sha256.Size*2 {
		return "", fmt.Errorf("sha256sum %s on %s: %q is no SHA-256", s.file(), s.Dest, out)
	}
	return sum, nil
}

// run runs script on Dest with stdin, and returns its standard output; a failure carries its
// standard error.
func (s SSHPresets) run(ctx context.Context, script string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cmp.Or(s.SSH, "ssh"), "-o", "BatchMode=yes", s.Dest, script)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// shellQuote quotes s as one word for a POSIX shell.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
