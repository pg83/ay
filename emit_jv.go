package main

import (
	"path/filepath"
	"strings"
)

var (
	antlr4RuntimeHeaderPath = antlr4RuntimeHeaderVFS.string()
	antlr4JarPath           = antlr4JarVFS.string()
	antlr3JarPath           = antlr3JarVFS.string()
	stdout2stderrPath       = stdout2stderrVFS.string()
	jvKV                    = KV{P: pkJV, PC: pcLightBlue, ShowOut: true}
)

const jdkResourcePath = "$(B)/resources/JDK17/bin/java"

func (e *EmitContext) emitJVDownstreamCPCC(
	cpccPairs []struct{ cpp, h VFS },
	outputIncludes []string,
) {
	ctx, instance := e.ctx, e.instance

	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h
		base := strings.TrimSuffix(filepath.Base(srcCpp.relString()), ".cpp")
		g4CppPath := build(instance.Path.relString(), "/", base, ".g4.cpp")
		cpRef := ctx.emit.reserve()
		emits := ctx.na.dirs.alloc(1 + len(outputIncludes))[:0]

		emits = append(emits, IncludeDirective{kind: includeQuoted, target: includeTarget(srcH.any())})

		for _, h := range outputIncludes {
			emits = append(emits, IncludeDirective{kind: includeQuoted, target: includeTarget(internStr(h).any())})
		}

		ctx.na.dirs.commit(len(emits))

		emits = emits[:len(emits):len(emits)]

		pe := func() {
			e.emitCPWithInputs(srcCpp, g4CppPath, nil, cpRef, e.d.tc, ctx.scripts)
		}

		pending := e.pendingEmit(pe)

		e.register(GeneratedFileSpec{
			OutputPath:     g4CppPath,
			ProducerRef:    cpRef,
			GeneratorRefs:  nil,
			ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: emits},

			OnUse: pending,
		})

		e.enqueueSrc(SrcMeta{
			Source: g4CppPath.any(), Prio: stmtPrioDefault,
		})
	}
}

func (e *EmitContext) emitJVNodeReserved(cmdArgs []ANY, inputs InputChunks, outputs []VFS, cwd string, id NodeRef) {
	na := e.ctx.na
	env := envVarsVCS
	cmd := Cmd{CmdArgs: na.chunkList(na.anyChunkAny(cmdArgs)), Env: env}

	if cwd != "" {
		cmd.Cwd = cwdANY(cwd)
	}

	node := NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(cmd),
		Env:  env, Inputs: inputs, KV: &jvKV,
		Outputs:   na.vfsList(outputs...),
		Resources: usesPython3JDK17},
	}

	e.emitReservedNode(node, id)
}

func (e *EmitContext) emitJVGeneralReserved(
	jarVFS VFS,
	args []string,
	inputSources []VFS,
	inputBuilds []VFS,
	hiddenInputs []VFS,
	outputs []VFS,
	cwd string,
	tc ModuleToolchain,
	id NodeRef,
) {
	na := e.ctx.na
	cmdArgs := make([]ANY, 0, 5+len(args))

	cmdArgs = append(cmdArgs,
		tc.Python3,
		internStr(stdout2stderrPath).any(),
		internStr(jdkResourcePath).any(),
		argJar.any(),
		jarVFS.any(),
	)

	cmdArgs = appendInternAnys(cmdArgs, args)

	jvInputs := na.rootInputList(hiddenInputs, inputSources, inputBuilds, na.vfsList(stdout2stderrVFS), na.vfsList(jarVFS))

	e.emitJVNodeReserved(cmdArgs, jvInputs, outputs, cwd, id)
}
