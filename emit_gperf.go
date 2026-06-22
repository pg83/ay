package main

import (
	"path/filepath"
	"strings"
)

// gperfFlags is the default $GP_FLAGS expanded as three separate argv tokens.
var gperfFlags = []STR{argGpCtTLANSIC.str(), argGpDk.str(), argDashC.str()}

// gperfGeneratedRel is the module-relative path of a gperf-generated source:
// flat in the module build dir as <basename>.cpp, unlike bison/ragel.
func gperfGeneratedRel(srcRel string) string {
	return filepath.Base(srcRel) + ".cpp"
}

// gperfSymbolName wraps the source basename (every extension stripped) as the
// gperf `-Nin_<name>_set` lookup-function symbol.
func gperfSymbolName(srcRel string) string {
	base := filepath.Base(srcRel)

	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	return "-Nin_" + base + "_set"
}

// emitGP emits the GP (gperf) producer node, running gperf with $GP_FLAGS and the
// generated -N symbol, stdout to the generated .cpp. srcInputs is the source-only
// include closure of the .gperf.
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

	// gperf copies the .gperf preamble verbatim into its output, so the tool and
	// the generated cpp read the same closure — one walk serves both nodes.
	srcClosure := walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)

	gpRef := emitGP(instance, srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, keepOnlySourceVFS(srcClosure), ctx.emit)

	// Register the generated cpp so codegen-dep resolution sees the GP producer.
	gpParsed := ctx.scannerFor(instance).parsers.sourceParsedBuckets(srcVFS, nil).bucket(parsedIncludesCpp)
	registerBoundGeneratedParsedOutput(ctx, instance, pkGP, genVFS, gpParsed, gpRef, []NodeRef{gperfLDRef})

	ccSrcRel := strings.TrimPrefix(genVFS.rel(), instance.Path.rel()+"/")
	ccIn := in
	// The compiled file leads IncludeInputs; the .gperf source closure follows.
	ccIn.IncludeInputs = append([]VFS{genVFS}, srcClosure...)
	ccIn.ExtraDepRefs = append([]NodeRef{gpRef}, resolveCodegenDepRefs(ctx, instance, srcClosure, gpRef)...)
	ccRef, ccOut, _ := emitCC(instance, ccSrcRel, genVFS, ccIn, ctx.host, ctx.emit)

	return &SourceEmit{Ref: ccRef, OutPath: ccOut}
}
