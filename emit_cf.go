package main

import (
	"regexp"
	"sort"
	"strings"
)

var (
	cfgVarRefRe      = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)
	cfgCmakeDefineRe = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)
	cfKV             = KV{P: pkCF, PC: pcYellow}
)

func (e *EmitContext) emitExplicitCF(cf *ConfigureFileStmt) {
	ctx, instance := e.ctx, e.instance
	srcVFS := e.requireProducedInput("CONFIGURE_FILE src", cf.Src, copyFileInputVFS(ctx.fs, instance.Path, cf.Src))
	outVFS := copyFileOutputVFS(instance.Path.rel(), cf.Dst)

	e.emitConfigureFile(srcVFS, outVFS)
}

func (e *EmitContext) emitLibraryHInSource(src STR) {
	_, instance, d := e.ctx, e.instance, e.d
	srcRel := src.string()
	srcVFS := e.resolveModuleSourceVFS(src, d.cc.SrcDirs)
	outVFS := build(instance.Path.rel(), "/", strings.TrimSuffix(srcRel, ".in"))

	e.emitConfigureFile(srcVFS, outVFS)
}

func (e *EmitContext) emitLibraryCInSource(meta SrcMeta) {
	_, instance, d := e.ctx, e.instance, e.d
	srcRel := meta.Source.string()
	srcVFS := e.resolveModuleSourceVFS(meta.Source, d.cc.SrcDirs)
	outVFS := build(instance.Path.rel(), "/", strings.TrimSuffix(srcRel, ".in"))

	e.emitConfigureFile(srcVFS, outVFS)

	meta.Generated = true
	meta.Source = outVFS.str()
	e.enqueueSrc(meta)
}

func (e *EmitContext) emitConfigureFile(srcVFS, outVFS VFS) NodeRef {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	cmdArgs := []STR{d.cc.TC.Python3, configureFilePyVFS.str(), srcVFS.str(), outVFS.str()}

	cmdArgs = appendInternStrs(cmdArgs, buildCFGVars(ctx.fs, srcVFS.rel(), d.cc.SetVars, d.cc.DefaultVars, instance.Platform.BuildTypeUpperSTR.string()))

	cv := walkClosure(e.scanner, srcVFS, d.cc.ScanCfg)

	cfRef := ctx.emit.emitNode(Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(configureFilePyVFS, cv.self), cv.buckets...),
		KV:           &cfKV,
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	})

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     outVFS,
		SourcePath:     srcVFS,
		ProducerRef:    cfRef,
		GeneratorRefs:  nil,
		SourceInputs:   []VFS{srcVFS, configureFilePyVFS},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: cfTemplateParsedIncludes(ctx.parsers, srcVFS.rel())},
		ClosureLeaves:  []VFS{srcVFS, configureFilePyVFS},
	})

	return cfRef
}

func buildCFGVars(fs FS, rel string, setVars, defaultVars map[STR]STR, buildTypeUpper string) []string {
	referenced := map[string]bool{}
	data := fs.read(rel)

	for _, re := range []*regexp.Regexp{cfgVarRefRe, cfgCmakeDefineRe} {
		for _, m := range re.FindAllSubmatch(data, -1) {
			referenced[string(m[1])] = true
		}
	}

	var vars []string

	for name := range referenced {
		k := internStr(name)

		switch {
		case mapHas(setVars, k):
			vars = append(vars, name+"="+trimSurroundingQuotes(setVars[k].string()))
		case mapHas(defaultVars, k):
			vars = append(vars, name+"="+trimSurroundingQuotes(defaultVars[k].string()))
		case name == "BUILD_TYPE":
			vars = append(vars, "BUILD_TYPE="+buildTypeUpper)
		}
	}

	sort.Strings(vars)

	return vars
}

func mapHas(m map[STR]STR, k STR) bool {
	_, ok := m[k]

	return ok
}

func cfTemplateParsedIncludes(pm *IncludeParserManager, rel string) []IncludeDirective {
	return pm.sourceParsedBuckets(source(rel), nil).bucket(parsedIncludesLocal)
}
