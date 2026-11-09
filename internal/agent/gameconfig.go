package agent

// Filling in a game's config files before it starts, as its egg says: the port in server.properties, say.
// It's what Pterodactyl's Wings does. Files that don't exist yet are left for the game to make.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Atrello/bastion/internal/protocol"
)

func applyConfigFiles(id int64, files []protocol.ConfigFile) error {
	if len(files) == 0 {
		return nil
	}
	root, err := openGame(id)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, cf := range files {
		p := insidePath(cf.File)
		info, err := root.Lstat(p)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxEditable {
			continue
		}
		raw, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		var out []byte
		switch cf.Parser {
		case "properties", "ini":
			out = setLines(raw, cf.Find, func(line, key string) (string, bool) {
				k, _, found := strings.Cut(line, "=")
				if !found || strings.TrimSpace(k) != key {
					return "", false
				}
				return key + "=", true
			})
		case "file":
			// Each key is the start of a line; the whole line becomes the value.
			out = setLines(raw, cf.Find, func(line, key string) (string, bool) { return "", strings.HasPrefix(line, key) })
		case "yaml":
			out = setYAML(raw, cf.Find)
		case "json":
			if out, err = setJSON(raw, cf.Find); err != nil {
				return fmt.Errorf("%s: %w", cf.File, err)
			}
		default:
			continue // xml isn't filled in
		}
		if err := root.WriteFile(p, out, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// setLines sets each key on the line match finds for it (with the prefix it gives), or adds it at the end.
func setLines(raw []byte, find map[string]string, match func(line, key string) (string, bool)) []byte {
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	for _, key := range sortedKeys(find) {
		done := false
		for i, line := range lines {
			if prefix, ok := match(strings.TrimSpace(line), key); ok {
				lines[i], done = prefix+find[key], true
			}
		}
		if !done {
			if prefix, ok := match(key+"=", key); ok {
				lines = append(lines, prefix+find[key])
			}
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

// setYAML sets dotted keys ("settings.port") in a YAML file's nested mappings, keeping everything else
// as it is. Lists aren't reached.
func setYAML(raw []byte, find map[string]string) []byte {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 64<<10), maxEditable)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	keyLine := regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+|"[^"]*"|'[^']*'):(\s|$)`)
	for _, key := range sortedKeys(find) {
		var path []string // the keys leading to this line
		var indents []int // and their indents
		for i, line := range lines {
			m := keyLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			indent := len(m[1])
			for len(indents) > 0 && indents[len(indents)-1] >= indent {
				indents, path = indents[:len(indents)-1], path[:len(path)-1]
			}
			name := strings.Trim(m[2], `"'`)
			if strings.Join(append(path, name), ".") == key {
				lines[i] = m[1] + m[2] + ": " + yamlValue(find[key])
				break
			}
			indents, path = append(indents, indent), append(path, name)
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func yamlValue(v string) string {
	if _, err := strconv.ParseFloat(v, 64); err == nil || v == "true" || v == "false" {
		return v
	}
	quoted, _ := json.Marshal(v)
	return string(quoted)
}

// setJSON sets dotted keys ("listeners.0.host", or "listeners[0].host") in a JSON file.
func setJSON(raw []byte, find map[string]string) ([]byte, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("it isn't JSON")
	}
	for _, key := range sortedKeys(find) {
		parts := strings.Split(strings.NewReplacer("[", ".", "]", "").Replace(key), ".")
		var value any = find[key]
		if err := json.Unmarshal([]byte(find[key]), &value); err != nil {
			value = find[key]
		}
		node := doc
		for i, part := range parts {
			last := i == len(parts)-1
			switch n := node.(type) {
			case map[string]any:
				if last {
					n[part] = value
				} else {
					node = n[part]
				}
			case []any:
				idx, err := strconv.Atoi(part)
				if err != nil || idx < 0 || idx >= len(n) {
					node = nil
				} else if last {
					n[idx] = value
				} else {
					node = n[idx]
				}
			default:
				node = nil
			}
			if node == nil {
				break
			}
		}
	}
	return json.MarshalIndent(doc, "", "  ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
