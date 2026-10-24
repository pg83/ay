package main

import "path"

var bpfKV = KV{P: pkBP, PC: pcLightGreen}

func (e *EmitContext) bpfCompileNode(stmt LlvmCompileStmt, out VFS) Node {
	na, d, p := e.ctx.na, e.d, e.instance.Platform
	input := e.llvmBcSourceInfo(stmt.Source)
	compiler := resolveResourceGlobalRef("$CLANG18_RESOURCE_GLOBAL", e.peers.ResourceGlobals) + "/bin/clang"
	platformFlags := resolveResourceGlobalArgs(p.CCHead, e.peers.ResourceGlobals)
	windows := "no"

	if p.OS == OSWindows {
		windows = "yes"
	}

	args := na.chunkList(
		na.anyList(d.tc.Python3.any(), buildScriptsClangWrapperPy.any(), internStr(windows).any(), internStr(compiler).any()),
		d.cc.CCBlocks.includes, platformFlags,
		na.anyList(argTarget2.any(), argBpf.any(), argDashC.any(), input.any(), argDashO.any(), out.any()),
		stmt.Options,
	)

	env := envVarsVCS
	commands := na.cmdList(Cmd{CmdArgs: args, Env: env})
	outputs := na.vfsList(out)
	resources := []STR{strYMakePython3Name, internStr(resourcePatternClang18)}

	if p.UsesSDKRoot {
		resources = append(resources, internStr(resourcePatternOSSDKRoot))
	}

	if stmt.Kind == tokBpfStatic {
		object := copyFileOutputVFS(e.instance.Path.relString(), stmt.Source+".bpf.o")
		linker := resolveResourceGlobalRef("$LLD_ROOT_RESOURCE_GLOBAL", e.peers.ResourceGlobals)
		binary := "/bin/ld"

		if p.OS == OSLinux {
			binary = "/bin/ld.lld"
		}

		commands = na.cmdList(commands[0], Cmd{
			CmdArgs: na.chunkList(na.anyList(internV(linker, binary).any(), argR.any(), argB2.any(), argBinary2.any(), argM.any(), argElfX8664.any(), argDashO.any(), object.any(), internStr(path.Base(stmt.Output)).any())),
			Cwd:     build(e.instance.Path.relString()), Env: env,
		})

		outputs = na.vfsList(out, object)
		resources = append(resources, strLLDRootName)
	}

	closure := e.scanner.walkClosure(input, d.scanCtx, scanDomainCC)
	inputs := na.dedupClosureChunks(closure)

	inputs = na.inputList(na.vfsList(buildScriptsClangWrapperPy), inputs...)

	return Node{Platform: p, Cmds: commands, Env: env, Inputs: inputs, Outputs: outputs, KV: &bpfKV, Resources: resources}
}
