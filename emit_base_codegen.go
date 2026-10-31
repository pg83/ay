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
	toolRes := e.toolResult(internArg(filepath.Clean(bc.ToolPath.string())))
	toolLDRef := toolRes.LDRef
	toolBin := *toolRes.LDPath
	inputIn := sourceJoinClean(moduleDir, prefix+".in")

	if strings.ContainsRune(prefix, '/') {
		inputIn = source(prefix, ".in")
	}

	base := filepath.Base(prefix)
	prefixCpp := copyFileOutputVFS(moduleDir, base+".cpp")
	prefixH := copyFileOutputVFS(moduleDir, base+".h")
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

	pe := func() {
		node := NodeSpec{Node: Node{Platform: instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs), Env: env}),
			Env:  env, Inputs: na.inputList(na.vfsList(toolBin), na.vfsList(inputIn)), Outputs: na.vfsList(prefixCpp, prefixH),
			KV:             &baseCodegenKV,
			ForeignDepRefs: na.refList(toolLDRef)}, input: InputSpec{roots: na.inputList(na.vfsList(inputIn))},
		}

		e.emitReservedNode(node, bcRef)
	}

	pending := e.pendingEmit(pe)

	e.register(GeneratedFileSpec{
		OutputPath:      prefixH,
		ProducerRef:     bcRef,
		ProducerMainOut: prefixCpp,
		GeneratorRefs:   e.ctx.na.refList(toolLDRef),
		ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: headerParsed},

		OnUse: pending,
	})

	e.register(GeneratedFileSpec{
		OutputPath:      prefixCpp,
		ProducerRef:     bcRef,
		ProducerMainOut: prefixCpp,
		GeneratorRefs:   e.ctx.na.refList(toolLDRef),
		ParsedIncludes:  ParsedIncludeSet{parsedIncludesLocal: headerParsed},

		OnUse: pending,
	})
}
