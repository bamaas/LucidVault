package vault

import (
	"fmt"
	"strings"
)

// yamlSpecialChars contains characters that require a YAML scalar value to be quoted.
const yamlSpecialChars = `:#&[]{}'"*?|>!%@` + "`,"

// QuoteYAMLValue wraps a YAML scalar value in double quotes if it contains
// characters that are special in YAML. Values already wrapped in matching
// quotes are returned as-is. Empty strings return "".
func QuoteYAMLValue(v string) string {
	if v == "" {
		return `""`
	}
	if isQuoted(v) {
		return v
	}
	if needsQuoting(v) {
		return `"` + escapeYAMLDoubleQuoted(v) + `"`
	}
	return v
}

// FixFrontmatter ensures scalar values in YAML frontmatter are properly quoted.
// Content without valid frontmatter (delimited by --- fences) is returned as-is.
func FixFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	// Split on "---" to get: before (empty), frontmatter, rest
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return content
	}

	lines := strings.Split(parts[1], "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines, list items, comments
		if trimmed == "" || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx < 0 {
			continue
		}
		key := trimmed[:colonIdx]
		rest := trimmed[colonIdx+1:]
		// Skip keys with no value (list/mapping start)
		value := strings.TrimSpace(rest)
		if value == "" {
			continue
		}
		if isQuoted(value) {
			continue
		}
		if needsQuoting(value) {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = fmt.Sprintf("%s%s: %s", indent, key, QuoteYAMLValue(value))
		}
	}
	return "---" + strings.Join(lines, "\n") + "---" + parts[2]
}

func isQuoted(v string) bool {
	return (len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"') ||
		(len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'')
}

func needsQuoting(v string) bool {
	if len(v) > 0 && (v[0] == '-' || v[0] == ' ' || v[0] == '\t') {
		return true
	}
	return strings.ContainsAny(v, yamlSpecialChars)
}

func escapeYAMLDoubleQuoted(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}
