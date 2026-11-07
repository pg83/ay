package main

import (
	"encoding/json"
	"fmt"
	"maps"
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

func expandRootDeferredTokens(args []ANY, env Environment, phase TokenExpansion) []ANY {
	if !slices.ContainsFunc(args, func(arg ANY) bool { return strHasDollar(arg.str().any()) }) {
		return args
	}

	bindir, curdir := env.unbind(envBINDIR), env.unbind(envCURDIR)
	arcadia, build := env.unbind(envARCADIA_ROOT), env.unbind(envARCADIA_BUILD_ROOT)
	expanded := expandTokens(args, env, false, phase)

	env.rebind(envBINDIR, bindir)
	env.rebind(envCURDIR, curdir)
	env.rebind(envARCADIA_ROOT, arcadia)
	env.rebind(envARCADIA_BUILD_ROOT, build)

	return expanded
}

func expandResourcePairs(args []ANY, line int, env Environment) ([]ResourcePair, bool, bool) {
	var expanded []string
	var ids []STR

	for _, arg := range args {
		value := arg.string()

		if strings.ContainsRune(value, '$') {
			value = expandResourceArgument(value, env)

			for _, word := range strings.Split(value, " ") {
				if word != "" {
					expanded = append(expanded, word)
					ids = append(ids, 0)
				}
			}
		} else {
			expanded = append(expanded, value)
			ids = append(ids, arg.str())
		}
	}

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
	restIDs := ids[start:]

	if len(rest)%2 != 0 {
		throwFmt("RESOURCE at line %d: argument count after DONT_PARSE/DONT_COMPRESS strip must be even (got %d)", line, len(rest))
	}

	pairs := make([]ResourcePair, 0, len(rest)/2)

	for i := 0; i < len(rest); i += 2 {
		path := strings.ReplaceAll(rest[i], "$(S)", "${ARCADIA_ROOT}")
		key := strings.ReplaceAll(rest[i+1], "$(S)", "${ARCADIA_ROOT}")

		pair := ResourcePair{
			Path: strings.ReplaceAll(path, "$(B)", "${ARCADIA_BUILD_ROOT}"),
			Key:  strings.ReplaceAll(key, "$(B)", "${ARCADIA_BUILD_ROOT}"),
		}

		if strings.IndexByte(rest[i], '$') < 0 {
			pair.PathID = restIDs[i]
		}

		pairs = append(pairs, pair)
	}

	return pairs, dontParse, dontCompress
}

func expandResourceArgument(value string, env Environment) string {
	return substMacroDeeply(value, nil, env, nil, &substPolicy{emptyUnknown: true, keepModifiers: true, literal: keepDeferredVar})
}

func expandMakeExpression(value string, env Environment, reserved func(string) bool) string {
	return expandMakeExpressionTracked(value, env, reserved, nil)
}

func expandMakeExpressionView(value string, env Environment, reserved func(string) bool, track *SourceTextTracker) string {
	return substMacroDeeplyInto(value, nil, env, nil, &substPolicy{makefile: true, emptyUnknown: true, literal: reserved, track: track}, &makeExprSubstBufs)
}

func expandMakeExpressionTracked(value string, env Environment, reserved func(string) bool, track *SourceTextTracker) string {
	return substMacroDeeply(value, nil, env, nil, &substPolicy{makefile: true, emptyUnknown: true, literal: reserved, track: track})
}

func expandResourceFiles(d *ModuleData, out []ResourceEntry, args []string, ids []ANY) []ResourceEntry {
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

			var pathID STR

			if len(ids) == len(args) {
				pathID = ids[i].str()
			}

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

			out = append(out, ResourceEntry{Path: "-", Key: fileKey, SrcPath: path, SrcPathID: pathID, DontParse: true, DontCompress: dontCompress})
			out = append(out, ResourceEntry{Path: path, PathID: pathID, Key: fileKey, DontParse: true, DontCompress: dontCompress})
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

			globVisit(fs, rel+"/*"+suffix, func(match string, _ bool) {
				if deduper.add(internStr(match).strID()) {
					rfArgs = append(rfArgs, d.resStr2("${ARCADIA_ROOT}/", match))
				}
			})
		}
	})

	return expandResourceFiles(d, out, rfArgs, nil)
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
	var matches []string

	globVisit(fs, pattern, func(file string, view bool) {
		if view {
			file = strings.Clone(file)
		}

		matches = append(matches, file)
	})

	return matches
}

func globVisit(fs FS, pattern string, visit func(file string, view bool)) {
	dirs := []globDir{{id: strEmpty}}

	var joinBuf [256]byte

	for i := 0; i <= len(pattern); {
		segEnd := pathSegmentEnd(pattern, i)
		seg := pattern[i:segEnd]
		last := segEnd == len(pattern)
		wild := strings.ContainsAny(seg, "*?")

		i = segEnd + 1

		var next []globDir

		for _, gd := range dirs {
			d := gd.text

			if seg == "**" {
				if !last {
					root := cleanRel(d)

					if root == d && gd.id != 0 {
						fs.walkDirID(gd.id, func(rel string, id STR, isDir bool) bool {
							if isDir && id != gd.id {
								next = append(next, globDir{rel, id})
							}

							return true
						})
					} else {
						fs.walk(root, func(rel string, isDir bool) bool {
							if isDir && rel != root {
								next = append(next, globDir{text: rel})
							}

							return true
						})
					}

					next = append(next, gd)

					continue
				}

				fs.walk(d, func(rel string, isDir bool) bool {
					if !isDir {
						visit(rel, true)
					}

					return true
				})

				continue
			}

			if !wild {
				child := joinRelInto(joinBuf[:0], d, seg)
				present, isDir := fs.exists(srcRootRel, bytesString(child))

				if !present {
					continue
				}

				if last {
					if !isDir {
						visit(bytesString(child), true)
					}
				} else if isDir {
					id := internBytes(child)

					next = append(next, globDir{id.string(), id})
				}

				continue
			}

			view := fs.listdir(gd.key())

			for _, packed := range view.names {
				name := STR(packed >> 1).string()

				if !globSegMatch(seg, name) {
					continue
				}

				isDir := packed&1 != 0

				if last {
					if !isDir {
						visit(bytesString(joinRelInto(joinBuf[:0], d, name)), true)
					}
				} else if isDir && d == "" {
					next = append(next, globDir{name, STR(packed >> 1)})
				} else if isDir {
					id := internStr(joinRel(d, name))

					next = append(next, globDir{id.string(), id})
				}
			}
		}

		dirs = next
	}
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

		if !ok {
			return
		}

		if !strings.HasPrefix(f, "build/") && !strings.HasPrefix(f, confDir+"/") {
			throwFmt("YA_TOOLS_CONF: formula %s must be in build/ or %s/", f, confDir)
		}

		if !fs.isFile(srcRootRel, f) {
			throwFmt("YA_TOOLS_CONF: formula %s not found", f)
		}

		formulas[f] = true
	}

	addBottles := func(conf map[string]any) {
		bottles, ok := conf["bottles"].(map[string]any)

		if !ok {
			throwFmt("YA_TOOLS_CONF: bottles must be an object")
		}

		for name, bottle := range bottles {
			b, ok := bottle.(map[string]any)

			if !ok {
				throwFmt("YA_TOOLS_CONF: bottle %s must be an object", name)
			}

			formula, ok := b["formula"]

			if !ok {
				throwFmt("YA_TOOLS_CONF: bottle %s has no formula", name)
			}

			addFormula(formula)
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
