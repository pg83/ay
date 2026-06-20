package main

import (
	"path/filepath"
	"strings"
)

// gperfFlags is the default $GP_FLAGS (ymake.core.conf:839 DEFAULT(GP_FLAGS
// -CtTLANSI-C -Dk* -c)) expanded as three separate argv tokens.
var gperfFlags = []STR{argGpCtTLANSIC.str(), argGpDk.str(), argDashC.str()}

// gperfGeneratedRel is the module-relative path of a gperf-generated source.
// Upstream's `${stdout;output;defext=.gperf.cpp;nopath;noext:SRC}` places the
// output flat in the module build dir (nopath) as <basename>.gperf.cpp — unlike
// bison/ragel, which rebase a subdir source under the _/ namespace.
func gperfGeneratedRel(srcRel string) string {
	return filepath.Base(srcRel) + ".cpp"
}

// gperfSymbolName reproduces `${pre=-Nin_;suf=_set;nopath;noallext:SRC}`: the
// source basename with every extension stripped (noallext), wrapped as the gperf
// `-Nin_<name>_set` lookup-function symbol (e.g. tags.gperf → -Nin_tags_set).
func gperfSymbolName(srcRel string) string {
	base := filepath.Base(srcRel)

	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	return "-Nin_" + base + "_set"
}

// emitGP emits the GP (gperf) producer node: it runs contrib/tools/gperf over the
// .gperf source with $GP_FLAGS and the generated -N symbol, redirecting stdout to
// the generated .gperf.cpp. srcInputs is the source-only include closure of the
// .gperf (the tool reads the .gperf and the headers its preamble #includes).
func emitGP(instance ModuleInstance, srcRel string, srcVFS, genVFS, gperfBin VFS, gperfLD NodeRef, srcInputs []VFS, emit Emitter) NodeRef {
	na := emit.nodeArenas()

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	head := make([]STR, 0, 3+len(gperfFlags))
	head = append(head, (gperfBin).str())
	head = append(head, gperfFlags...)
	head = append(head, internStr(gperfSymbolName(srcRel)), (srcVFS).str())

	node := &Node{
		Platform:         instance.Platform,
		Cmds:             na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env, Stdout: (genVFS).str()}),
		Env:              env,
		Inputs:           na.inputList(na.vfsList(gperfBin), srcInputs),
		Outputs:          na.vfsList(genVFS),
		KV:               KV{P: pkGP, PC: pcYellow},
		TargetProperties: TargetProperties{ModuleDir: instance.Path.rel()},
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs:   depRefs(gperfLD),
	}

	return emit.emit(node)
}

func emitLibraryGperfSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	gperfLDRef, gperfBinVFS := ctx.tool(argContribToolsGperf)

	srcVFS := resolveModuleSourceVFS(ctx, instance, d, srcRel, in.SrcDirs)
	genVFS := build(instance.Path.rel() + "/" + gperfGeneratedRel(srcRel))

	// The .gperf is parsed for C includes (CIncludeDirectiveParser, registered in
	// parsers_generated.go). The gperf tool and the generated cpp both read exactly
	// that closure — gperf copies the .gperf preamble verbatim into its output — so
	// one walk of the source serves both nodes (the source leads its own window).
	srcClosure := walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)

	gpRef := emitGP(instance, srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, keepOnlySourceVFS(srcClosure), ctx.emit)

	// Register the generated cpp so codegen-dep resolution (and any sibling that
	// includes it) sees the GP producer. Its parsed includes equal the .gperf's.
	gpParsed := ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesCpp)
	registerBoundGeneratedParsedOutput(ctx, instance, pkGP, genVFS, gpParsed, gpRef, []NodeRef{gperfLDRef})

	ccSrcRel := strings.TrimPrefix(genVFS.rel(), instance.Path.rel()+"/")
	ccIn := in
	// The compiled file leads IncludeInputs (emitCC takes the window verbatim); the
	// .gperf source closure follows — the same headers the generated cpp #includes.
	ccIn.IncludeInputs = append([]VFS{genVFS}, srcClosure...)
	ccIn.ExtraDepRefs = append([]NodeRef{gpRef}, resolveCodegenDepRefs(ctx, instance, srcClosure, gpRef)...)
	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, genVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}
