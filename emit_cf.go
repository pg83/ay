package main

import (
	"regexp"
	"sort"
	"strings"
)

// A CONFIGURE_FILE / *.in source is a template: configure_file.py substitutes its
// @VAR@ and #cmakedefine[01] references (resolved from SET/DEFAULT vars) to
// produce the output. The three entry points below all walk the template's
// include closure, emit the configure node, and register the output; they differ
// only in how src/dst are named and what happens to the output afterwards.

var (
	cfgVarRefRe      = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)
	cfgCmakeDefineRe = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)
)

const buildTypeDebug = "BUILD_TYPE=DEBUG"

// emitExplicitCF handles a CONFIGURE_FILE(src dst) macro.
func emitExplicitCF(ctx *GenCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *ModuleData) {
	in := ModuleCCInputs{
		TC:          d.tc,
		SetVars:     d.setVars,
		DefaultVars: d.defaultVars,
		ScanCfg:     newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()),
	}
	srcVFS := copyFileInputVFS(ctx.fs, instance.Path.rel(), cf.Src)
	outVFS := copyFileOutputVFS(instance.Path.rel(), cf.Dst)

	emitConfigureFile(ctx, instance, d, srcVFS, outVFS, in, cfIncludeDirectives(ctx.parsers, srcVFS.rel()))
}

// emitLibraryHInSource handles a .h.in source: the configured header is consumed
// via #include, so it carries its own quoted includes; no compile follows.
func emitLibraryHInSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	outVFS := build(instance.Path.rel() + "/" + strings.TrimSuffix(srcRel, ".in"))

	emitConfigureFile(ctx, instance, d, srcVFS, outVFS, in, cfIncludeDirectives(ctx.parsers, srcVFS.rel()))

	return nil
}

// emitLibraryCInSource handles a .cpp.in / .c.in source: configure the template,
// then compile the generated translation unit. The output is compiled (its own CC
// walks it), never #included, so it registers no extra includes.
func emitLibraryCInSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	outVFS := build(instance.Path.rel() + "/" + strings.TrimSuffix(srcRel, ".in"))

	cfRef := emitConfigureFile(ctx, instance, d, srcVFS, outVFS, in, nil)

	in.IncludeInputs = walkClosure(ctx.scannerFor(instance), outVFS, in.ScanCfg)
	in.ExtraDepRefs = append([]NodeRef{cfRef}, resolveCodegenDepRefs(ctx, instance, in.IncludeInputs, cfRef)...)
	ccSrcRel := strings.TrimPrefix(outVFS.rel(), instance.Path.rel()+"/")
	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, outVFS, in, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}

// emitConfigureFile walks the template's include closure, emits the
// configure_file.py node producing outVFS from srcVFS, and registers outVFS as a
// pkCF codegen output. The registered include set is the witness pair (srcVFS,
// configure_file.py) plus parsedExtra — a header template's own quoted includes,
// or nil for a translation unit. The template is recorded as the output's source
// (GeneratedFileInfo.SourcePath) so consumers taking the output as an input pull
// the template + script too. Returns the producer ref.
func emitConfigureFile(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcVFS, outVFS VFS, in ModuleCCInputs, parsedExtra []IncludeDirective) NodeRef {
	na := ctx.emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmdArgs := []STR{in.TC.Python3, configureFilePyVFS.str(), srcVFS.str(), outVFS.str()}
	cmdArgs = appendInternStrs(cmdArgs, buildCFGVars(ctx.fs, srcVFS.rel(), in.SetVars, in.DefaultVars))

	tp := TargetProperties{ModuleDir: instance.Path.rel()}
	if tag := cfModuleTag(d, instance); tag != 0 {
		tp.ModuleTag = tag
	}

	cfRef := ctx.emit.emit(&Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(configureFilePyVFS), walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)),
		KV:               KV{P: pkCF, PC: pcYellow},
		Outputs:          na.vfsList(outVFS),
		TargetProperties: tp,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:        usesPython3,
	})

	parsed := append([]IncludeDirective{
		{kind: includeQuoted, target: internStr(srcVFS.rel())},
		{kind: includeQuoted, target: internStr(configureFilePyVFS.rel())},
	}, parsedExtra...)
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, pkCF, outVFS, srcVFS, parsed, cfRef, nil)

	return cfRef
}

// buildCFGVars scans the template for @VAR@ / #cmakedefine references and emits a
// sorted NAME=value arg for each one resolved through SET/DEFAULT vars (BUILD_TYPE
// falls back to DEBUG). Unreferenced or unresolved names are dropped.
func buildCFGVars(fs FS, rel string, setVars, defaultVars map[STR]STR) []string {
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
			vars = append(vars, buildTypeDebug)
		}
	}

	sort.Strings(vars)

	return vars
}

func mapHas(m map[STR]STR, k STR) bool {
	_, ok := m[k]

	return ok
}

// cfgVarValue strips one outer pair of quotes — the escaped `\"…\"` a SET(K
// "\"raw\"") produces, or a plain `"…"` — so the KEY=VALUE arg holds the bare
// substitution value instead of passing stray quote chars to configure_file.py.
func cfgVarValue(v string) string {
	if len(v) >= 4 && strings.HasPrefix(v, `\"`) && strings.HasSuffix(v, `\"`) {
		return v[2 : len(v)-2]
	}

	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		return v[1 : len(v)-1]
	}

	return v
}

// cfIncludeDirectives is the template's own quoted #includes (sorted), registered
// on a configured header so a consumer's closure walk resolves them.
func cfIncludeDirectives(pm *IncludeParserManager, rel string) []IncludeDirective {
	out := make([]IncludeDirective, 0)

	for _, d := range pm.sourceParsedBuckets(source(rel), nil).bucket(parsedIncludesLocal) {
		if d.kind == includeQuoted {
			out = append(out, d)
		}
	}

	if len(out) == 0 {
		return nil
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.string() < out[j].target.string() })

	return out
}

// cfModuleTag returns the lowercased submodule tag for the CF node's
// TargetProperties: a PROTO_LIBRARY's CPP_PROTO instance surfaces as `cpp_proto`
// in REF dumps; other module types leave module_tag unset.
func cfModuleTag(d *ModuleData, instance ModuleInstance) STR {
	if d.moduleStmt.Name == tokProtoLibrary && instance.Language != LangPy {
		return tagCppProto
	}

	return 0
}
