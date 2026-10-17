// Package suites is the quality suites' uv project, evaluation/suites, seen from Go: it embeds
// the project's pyproject.toml, so the versions the project pins are read from the one place
// they are set. The quality stage (package evaluation/gate) checks that a result was written by
// the pinned MTEB or Inspect AI. Its test also runs the claude-code Inspect provider's own test,
// test_claude_code.py, against a fake claude executable, through the pinned uv and Python.
//
// The API:
//   - Pin returns the exact version pyproject.toml pins a package to.
package suites

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed pyproject.toml
var pyproject string

// Pin returns the version pyproject.toml pins the package name to, exactly (name==version), and
// an error when the project doesn't pin it so.
func Pin(name string) (string, error) {
	var p struct {
		Project struct {
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
	}
	if _, err := toml.Decode(pyproject, &p); err != nil {
		return "", fmt.Errorf("evaluation/suites/pyproject.toml: %w", err)
	}
	for _, dep := range p.Project.Dependencies {
		if pkg, version, ok := strings.Cut(dep, "=="); ok && strings.TrimSpace(pkg) == name {
			return strings.TrimSpace(version), nil
		}
	}
	return "", fmt.Errorf("evaluation/suites/pyproject.toml pins no %s exactly", name)
}
