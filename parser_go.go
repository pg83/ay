package main

import "strings"

func parseGoImports(data []byte) []string {
	var out []string

	s := string(data)
	i := 0

	n := len(s)

	skipSpace := func() {
		for i < n {
			switch {
			case s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n':
				i++
			case strings.HasPrefix(s[i:], "//"):
				for i < n && s[i] != '\n' {
					i++
				}
			case strings.HasPrefix(s[i:], "/*"):
				end := strings.Index(s[i+2:], "*/")

				if end < 0 {
					i = n

					return
				}

				i += 2 + end + 2
			default:
				return
			}
		}
	}

	word := func() string {
		start := i

		for i < n && (s[i] == '_' || s[i] == '.' || 'a' <= s[i]|0x20 && s[i]|0x20 <= 'z' || '0' <= s[i] && s[i] <= '9') {
			i++
		}

		return s[start:i]
	}

	importPath := func() string {
		skipSpace()

		if i < n && (s[i] == '_' || s[i] == '.' || isGoIdentStart(s[i])) {
			word()
			skipSpace()
		}

		if i >= n || s[i] != '"' {
			return ""
		}

		i++

		start := i

		for i < n && s[i] != '"' {
			i++
		}

		p := s[start:i]

		if i < n {
			i++
		}

		return p
	}

	for {
		skipSpace()

		if i >= n {
			return out
		}

		kw := word()

		switch kw {
		case "package":
			skipSpace()
			word()
		case "import":
			skipSpace()

			if i < n && s[i] == '(' {
				i++

				for {
					skipSpace()

					if i >= n {
						return out
					}

					if s[i] == ')' {
						i++

						break
					}

					if p := importPath(); p != "" {
						out = append(out, p)
					} else {
						i++
					}
				}
			} else {
				if p := importPath(); p != "" {
					out = append(out, p)
				}
			}
		default:
			return out
		}
	}
}

func isGoIdentStart(c byte) bool {
	return c == '_' || 'a' <= c|0x20 && c|0x20 <= 'z'
}
