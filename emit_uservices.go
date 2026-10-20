package main

import (
	"path"
	"slices"
	"strings"
)

type UsrvFile struct {
	Tar      string
	File     string
	DepsFile string
	Auto     bool
}

func (d *ModuleData) collectUsrvFiles(args []string) {
	tar, deps := "", "NO_DEPS"

	var files, noauto []string

	dst := &files

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "FROM", "DEPS_FILE":
			key := args[i]

			i++

			if i == len(args) {
				throwFmt("USRV_BUILD: missing %s argument", key)
			}

			if key == "FROM" {
				tar = args[i]
			} else {
				deps = args[i]
			}
		case "OUT_NOAUTO":
			dst = &noauto
		default:
			*dst = append(*dst, args[i])
		}
	}

	if tar == "" {
		throwFmt("USRV_BUILD: FROM is required")
	}

	for _, f := range files {
		d.usrvFiles = append(d.usrvFiles, UsrvFile{Tar: tar, File: f, DepsFile: deps, Auto: deps != "NO_DEPS" || strings.HasSuffix(f, ".cpp")})
	}

	for _, f := range noauto {
		d.usrvFiles = append(d.usrvFiles, UsrvFile{Tar: tar, File: f, DepsFile: deps})
	}

	d.peerdirs = append(d.peerdirs, strTaxiUservicesArcGenUnpackTarUsrvBuildLib.any())
}

func (e *EmitContext) emitUsrvFiles() {
	if len(e.d.usrvFiles) == 0 {
		return
	}

	na := e.ctx.na
	toolArgs := resolveResourceGlobalArgs([]ANY{strUsrvUnpackTarResourceGlobalUnpackTar.any()}, e.peers.ResourceGlobals)

	for _, file := range e.d.usrvFiles {
		src := e.runProgramInputVFS(file.Tar)
		out := copyFileOutputVFS(e.instance.Path.relString(), file.File)

		var parsed []IncludeDirective

		if file.DepsFile != "NO_DEPS" {
			depFile := copyFileInputVFS(e.ctx.fs, e.instance.Path, file.DepsFile)
			object := e.ctx.parsers.notsJSON(depFile.relString())
			deps, ok := object[file.File].([]any)

			if !ok {
				throwFmt("USRV_BUILD: dependencies for %s missing in %s", file.File, file.DepsFile)
			}

			for _, dep := range deps {
				parsed = append(parsed, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(dep.(string)).any())})
			}
		}

		inputs := na.vfsList(src)

		if producer := e.codegen.lookup(src); producer != nil && producer.ProducerMainOut != 0 && producer.ProducerMainOut != src {
			inputs = na.vfsList(src, producer.ProducerMainOut)
		}

		n := Node{Platform: e.instance.Platform, KV: &KV{P: pkUT, PC: pcLightCyan, ShowOut: true}, Env: envVarsVCS,
			Cmds:   na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(toolArgs[0], strSourceArg.any(), src.any(), strTarget2.any(), build(e.instance.Path.relString()).any(), out.any())), Env: envVarsVCS}),
			Inputs: na.inputList(inputs), Outputs: na.vfsList(out)}

		e.emitLazyModuleNode(n, nil)

		info := e.codegen.mustInfo(out)

		if strings.HasSuffix(file.File, ".proto") || strings.HasSuffix(file.File, ".fbs") {
			info.ParsedIncludes = ParsedIncludeSet{parsedIncludesLocal: parsed}
		} else {
			info.ParsedIncludes = ParsedIncludeSet{parsedIncludesCpp: parsed, parsedIncludesHeader: parsed}
		}
	}

	for _, file := range e.d.usrvFiles {
		if file.Auto {
			e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(e.instance.Path.relString(), file.File).any(), Prio: stmtPrioDefault})
		}
	}
}

func usrvAllSources(fs FS, module string, args []string) []string {
	var dirs, excludes []string

	recursive, suffix := false, ""
	dst := &dirs

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "RECURSIVE":
			recursive = true
		case "EXCLUDE":
			dst = &excludes
		case "SUFFIX":
			i++

			if i == len(args) {
				throwFmt("USRV_ALL_SRCS: missing SUFFIX")
			}

			suffix = args[i]
		default:
			*dst = append(*dst, args[i])
		}
	}

	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	var files []string

	for _, dir := range dirs {
		pattern := path.Join(module, dir) + "/*"

		if recursive {
			pattern += "*"
		}

		for _, f := range globMatch(fs, pattern) {
			ext := path.Ext(f)

			if !slices.Contains([]string{".c", ".cc", ".cpp", ".h", ".hpp", ".hxx", ".make"}, ext) || !strings.HasSuffix(strings.TrimSuffix(f, ext), suffix) {
				continue
			}

			rel := strings.TrimPrefix(f, module+"/")

			if rel == "ya.make" {
				continue
			}

			excluded := false

			for _, p := range excludes {
				if sourceGlobMatches(p, rel) {
					excluded = true

					break
				}
			}

			if !excluded {
				if path.Base(f) == "ya.make" {
					throwFmt("USRV_ALL_SRCS: nested module %s", f)
				}

				files = append(files, rel)
			}
		}
	}

	slices.Sort(files)

	return slices.Compact(files)
}

func sourceGlobMatches(pattern, file string) bool {
	p := strings.Split(pattern, "/")
	f := strings.Split(file, "/")

	var match func(int, int) bool
	match = func(i, j int) bool {
		if i == len(p) {
			return j == len(f)
		}

		if p[i] == "**" {
			return match(i+1, j) || j < len(f) && match(i, j+1)
		}

		return j < len(f) && globSegMatch(p[i], f[j]) && match(i+1, j+1)
	}

	return match(0, 0)
}
