package main

import (
	"fmt"
	"strings"
)

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func reverseStr(s string) string {
	b := []byte(s)

	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}

	return string(b)
}

func dedupKeepOrder(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]

	for _, s := range in {
		if seen[s] {
			continue
		}

		seen[s] = true
		out = append(out, s)
	}

	return out
}

func prefixEach(prefix string, items []string) []string {
	if len(items) == 0 {
		return nil
	}

	out := make([]string, len(items))

	for i, it := range items {
		out[i] = prefix + it
	}

	return out
}

func flagsContain(flags []string, want string) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}

	return false
}

func isTokenPrefix(p, of []string) bool {
	if len(p) > len(of) {
		return false
	}

	for i, tok := range p {
		if of[i] != tok {
			return false
		}
	}

	return true
}

func stringIsTruthy(v string) bool {
	if v == "" {
		return false
	}

	switch strings.ToLower(v) {
	case "false", "f", "no", "n", "off", "0", "net":
		return false
	}

	return true
}

func humanBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0

	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
