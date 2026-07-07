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
	cmdArgs := make([]STR, 0, 4+len(bc.Opts))

	cmdArgs = append(cmdArgs,
		toolBin.fullSTR(),
		inputIn.fullSTR(),
		prefixCpp.fullSTR(),
		prefixH.fullSTR(),
	)

	cmdArgs = append(cmdArgs, bc.Opts...)

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}
	bcRef := ctx.emit.reserve()
	var headerParsed []IncludeDirective

	for _, oi := range bc.OutputIncludes {
		headerParsed = append(headerParsed, IncludeDirective{kind: includeQuoted, target: oi})
	}

	reg := e.codegen

	reg.register(&GeneratedFileInfo{
		OutputPath:     prefixH,
		ProducerRef:    bcRef,
		GeneratorRefs:  []NodeRef{toolLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
		ClosureLeaves:  []VFS{prefixCpp, inputIn},
	})

	reg.register(&GeneratedFileInfo{
		OutputPath:     prefixCpp,
		ProducerRef:    bcRef,
		GeneratorRefs:  []NodeRef{toolLDRef},
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: headerParsed},
		ClosureLeaves:  []VFS{inputIn},
	})

	node := Node{
		Platform:       instance.Platform,
		Cmds:           na.cmdList(Cmd{CmdArgs: na.chunkListSTR(cmdArgs), Env: env}),
		Env:            env,
		Inputs:         na.inputList(na.vfsList(toolBin, inputIn)),
		Outputs:        []VFS{prefixCpp, prefixH},
		KV:             &baseCodegenKV,
		Requirements:   Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		ForeignDepRefs: depRefs(toolLDRef),
	}

	ctx.emit.emitReservedNode(node, bcRef)
}
