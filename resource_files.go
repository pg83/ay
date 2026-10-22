package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
)

const (
	yaConfJSONName     = "ya.conf.json"
	yaToolsFormulaTmpl = "build/external_resources/%s/resources.json"
	yaToolSuffix       = ".tool.json"
	yaToolchainSuffix  = ".toolchain.json"
)

func expandResourceFileArgs(args []ANY, env Environment) []string {
	bindir, curdir := env.unbind(envBINDIR), env.unbind(envCURDIR)
	expanded := anyStrs(expandStmtTokens(args, env))

	env.rebind(envBINDIR, bindir)
	env.rebind(envCURDIR, curdir)

	return expanded
}

// TEvalContext::Deref expands each lexical argument once, then splits only
// arguments containing '$' on spaces. Quoting has already been removed by
// makefile_lang.rl6; quotes in an expanded value do not group its words.
func expandResourcePairs(args []ANY, line int, env Environment) ([]ResourcePair, bool) {
	var expanded []string

	for _, arg := range args {
		value := arg.string()
		if strings.ContainsRune(value, '$') {
			value = expandResourceArgument(value, env)
			for _, word := range strings.Split(value, " ") {
				if word != "" {
					expanded = append(expanded, word)
				}
			}
		} else {
			expanded = append(expanded, value)
		}
	}

	// TPluginResourceHandler searches the first two arguments for each flag
	// and advances by the number of distinct flags found, regardless of order.
	dontParse, dontCompress := false, false
	for _, arg := range expanded[:min(2, len(expanded))] {
		dontParse = dontParse || arg == "DONT_PARSE"
		dontCompress = dontCompress || arg == "DONT_COMPRESS"
	}
	start := 0
	if dontParse {
		start++
	}
	if dontCompress {
		start++
	}
	rest := expanded[start:]
	if len(rest)%2 != 0 {
		throwFmt("RESOURCE at line %d: argument count after DONT_PARSE/DONT_COMPRESS strip must be even (got %d)", line, len(rest))
	}

	pairs := make([]ResourcePair, 0, len(rest)/2)
	for i := 0; i < len(rest); i += 2 {
		path := strings.ReplaceAll(rest[i], "$(S)", "${ARCADIA_ROOT}")
		key := strings.ReplaceAll(rest[i+1], "$(S)", "${ARCADIA_ROOT}")
		pairs = append(pairs, ResourcePair{
			Path: strings.ReplaceAll(path, "$(B)", "${ARCADIA_BUILD_ROOT}"),
			Key:  strings.ReplaceAll(key, "$(B)", "${ARCADIA_BUILD_ROOT}"),
		})
	}
	return pairs, dontCompress
}

// lang/expansion.rl6 and TVarsLookup in lang/eval.cpp implement EvalExpr.
// Backslash escapes one character; only $name{key} references are recognized.
// Reserved variables and resource globals remain for the command renderer.
func expandResourceArgument(value string, env Environment) string {
	return expandMakeExpression(value, env, keepDeferredVar)
}

func expandMakeExpression(value string, env Environment, reserved func(string) bool) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] == '\\' && i+1 < len(value) {
			out.WriteByte(value[i+1])
			i += 2
			continue
		}
		if value[i] == '$' {
			j := i + 1
			for j < len(value) && (value[j] >= 'a' && value[j] <= 'z' || value[j] >= 'A' && value[j] <= 'Z' || value[j] >= '0' && value[j] <= '9') {
				j++
			}
			if j < len(value) && value[j] == '{' {
				end := strings.IndexAny(value[j+1:], "${}")
				if end > 0 && value[j+1+end] == '}' {
					end += j + 1
					macro, key := value[i+1:j], value[j+1:end]
					replacement, _ := env.lookup(key)
					expand := macro == "" && !reserved(key)
					if macro == "ENV" {
						replacement, expand = os.Getenv(key), true
					}
					if expand {
						out.WriteString(replacement)
					} else {
						out.WriteString(value[i : end+1])
					}
					i = end + 1
					continue
				}
			}
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

func expandResourceFiles(d *ModuleData, out []ResourceEntry, args []string) []ResourceEntry {
	prefix := ""
	prefixToStrip := ""
	dest := ""
	dontCompress := false
	i := 0

	for i < len(args) {
		tok := args[i]

		switch tok {
		case "DONT_COMPRESS":
			dontCompress = true
			i++
		case "PREFIX":
			if i+1 >= len(args) {
				throwFmt("RESOURCE_FILES: PREFIX is the last token; expected a prefix value")
			}

			prefix = args[i+1]
			dest = ""
			i += 2
		case "DEST":
			if i+1 >= len(args) {
				throwFmt("RESOURCE_FILES: DEST is the last token; expected a dest value")
			}

			dest = args[i+1]
			prefix = ""
			i += 2
		case "STRIP":
			if i+1 >= len(args) {
				throwFmt("RESOURCE_FILES: STRIP is the last token; expected a prefix-to-strip value")
			}

			prefixToStrip = args[i+1]
			i += 2
		default:
			path := tok

			i++

			keyTail := path

			if prefixToStrip != "" && strings.HasPrefix(keyTail, prefixToStrip) {
				keyTail = keyTail[len(prefixToStrip):]
			}

			var fileKey string

			if dest != "" {
				fileKey = d.resStr2("resfs/file/", dest)
			} else {
				fileKey = d.resStr4("resfs/file/", prefix, keyTail, "")
			}

			out = append(out, ResourceEntry{Path: "-", Key: fileKey, SrcPath: path, DontCompress: dontCompress})
			out = append(out, ResourceEntry{Path: path, Key: fileKey, DontCompress: dontCompress})
		}
	}

	return out
}

func expandAllResourceFiles(d *ModuleData, out []ResourceEntry, fs FS, modulePath string, env Environment, stmt *AllResourceFilesStmt) []ResourceEntry {
	prefix := ""
	strip := ""

	var ext string
	var dirs []ANY

	i := 0

	if !stmt.FromDirs {
		ext = expandStmtTokenAny(stmt.Args[0], env).string()
		i = 1
	}

	for i < len(stmt.Args) {
		switch stmt.Args[i] {
		case kwPREFIX.any():
			if i+1 < len(stmt.Args) {
				prefix = expandStmtTokenAny(stmt.Args[i+1], env).string()
			}

			i += 2
		case kwSTRIP.any():
			if i+1 < len(stmt.Args) {
				strip = expandStmtTokenAny(stmt.Args[i+1], env).string()
			}

			i += 2
		default:
			dirs = append(dirs, stmt.Args[i])
			i++
		}
	}

	suffix := ""

	if !stmt.FromDirs {
		suffix = "." + ext
	}

	rfArgs := make([]string, 0, len(dirs)*8)

	if prefix != "" {
		rfArgs = append(rfArgs, "PREFIX", prefix)
	}

	rfArgs = append(rfArgs, "STRIP", d.resStr4("${ARCADIA_ROOT}/", modulePath, "/", strip))

	dedupers.with(func(deduper *DeDuper) {
		for _, dir := range dirs {
			rel, ok := allResourceDir(modulePath, expandStmtTokenAny(dir, env).string())

			if !ok {
				continue
			}

			for _, match := range globMatch(fs, rel+"/*"+suffix) {
				if deduper.add(internStr(match).strID()) {
					rfArgs = append(rfArgs, d.resStr2("${ARCADIA_ROOT}/", match))
				}
			}
		}
	})

	return expandResourceFiles(d, out, rfArgs)
}

func allResourceDir(modulePath, dir string) (rel string, ok bool) {
	switch {
	case strings.HasPrefix(dir, "$(S)/"):
		rel = dir[len("$(S)/"):]
	case strings.HasPrefix(dir, "${ARCADIA_ROOT}/"):
		rel = dir[len("${ARCADIA_ROOT}/"):]
	case strings.HasPrefix(dir, "$") || strings.HasPrefix(dir, "/"):
		return "", false
	default:
		rel = modulePath + "/" + dir
	}

	return path.Clean(rel), true
}

func globMatch(fs FS, pattern string) []string {
	segs := strings.Split(pattern, "/")
	dirs := []string{""}

	var matches []string

	for i, seg := range segs {
		last := i == len(segs)-1
		wild := strings.ContainsAny(seg, "*?")

		var next []string

		for _, d := range dirs {
			if seg == "**" {
				if !last {
					queue := []string{d}

					for head := 0; head < len(queue); head++ {
						current := queue[head]

						for _, packed := range fs.listdir(dirKey(current)).names {
							if packed&1 != 0 {
								child := joinRel(current, STR(packed>>1).string())

								next = append(next, child)
								queue = append(queue, child)
							}
						}
					}

					next = append(next, d)

					continue
				}

				fs.walk(d, func(rel string, isDir bool) bool {
					if !isDir {
						matches = append(matches, rel)
					}

					return true
				})

				continue
			}

			if !wild {
				child := joinRel(d, seg)
				present, isDir := fs.exists(srcRootRel, child)

				if !present {
					continue
				}

				if last {
					if !isDir {
						matches = append(matches, child)
					}
				} else if isDir {
					next = append(next, child)
				}

				continue
			}

			view := fs.listdir(internStr(d))

			for _, packed := range view.names {
				name := STR(packed >> 1).string()

				if !globSegMatch(seg, name) {
					continue
				}

				isDir := packed&1 != 0
				child := joinRel(d, name)

				if last {
					if !isDir {
						matches = append(matches, child)
					}
				} else if isDir {
					next = append(next, child)
				}
			}
		}

		dirs = next
	}

	return matches
}

func globSegMatch(pat, name string) bool {
	var px, nx, starPx, starNx int
	starPx = -1

	for nx < len(name) {
		if px < len(pat) && (pat[px] == '?' || pat[px] == name[nx]) {
			px++
			nx++
		} else if px < len(pat) && pat[px] == '*' {
			starPx = px
			starNx = nx
			px++
		} else if starPx != -1 {
			px = starPx + 1
			starNx++
			nx = starNx
		} else {
			return false
		}
	}

	for px < len(pat) && pat[px] == '*' {
		px++
	}

	return px == len(pat)
}

func yaToolsConfResourceFiles(fs FS, confDir string) [][]string {
	confDir = strings.TrimRight(confDir, "/")

	confFile := confDir + "/" + yaConfJSONName

	if !fs.isFile(srcRootRel, confFile) {
		throwFmt("YA_TOOLS_CONF: file %s not found", confFile)
	}

	calls := [][]string{{"STRIP", confDir + "/", confFile}}
	formulas := map[string]bool{}

	addFormula := func(formula any) {
		f, ok := formula.(string)

		if ok && (strings.HasPrefix(f, "build/") || strings.HasPrefix(f, confDir+"/")) && fs.isFile(srcRootRel, f) {
			formulas[f] = true
		}
	}

	addBottles := func(conf map[string]any) {
		bottles, _ := conf["bottles"].(map[string]any)

		for _, bottle := range bottles {
			if b, ok := bottle.(map[string]any); ok {
				addFormula(b["formula"])
			}
		}
	}

	conf := readJSONObject(fs, confFile)
	simpleTools, _ := conf["simple_tools"].(map[string]any)

	for name, info := range simpleTools {
		tool, _ := info.(map[string]any)

		if resource, ok := tool["resource"].(string); ok {
			name = resource
		}

		addFormula(fmt.Sprintf(yaToolsFormulaTmpl, name))
	}

	addBottles(conf)

	var resourceFiles []string

	if tiers := confDir + "/tools/internal/tiers.json"; fs.isFile(srcRootRel, tiers) {
		resourceFiles = append(resourceFiles, tiers)
	}

	toolsDir := confDir + "/tools/tools/"

	for _, rel := range ymakeGlob(fs, toolsDir+"**/*"+yaToolSuffix) {
		resourceFiles = append(resourceFiles, rel)

		tool, _ := readJSONObject(fs, rel)["tool"].(map[string]any)

		if tool["type"] != "simple" {
			continue
		}

		definition, _ := tool["definition"].(map[string]any)

		if formula, ok := definition["formula"]; ok {
			addFormula(formula)
		} else {
			addFormula(fmt.Sprintf(yaToolsFormulaTmpl, strings.TrimSuffix(strings.TrimPrefix(rel, toolsDir), yaToolSuffix)))
		}
	}

	for _, rel := range ymakeGlob(fs, confDir+"/tools/toolchains/*"+yaToolchainSuffix) {
		resourceFiles = append(resourceFiles, rel)
		addBottles(readJSONObject(fs, rel))
	}

	if len(resourceFiles) > 0 {
		sort.Strings(resourceFiles)
		calls = append(calls, append([]string{"PREFIX", "yatools", "STRIP", confDir}, resourceFiles...))
	}

	for _, formula := range slices.Sorted(maps.Keys(formulas)) {
		calls = append(calls, []string{formula})
	}

	return calls
}

func readJSONObject(fs FS, rel string) map[string]any {
	var m map[string]any

	throw(json.Unmarshal(concatChunks(fs.read(rel)), &m))

	return m
}
