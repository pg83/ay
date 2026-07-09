package main

import (
	"path/filepath"
	"strings"
)

var baseCodegenKV = KV{P: pkBC, PC: pcYellow}

func (e *EmitContext) emitBaseCodegen(bc *BaseCodegenStmt) {
	ctx, instance := e.ctx, e.instance
	na := ctx.emit.nodeArenas()
	moduleDir := instance.Path.relString()
	prefix := bc.Prefix.string()
	toolRes := ctx.toolResult(internArg(filepath.Clean(bc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath
	inputIn := source(moduleDir, "/", prefix, ".in")

	if strings.ContainsRune(prefix, '/') {
		inputIn = source(prefix, ".in")
	}

	base := filepath.Base(prefix)
	prefixCpp := build(moduleDir, "/", base, ".cpp")
	prefixH := build(moduleDir, "/", base, ".h")
	cmdArgs := na.anys.alloc(4 + len(bc.Opts))[:0]

	cmdArgs = append(cmdArgs,
		toolBin.any(),
		inputIn.any(),
		prefixCpp.any(),
		prefixH.any(),
	)

	cmdArgs = append(cmdArgs, bc.Opts...)
	na.anys.commit(len(cmdArgs))

	cmdArgs = cmdArgs[:len(cmdArgs):len(cmdArgs)]

	env := envVarsVCS
	bcRef := ctx.emit.reserve()

	var headerParsed []IncludeDirective

	for _, oi := range bc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: includeTarget(oi)})
	}

	reg := e.codegen

	hInfo := reg.register(GeneratedFileInfo{
		OutputPath:     prefixH,
		ProducerRef:    bcRef,
		GeneratorRefs:  e.ctx.na.refList(toolLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
		ClosureLeaves:  e.ctx.na.vfsList(prefixCpp, inputIn),
	})

	cppInfo := reg.register(GeneratedFileInfo{
		OutputPath:     prefixCpp,
		ProducerRef:    bcRef,
		GeneratorRefs:  e.ctx.na.refList(toolLDRef),
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
		ClosureLeaves:  e.ctx.na.vfsList(inputIn),
	})

	pe := &PendingEmit{fn: func() {
		node := Node{
			Platform:       instance.Platform,
			Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:            env,
			Inputs:         na.inputList(na.vfsList(toolBin, inputIn)),
			Outputs:        na.vfsList(prefixCpp, prefixH),
			KV:             &baseCodegenKV,
			Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			ForeignDepRefs: na.refList(toolLDRef),
		}

		ctx.emit.emitReservedNode(node, bcRef)
	}}

	hInfo.pending = pe
	cppInfo.pending = pe

}
