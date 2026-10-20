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

func expandResourceFileArgs(args []ANY, env Environment) []string {
	bindir, curdir := env.unbind(envBINDIR), env.unbind(envCURDIR)
	expanded := anyStrs(expandStmtTokens(args, env))

	env.rebind(envBINDIR, bindir)
	env.rebind(envCURDIR, curdir)

	return expanded
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
					next = append(next, d)
				}

				fs.walk(d, func(rel string, isDir bool) bool {
					if rel == d {
						return true
					}

					if isDir {
						if !last {
							next = append(next, rel)
						}
					} else if last {
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
			entries := append([]uint32(nil), view.names...)

			slices.SortFunc(entries, func(a, b uint32) int {
				return strings.Compare(STR(a>>1).string(), STR(b>>1).string())
			})

			for _, packed := range entries {
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
