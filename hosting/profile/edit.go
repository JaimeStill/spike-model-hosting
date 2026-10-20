package profile

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Setting is the four keys of a model's entry that set how many requests it serves at once and
// the context they share: its slots and c, each with its reason.
type Setting struct {
	Slots       int
	C           int
	SlotsReason string
	CReason     string
}

// reasonWidth is the column a reason's lines wrap at, the profile's own.
const reasonWidth = 100

// SetSetting returns src, a profile's TOML, with model id's slots, c, slots_reason and c_reason
// replaced by s's, every other byte as it was: the comments, the order and the other entries
// stay. Each key must appear once in the model's own table (from its [[models]] header to the
// next table header). s's reasons are written as multi-line basic strings, their paragraphs a
// blank line apart: a paragraph whose lines fit 100 columns kept line for line, any other wrapped
// at 100. The result must parse as src's profile with those four values alone
// changed, and validate; SetSetting returns an error, and no bytes, otherwise.
func SetSetting(src []byte, id string, s Setting) ([]byte, error) {
	before, err := Parse(src, "the profile")
	if err != nil {
		return nil, err
	}
	if before.Model(id) == nil {
		return nil, fmt.Errorf("profile: no model %q", id)
	}
	for _, r := range []string{s.SlotsReason, s.CReason} {
		if strings.TrimSpace(r) == "" || strings.Contains(r, `"""`) || strings.Contains(r, `\`) {
			return nil, fmt.Errorf("profile: a reason must be text with no backslash or triple quote: %q", r)
		}
	}
	lines := strings.SplitAfter(string(src), "\n")
	start, end, err := modelTable(lines, id)
	if err != nil {
		return nil, err
	}
	table := lines[start:end]
	for _, kv := range []struct {
		key  string
		with []string
	}{
		{"c", []string{fmt.Sprintf("c = %d\n", s.C)}},
		{"slots", []string{fmt.Sprintf("slots = %d\n", s.Slots)}},
		{"c_reason", multiline("c_reason", s.CReason)},
		{"slots_reason", multiline("slots_reason", s.SlotsReason)},
	} {
		if table, err = replaceKey(table, kv.key, kv.with); err != nil {
			return nil, fmt.Errorf("profile: %s's %w", id, err)
		}
	}
	out := []byte(strings.Join(slices.Concat(lines[:start], table, lines[end:]), ""))

	after, err := Parse(out, "the edited profile")
	if err != nil {
		return nil, err
	}
	want := *before
	want.Models = slices.Clone(before.Models)
	m := want.Model(id)
	m.Slots, m.C, m.SlotsReason, m.CReason = s.Slots, s.C, wrapped(s.SlotsReason), wrapped(s.CReason)
	if !reflect.DeepEqual(&want, after) {
		return nil, fmt.Errorf("profile: the edit of %s's setting changed more than its four keys", id)
	}
	return out, nil
}

// modelTable returns the lines of model id's own table, [start, end): from its [[models]] header
// to the next table header, outside any multi-line string.
func modelTable(lines []string, id string) (start, end int, err error) {
	start = -1
	header := -1
	inString := false
	idLine := fmt.Sprintf("id = %q", id)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inString && strings.HasPrefix(trimmed, "[") {
			if start >= 0 {
				return start, i, nil
			}
			header = -1
			if trimmed == "[[models]]" {
				header = i
			}
		}
		if !inString && header >= 0 && trimmed == idLine {
			start = header
		}
		if strings.Count(line, `"""`)%2 == 1 {
			inString = !inString
		}
	}
	if start < 0 {
		return 0, 0, fmt.Errorf("profile: no [[models]] table with %s", idLine)
	}
	return start, len(lines), nil
}

// replaceKey replaces key's one assignment in table, a single line or a multi-line string's
// lines, with with.
func replaceKey(table []string, key string, with []string) ([]string, error) {
	at, to := -1, -1
	inString := false
	for i, line := range table {
		if !inString && (strings.HasPrefix(line, key+" = ") || strings.HasPrefix(line, key+"=")) {
			if at >= 0 {
				return nil, fmt.Errorf("%s appears more than once", key)
			}
			at, to = i, i+1
			if strings.HasSuffix(strings.TrimSpace(line), `= """`) {
				j := i + 1
				for j < len(table) && strings.TrimSpace(table[j]) != `"""` {
					j++
				}
				if j == len(table) {
					return nil, fmt.Errorf("%s's multi-line string never closes", key)
				}
				to = j + 1
			} else if strings.Contains(line, `"""`) {
				return nil, fmt.Errorf("%s isn't a multi-line string opened on its own line", key)
			}
		}
		if strings.Count(line, `"""`)%2 == 1 {
			inString = !inString
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("%s isn't in its table", key)
	}
	return slices.Concat(table[:at], with, table[to:]), nil
}

// multiline is key = text as a multi-line basic string. text's paragraphs, split at blank lines,
// stay a blank line apart: a paragraph whose lines all fit reasonWidth keeps its lines as they
// are, and any other is wrapped at reasonWidth.
func multiline(key, text string) []string {
	out := []string{key + ` = """` + "\n"}
	for i, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if i > 0 {
			out = append(out, "\n")
		}
		lines := strings.Split(strings.TrimSpace(para), "\n")
		if !slices.ContainsFunc(lines, func(l string) bool { return len(l) > reasonWidth }) {
			for _, l := range lines {
				out = append(out, strings.TrimRight(l, " \t")+"\n")
			}
			continue
		}
		var line bytes.Buffer
		for word := range strings.FieldsSeq(para) {
			if line.Len() > 0 && line.Len()+1+len(word) > reasonWidth {
				out = append(out, line.String()+"\n")
				line.Reset()
			}
			if line.Len() > 0 {
				line.WriteByte(' ')
			}
			line.WriteString(word)
		}
		out = append(out, line.String()+"\n")
	}
	return append(out, `"""`+"\n")
}

// wrapped is text as its multi-line string decodes: its lines as multiline writes them, each
// ending in a newline.
func wrapped(text string) string {
	lines := multiline("", text)
	return strings.Join(lines[1:len(lines)-1], "")
}
