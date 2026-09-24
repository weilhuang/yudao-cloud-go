package message

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var paramPattern = regexp.MustCompile(`\{(.*?)}`)

// parseParams 抽出模板里的 {变量}。
func parseParams(content string) []string {
	found := paramPattern.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(found))
	seen := map[string]bool{}
	for _, item := range found {
		if len(item) < 2 || item[1] == "" || seen[item[1]] {
			continue
		}
		seen[item[1]] = true
		out = append(out, item[1])
	}
	return out
}

func formatContent(content string, params map[string]any) string {
	return paramPattern.ReplaceAllStringFunc(content, func(token string) string {
		match := paramPattern.FindStringSubmatch(token)
		if len(match) < 2 {
			return token
		}
		value, ok := params[match[1]]
		if !ok || value == nil {
			return token
		}
		return fmt.Sprint(value)
	})
}

func missingParam(names []string, params map[string]any) string {
	for _, name := range names {
		if params == nil || params[name] == nil || fmt.Sprint(params[name]) == "" {
			return name
		}
	}
	return ""
}

func encodeParams(names []string) string {
	if names == nil {
		names = []string{}
	}
	raw, _ := json.Marshal(names)
	return string(raw)
}

func decodeParams(raw string) []string {
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil || names == nil {
		return []string{}
	}
	return names
}
