package main

import (
	"fmt"
	"strings"
	"unsafe"
)

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

func trimSurroundingQuotes(v string) string {
	if len(v) >= 4 && strings.HasPrefix(v, `\"`) && strings.HasSuffix(v, `\"`) {
		return v[2 : len(v)-2]
	}

	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		return v[1 : len(v)-1]
	}

	return v
}

func containsRegexMeta(s string) bool {
	const meta = `\.+*?[]{}()|^$`

	for i := 0; i < len(s); i++ {
		for j := 0; j < len(meta); j++ {
			if s[i] == meta[j] {
				return true
			}
		}
	}

	return false
}

func splitShellWords(s string) []string {
	return splitQuotedWords(s, 0)
}

func splitJavaCommandWords(s string) []string {
	return splitQuotedWords(s, 1)
}

func splitQuotedWords(s string, quotedEscapeSlashes int) []string {
	var out []string

	eachQuotedWord(s, quotedEscapeSlashes, func(word STR) {
		out = append(out, word.string())
	})

	return out
}

func appendQuotedWords(out []ANY, s string, quotedEscapeSlashes int) []ANY {
	eachQuotedWord(s, quotedEscapeSlashes, func(word STR) {
		out = append(out, pathAny(word))
	})

	return out
}

func eachQuotedWord(s string, quotedEscapeSlashes int, emit func(STR)) {
	start := 0

	var block []byte

	k := 0

	var quote byte

	escaped := false

	materialize := func(end int) {
		if block == nil {
			block = internTable.bytes.alloc((len(s)-start)*(1+quotedEscapeSlashes) + 1)
			k = copy(block, s[start:end])
		}
	}

	flush := func(end int) {
		if block != nil {
			if k > 0 {
				emit(internBlock(block, k))
			}
		} else if end > start {
			emit(internStr(s[start:end]))
		}

		block, k = nil, 0
		start = end + 1
	}

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escaped {
			if quotedEscapeSlashes > 0 && quote != 0 && ch != '\'' && ch != '"' && ch != '\\' && ch != '/' {
				for range quotedEscapeSlashes {
					block[k] = '\\'
					k++
				}
			}

			block[k] = ch
			k++
			escaped = false

			continue
		}

		if ch == '\\' {
			materialize(i)
			escaped = true

			continue
		}

		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				block[k] = ch
				k++
			}

			continue
		}

		switch ch {
		case '\t', '\n', '\r', ' ':
			flush(i)
		case '\'', '"':
			materialize(i)
			quote = ch
		default:
			if block != nil {
				block[k] = ch
				k++
			}
		}
	}

	if escaped {
		block[k] = '\\'
		k++
	}

	flush(len(s))
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

func strBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func bytesString(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	return unsafe.String(&b[0], len(b))
}
