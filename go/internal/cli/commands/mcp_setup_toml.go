package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/BurntSushi/toml"
)

// The Codex config (~/.codex/config.toml) is hand-edited: it carries comments,
// the user's own key order and other MCP servers. Decoding it into a map and
// re-encoding it (the previous approach) threw all of that away on every
// `wendy mcp setup` — and, through maybeRefreshMCPSetup, on every CLI upgrade.
// So the file is edited as text. Setup owns only the command key of the
// [<topKey>.<name>] table, and its args unless they already start with the
// ones setup writes (a pinned `mcp serve --device <host>` is the user's):
// those lines are replaced in place (or added), and every other byte is left
// alone — including keys and sub-tables the user added to the entry, such as
// env or startup_timeout_sec. When the table does not exist it is appended.
// The result is then decoded and compared with the original, so a scanner
// mistake can never silently change another setting.

// errTOMLUnmanagedEntry reports that the entry (or its parent) is written with
// inline-table or dotted-key syntax. Adding a [table] header next to such a
// definition is invalid TOML — Codex would refuse to start — so the editor
// stops and asks the user instead.
var errTOMLUnmanagedEntry = errors.New("is defined with inline-table or dotted-key syntax")

// tomlLine is one physical line of a TOML document.
type tomlLine struct {
	start, end int      // byte range; end includes the line terminator
	header     []string // key path of a [table] or [[array]] header line
	table      []string // key path of the table a key/value line belongs to
	key        []string // dotted key of a key/value line, relative to table
	trivia     bool     // blank or comment-only line outside any value
	cont       bool     // continues a multi-line value begun on an earlier line
}

// tomlKeyValue is one key = value line to write; value is already rendered.
type tomlKeyValue struct {
	key, value string
}

// scanTOMLLines splits src into lines and classifies them. It tracks
// multi-line strings and open brackets so that a '[' inside a value (a nested
// array or a string) is never mistaken for a table header.
func scanTOMLLines(src []byte) []tomlLine {
	var (
		lines     []tomlLine
		table     []string
		inMLBasic bool // inside """ ... """
		inMLLit   bool // inside ''' ... '''
		depth     int  // open [ and { inside a value
	)
	for start := 0; start < len(src); {
		end := bytes.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start + 1
		}
		text := strings.TrimRight(string(src[start:end]), "\r\n")
		trimmed := strings.TrimLeft(text, " \t")
		clean := !inMLBasic && !inMLLit && depth == 0
		ln := tomlLine{start: start, end: end, cont: !clean}
		switch {
		case clean && (trimmed == "" || strings.HasPrefix(trimmed, "#")):
			ln.trivia = true
		case clean && strings.HasPrefix(trimmed, "["):
			ln.header = parseTOMLKeyPath(strings.TrimLeft(trimmed, "["), ']')
			table = ln.header
		default:
			if clean {
				ln.table, ln.key = table, parseTOMLKeyPath(trimmed, '=')
			}
			inMLBasic, inMLLit, depth = scanTOMLValueLine(text, inMLBasic, inMLLit, depth)
		}
		lines = append(lines, ln)
		start = end
	}
	return lines
}

// scanTOMLValueLine advances the string/bracket state across one line that is
// not a table header.
func scanTOMLValueLine(text string, inMLBasic, inMLLit bool, depth int) (bool, bool, int) {
	for i := 0; i < len(text); i++ {
		switch {
		case inMLBasic:
			if text[i] == '\\' {
				i++
			} else if strings.HasPrefix(text[i:], `"""`) {
				inMLBasic = false
				i += 2
			}
		case inMLLit:
			if strings.HasPrefix(text[i:], "'''") {
				inMLLit = false
				i += 2
			}
		case strings.HasPrefix(text[i:], `"""`):
			inMLBasic = true
			i += 2
		case strings.HasPrefix(text[i:], "'''"):
			inMLLit = true
			i += 2
		case text[i] == '"':
			for i++; i < len(text) && text[i] != '"'; i++ {
				if text[i] == '\\' {
					i++
				}
			}
		case text[i] == '\'':
			for i++; i < len(text) && text[i] != '\''; i++ {
			}
		case text[i] == '#':
			return inMLBasic, inMLLit, depth
		case text[i] == '[' || text[i] == '{':
			depth++
		case text[i] == ']' || text[i] == '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return inMLBasic, inMLLit, depth
}

// parseTOMLKeyPath parses a dotted key — bare, "basic" or 'literal' parts,
// with optional whitespace around dots — up to the stop byte (']' for a
// header, '=' for a key/value line) and returns the unquoted parts.
func parseTOMLKeyPath(s string, stop byte) []string {
	var (
		key  []string
		part strings.Builder
	)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == stop:
			return append(key, part.String())
		case c == ' ' || c == '\t':
		case c == '.':
			key = append(key, part.String())
			part.Reset()
		case c == '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				part.WriteByte(s[i])
			}
		case c == '\'':
			for i++; i < len(s) && s[i] != '\''; i++ {
				part.WriteByte(s[i])
			}
		default:
			part.WriteByte(c)
		}
	}
	return append(key, part.String())
}

// hasKeyPrefix reports whether key starts with prefix.
func hasKeyPrefix(key, prefix []string) bool {
	if len(key) < len(prefix) {
		return false
	}
	for i := range prefix {
		if key[i] != prefix[i] {
			return false
		}
	}
	return true
}

// sameKeyPath reports whether a and b name the same key.
func sameKeyPath(a, b []string) bool {
	return len(a) == len(b) && hasKeyPrefix(a, b)
}

// tomlQuote renders s as a TOML basic string.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tomlStdioServerKeys renders the keys of a stdio MCP server that setup owns,
// in the order they are written.
func tomlStdioServerKeys(command string, args []string) []tomlKeyValue {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = tomlQuote(a)
	}
	return []tomlKeyValue{
		{key: "command", value: tomlQuote(command)},
		{key: "args", value: "[" + strings.Join(quoted, ", ") + "]"},
	}
}

// tomlNewline returns the line terminator src already uses.
func tomlNewline(src []byte) string {
	if i := bytes.IndexByte(src, '\n'); i > 0 && src[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// upsertTOMLTableKeys returns src with the keys kvs set in the table at path.
// When the table has a [header], each key line (with any continuation lines of
// a multi-line value) is replaced in place, and missing keys are inserted after
// the last owned key present, or right after the header. Every other line —
// other keys of the table, comments, sub-tables — is copied unchanged. A
// comment at the end of a replaced key line goes with it. Without a header,
// the whole table is appended to the end of src.
func upsertTOMLTableKeys(src []byte, lines []tomlLine, path []string, kvs []tomlKeyValue) []byte {
	nl := tomlNewline(src)
	header := -1
	spans := make(map[string][2]int, len(kvs)) // owned key -> line range [first, end)
	for i, ln := range lines {
		switch {
		case ln.header != nil && sameKeyPath(ln.header, path):
			header = i
		case ln.key != nil && len(ln.key) == 1 && sameKeyPath(ln.table, path):
			for _, kv := range kvs {
				if ln.key[0] == kv.key {
					end := i + 1
					for end < len(lines) && lines[end].cont {
						end++
					}
					spans[kv.key] = [2]int{i, end}
				}
			}
		}
	}

	if header < 0 {
		var body strings.Builder
		body.WriteString("[" + strings.Join(path, ".") + "]" + nl)
		for _, kv := range kvs {
			body.WriteString(kv.key + " = " + kv.value + nl)
		}
		res := append([]byte(nil), src...)
		if len(bytes.TrimSpace(res)) > 0 {
			if !bytes.HasSuffix(res, []byte("\n")) {
				res = append(res, nl...)
			}
			if !bytes.HasSuffix(res, []byte(nl+nl)) {
				res = append(res, nl...)
			}
		}
		return append(res, body.String()...)
	}

	insertAt := header + 1
	starts := make(map[int]tomlKeyValue, len(spans))
	var missing []tomlKeyValue
	for _, kv := range kvs {
		if s, ok := spans[kv.key]; ok {
			starts[s[0]] = kv
			insertAt = max(insertAt, s[1])
		} else {
			missing = append(missing, kv)
		}
	}
	var out bytes.Buffer
	writeKey := func(kv tomlKeyValue) {
		if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
			out.WriteString(nl) // the previous line was the last, unterminated one
		}
		out.WriteString(kv.key + " = " + kv.value + nl)
	}
	for i := 0; ; {
		if i == insertAt {
			for _, kv := range missing {
				writeKey(kv)
			}
		}
		if i >= len(lines) {
			break
		}
		if kv, ok := starts[i]; ok {
			writeKey(kv)
			i = spans[kv.key][1]
			continue
		}
		out.Write(src[lines[i].start:lines[i].end])
		i++
	}
	return out.Bytes()
}

// canonicalTOML renders a decoded document deterministically (the encoder
// sorts keys) so two decodes can be compared.
func canonicalTOML(doc map[string]any) (string, error) {
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(doc); err != nil {
		return "", err
	}
	return b.String(), nil
}

// tomlEntry returns doc[topKey][name] when it is a table.
func tomlEntry(doc map[string]any, topKey, name string) (map[string]any, bool) {
	top, _ := doc[topKey].(map[string]any)
	entry, ok := top[name].(map[string]any)
	return entry, ok
}

// withoutTOMLKeys returns a copy of doc with keys removed from the
// doc[topKey][name] table, dropping that table, and then topKey, when this
// leaves them empty. Only the maps along that path are copied.
func withoutTOMLKeys(doc map[string]any, topKey, name string, keys ...string) map[string]any {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	top, ok := doc[topKey].(map[string]any)
	if !ok {
		return out
	}
	rest := make(map[string]any, len(top))
	for k, v := range top {
		rest[k] = v
	}
	if entry, ok := top[name].(map[string]any); ok {
		kept := make(map[string]any, len(entry))
		for k, v := range entry {
			kept[k] = v
		}
		for _, k := range keys {
			delete(kept, k)
		}
		if len(kept) == 0 {
			delete(rest, name)
		} else {
			rest[name] = kept
		}
	}
	if len(rest) == 0 {
		delete(out, topKey)
	} else {
		out[topKey] = rest
	}
	return out
}

// upsertCodexMCPServer returns the new config text with [topKey.name] set to a
// stdio server running command with args. Existing args that already start with
// args (a pinned --device) and any other key of that entry are kept. It
// returns src unchanged when the entry is already current, and an error —
// never a partial edit — when the edit is unsafe or would change anything else.
func upsertCodexMCPServer(src []byte, topKey, name, command string, args []string) ([]byte, error) {
	var before map[string]any
	if _, err := toml.Decode(string(src), &before); err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}
	current := func(doc map[string]any) bool {
		entry, ok := tomlEntry(doc, topKey, name)
		return ok && entry["command"] == command && mcpArgsStartWith(entry["args"], args)
	}
	if current(before) {
		return src, nil // nothing to do: keep the user's formatting as is
	}

	path := []string{topKey, name}
	lines := scanTOMLLines(src)
	for _, ln := range lines {
		if ln.key == nil || hasKeyPrefix(ln.table, path) {
			continue // not a key line, or inside the entry's own [tables]
		}
		full := append(append([]string(nil), ln.table...), ln.key...)
		if hasKeyPrefix(full, path) || hasKeyPrefix(path, full) {
			return nil, fmt.Errorf("%s %w; move it into a [%s.%s] table or delete it, then re-run `wendy mcp setup`",
				strings.Join(full[:min(len(full), len(path))], "."), errTOMLUnmanagedEntry, topKey, name)
		}
	}

	var owned []tomlKeyValue
	for _, kv := range tomlStdioServerKeys(command, args) {
		if entry, ok := tomlEntry(before, topKey, name); ok && kv.key == "args" && mcpArgsStartWith(entry["args"], args) {
			continue // the user's own args, e.g. a pinned --device
		}
		owned = append(owned, kv)
	}
	out := upsertTOMLTableKeys(src, lines, path, owned)

	var after map[string]any
	if _, err := toml.Decode(string(out), &after); err != nil {
		return nil, fmt.Errorf("updating [%s.%s] would produce invalid TOML (%v); edit the file by hand", topKey, name, err)
	}
	ownedKeys := make([]string, len(owned))
	for i, kv := range owned {
		ownedKeys[i] = kv.key
	}
	restBefore, errBefore := canonicalTOML(withoutTOMLKeys(before, topKey, name, ownedKeys...))
	restAfter, errAfter := canonicalTOML(withoutTOMLKeys(after, topKey, name, ownedKeys...))
	if err := errors.Join(errBefore, errAfter); err != nil || !current(after) || restBefore != restAfter {
		return nil, fmt.Errorf("could not update [%s.%s] without touching other settings; edit the file by hand", topKey, name)
	}
	return out, nil
}

// addMCPToTOMLConfig points [topKey.name] in the TOML file at path at a stdio
// server, preserving every other byte of the file. An up-to-date file is not
// rewritten, and a symlinked config is updated at its target so the link
// survives.
func addMCPToTOMLConfig(path, topKey, name, command string, args []string) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	src, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	out, err := upsertCodexMCPServer(src, topKey, name, command, args)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(out, src) {
		return nil
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(target, out, mode)
}

// writeFileAtomic writes data to a temp file beside path and renames it into
// place, so a crash never leaves a truncated config behind.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
