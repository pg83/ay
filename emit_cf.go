package main

import (
	"regexp"
	"sort"
	"strings"
)

var (
	cfgVarRefRe      = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)
	cfgCmakeDefineRe = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)
)

func emitExplicitCF(ctx *GenCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *ModuleData) {
	in := ModuleCCInputs{
		TC:          d.tc,
		SetVars:     d.setVars,
		DefaultVars: d.defaultVars,
		ScanCfg:     newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()),
	}
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path.rel(), cf.Src)
	outVFS := copyFileOutputVFS(instance.Path.rel(), cf.Dst)

	emitConfigureFile(ctx, instance, d, srcVFS, outVFS, in)
}

func emitLibraryHInSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	outVFS := build(instance.Path.rel() + "/" + strings.TrimSuffix(srcRel, ".in"))

	emitConfigureFile(ctx, instance, d, srcVFS, outVFS, in)

	return nil
}

func emitLibraryCInSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	outVFS := build(instance.Path.rel() + "/" + strings.TrimSuffix(srcRel, ".in"))

	cfRef := emitConfigureFile(ctx, instance, d, srcVFS, outVFS, in)

	in.IncludeInputs = walkClosure(ctx.scannerFor(instance), outVFS, in.ScanCfg)
	in.ExtraDepRefs = resolveCodegenDepRefsIncl(ctx, instance, ctx.na, in.IncludeInputs, cfRef)
	ccRef, ccOut, _ := emitCC(instance, outVFS.str(), outVFS, in, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

func emitConfigureFile(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcVFS, outVFS VFS, in ModuleCCInputs) NodeRef {
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmdArgs := []STR{in.TC.Python3, configureFilePyVFS.str(), srcVFS.str(), outVFS.str()}
	cmdArgs = appendInternStrs(cmdArgs, buildCFGVars(ctx.fs, srcVFS.rel(), in.SetVars, in.DefaultVars, instance.Platform.BuildTypeUpperSTR.string()))

	cfRef := ctx.emit.emit(&Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:          env,
		Inputs:       na.inputList(na.vfsList(configureFilePyVFS), walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)),
		KV:           &cfKV,
		Outputs:      na.vfsList(outVFS),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    usesPython3,
	})

	registerBoundGeneratedParsedOutputWithSource(ctx, instance, pkCF, outVFS, srcVFS, cfTemplateParsedIncludes(ctx.parsers, srcVFS.rel()), cfRef, nil)

	reg := codegenRegForInstance(ctx, instance)
	reg.addClosureLeaf(outVFS, srcVFS)
	reg.addClosureLeaf(outVFS, configureFilePyVFS)

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
			vars = append(vars, name+"="+cfgVarValue(setVars[k].string()))
		case mapHas(defaultVars, k):
			vars = append(vars, name+"="+cfgVarValue(defaultVars[k].string()))
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

func cfgVarValue(v string) string {
	if len(v) >= 4 && strings.HasPrefix(v, `\"`) && strings.HasSuffix(v, `\"`) {
		return v[2 : len(v)-2]
	}

	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		return v[1 : len(v)-1]
	}

	return v
}

func cfTemplateParsedIncludes(pm *IncludeParserManager, rel string) []IncludeDirective {
	return pm.sourceParsedBuckets(source(rel), nil).bucket(parsedIncludesLocal)
}

func cfModuleTag(d *ModuleData, instance ModuleInstance) STR {
	if d.moduleStmt.Name == tokProtoLibrary && instance.Language != LangPy {
		return tagCppProto
	}

	return 0
}

var (
	cfKV = KV{P: pkCF, PC: pcYellow}
)
