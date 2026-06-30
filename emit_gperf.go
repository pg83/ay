package main

import (
	"path/filepath"
	"strings"
)

var (
	gperfFlags = []STR{argGpCtTLANSIC.str(), argGpDk.str(), argDashC.str()}
	gperfKV    = KV{P: pkGP, PC: pcYellow}
)

func gperfGeneratedRel(srcRel string) string {
	return filepath.Base(srcRel) + ".cpp"
}

func gperfSymbolName(srcRel string) string {
	base := filepath.Base(srcRel)

	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	return "-Nin_" + base + "_set"
}

func emitGP(instance ModuleInstance, srcRel string, srcVFS, genVFS, gperfBin VFS, gperfLD NodeRef, srcInputs []VFS, emit *StreamingEmitter) NodeRef {
	na := emit.nodeArenas()
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	head := make([]STR, 0, 3+len(gperfFlags))

	head = append(head, (gperfBin).str())
	head = append(head, gperfFlags...)
	head = append(head, internStr(gperfSymbolName(srcRel)), (srcVFS).str())

	node := &Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(head), Env: env, Stdout: (genVFS).str()}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(gperfBin), srcInputs),
		Outputs:        na.vfsList(genVFS),
		KV:             &gperfKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(gperfLD),
	}

	return emit.emit(node)
}

func emitLibraryGperfSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()
	gperfLDRef, gperfBinVFS := ctx.tool(argContribToolsGperf)
	srcVFS := resolveModuleSourceVFS(ctx, instance, d, src, in.SrcDirs)
	genVFS := build(instance.Path.rel(), "/", gperfGeneratedRel(srcRel))
	srcClosure := walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)
	gpRef := emitGP(instance, srcRel, srcVFS, genVFS, gperfBinVFS, gperfLDRef, keepOnlySourceVFS(srcClosure), ctx.emit)

	ctx.codegenFor(instance).register(&GeneratedFileInfo{
		OutputPath:     genVFS,
		ProducerRef:    gpRef,
		GeneratorRefs:  []NodeRef{gperfLDRef},
		ParsedIncludes: []IncludeDirective{{kind: includeQuoted, target: internStr(srcVFS.rel())}},
		Compile:        &CompileSpec{FlatOutput: in.FlatOutput, CFlags: in.PerSourceCFlags},
	})

	return emitOneSource(ctx, instance, d, genVFS.str(), in)
}
