package main

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
)

type DocsData struct {
	dirs            []string
	sources         []VFS
	includes        []VFS
	implicit        []VFS
	copies          []DocsCopy
	vars            []string
	varsJSON        string
	config          string
	explicitSources bool
	plantuml        bool
}

type DocsCopy struct {
	from      string
	namespace string
	files     []string
	declSeq   int
}

func (d *DocsData) apply(v UnknownStmt, module string, owner *ModuleData) bool {
	args := anyStrs(v.Args)

	switch v.Name {
	case tokDocsDir:
		d.dirs = append(d.dirs, args...)
	case tokDocsConfig:
		if len(args) > 0 {
			d.config = args[0]
		}
	case tokDocsVars:
		d.vars = append(d.vars, args...)
	case tokDocsIncludeSources:
		for _, a := range args {
			d.includes = append(d.includes, docsSource(a))
		}
	case tokUsePlantuml:
		d.plantuml = true
	case tokDocsCopyFiles:
		c := DocsCopy{from: source(module).string(), namespace: ".", declSeq: owner.nextDeclSeq()}

		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "FROM", "NAMESPACE":
				if i+1 == len(args) {
					throwFmt("DOCS_COPY_FILES: missing value for %s", args[i])
				}

				if args[i] == "FROM" {
					c.from = args[i+1]
				} else {
					c.namespace = args[i+1]
				}

				i++
			default:
				c.files = append(c.files, args[i])
			}
		}

		d.copies = append(d.copies, c)
	default:
		return false
	}

	return true
}

func docsSource(s string) VFS {
	if vfsHasPrefix(s) {
		return intern(s)
	}

	return sourceClean(strings.TrimPrefix(s, "${ARCADIA_ROOT}/"))
}

func (d *DocsData) addSources(fs FS, module string, args []string) {
	srcdir := "."
	exclude := false

	var patterns, excludes []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "SRCDIR":
			if i+1 == len(args) {
				throwFmt("DOCS SRCS: missing SRCDIR")
			}

			i++
			srcdir = args[i]
		case "EXCLUDE":
			exclude = true
		default:
			if exclude {
				excludes = append(excludes, args[i])
			} else {
				patterns = append(patterns, args[i])
			}
		}
	}

	d.explicitSources = true
	var matches []string

	for _, pattern := range patterns {
		rel, ok := allResourceDir(module, path.Join(srcdir, pattern))

		if ok {
			matches = append(matches, ymakeGlob(fs, rel)...)
		}
	}

	for _, match := range dedupKeepOrder(matches) {
		excluded := false

		for _, pattern := range excludes {
			if docsGlobMatches(pattern, strings.TrimPrefix(match, module+"/")) {
				excluded = true

				break
			}
		}

		if !excluded {
			d.sources = append(d.sources, source(match))
		}
	}
}

func docsGlobMatches(pattern, value string) bool {
	p, ps, more := strings.Cut(pattern, "/")
	v, vs, rest := strings.Cut(value, "/")

	if p == "**" {
		return !more || docsGlobMatches(ps, value) || rest && docsGlobMatches(pattern, vs)
	}

	if !globSegMatch(p, v) {
		return false
	}

	if more && rest {
		return docsGlobMatches(ps, vs)
	}

	return more == rest
}

func (d *DocsData) finish(fs FS, module string, env Environment) {
	if len(d.dirs) == 0 && !d.explicitSources {
		d.dirs = append(d.dirs, module)
	} else {
		d.dirs = append(d.dirs, "__dummy_dir__")
	}

	for _, dir := range d.dirs {
		for _, file := range globMatch(fs, path.Join(docsSource(dir).relString(), "**/*")) {
			d.implicit = append(d.implicit, source(file))
		}
	}

	variables := map[string]string{}

	for _, a := range d.vars {
		kv := strings.FieldsFunc(a, func(c rune) bool { return c == '=' })

		if len(kv) != 2 {
			throwFmt("DOCS_VARS: invalid variables specification %q; expected name=value", a)
		}

		value, _ := env.lookup(kv[0])

		if value == "" {
			value = kv[1]
		}

		variables[kv[0]] = value
	}

	if len(variables) > 0 {
		keys := make([]string, 0, len(variables))

		for k := range variables {
			keys = append(keys, k)
		}

		slices.Sort(keys)

		parts := make([]string, 0, len(keys))

		for _, k := range keys {
			parts = append(parts, docsJSONString(k)+": "+docsJSONString(variables[k]))
		}

		d.varsJSON = "{" + strings.Join(parts, ", ") + "}"
	}
}

func docsJSONString(s string) string {
	encoded := string(throw2(json.Marshal(s)))

	encoded = strings.NewReplacer(`\u003c`, "<", `\u003e`, ">", `\u0026`, "&").Replace(encoded)
	var out strings.Builder

	for _, c := range encoded {
		if c < 128 {
			out.WriteRune(c)
		} else {
			if c > 0xffff {
				c -= 0x10000
				out.WriteString(docsUnicodeEscape(0xd800 + c>>10))
				c = 0xdc00 + c&0x3ff
			}

			out.WriteString(docsUnicodeEscape(c))
		}
	}

	return out.String()
}

func docsUnicodeEscape(c rune) string {
	const hex = "0123456789abcdef"

	return string([]byte{'\\', 'u', hex[c>>12&15], hex[c>>8&15], hex[c>>4&15], hex[c&15]})
}
