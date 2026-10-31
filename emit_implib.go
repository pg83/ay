package main

import "path/filepath"

var implibKV = KV{P: pkIL, PC: pcYellow}

type ImplibStmt struct {
	Path       string
	Soname     string
	Callback   string
	FromBundle bool
}

func parseImplib(args []ANY) ImplibStmt {
	if len(args) < 2 {
		throwFmt("GENERATE_IMPLIB requires a library name and path")
	}

	s := ImplibStmt{Path: args[1].string(), Callback: "_"}

	for i := 2; i < len(args); i++ {
		switch args[i].string() {
		case "FROM_BUNDLE":
			s.FromBundle = true
		case "SONAME", "DLOPEN_CALLBACK":
			key := args[i].string()

			i++

			if i == len(args) {
				throwFmt("GENERATE_IMPLIB: %s requires a value", key)
			}

			if key == "SONAME" {
				s.Soname = args[i].string()
			} else {
				s.Callback = args[i].string()
			}
		default:
			throwFmt("GENERATE_IMPLIB: unexpected argument %q", args[i].string())
		}
	}

	return s
}

func implibOutputs(module, path string) []VFS {
	base := filepath.Base(path)

	return []VFS{build(module, "/", base, ".init.c"), build(module, "/", base, ".tramp.S")}
}

func (e *EmitContext) emitImplib(stmt ImplibStmt) {
	na := e.ctx.na
	toolRef, toolPath := e.tool(argContribToolsImplib)
	outs := na.vfsList(implibOutputs(e.instance.Path.relString(), stmt.Path)...)
	args := []ANY{toolPath.any(), strTarget2.any(), internStr(string(e.instance.Platform.ISA)).any(), strDlopenCallback.any(), internStr(stmt.Callback).any(), strOutdir.any(), e.instance.Path.rel().build().any()}

	if stmt.Soname != "" {
		args = append(args, strLibraryLoadName.any(), internStr(stmt.Soname).any())
	}

	var libInput VFS

	if stmt.FromBundle {
		libInput = e.runProgramInputVFS(stmt.Path)
		args = append(args, libInput.any())
	} else {
		args = append(args, internStr(stmt.Path).any())
	}

	value := resourceGlobalArgumentValue(args, e.peers.ResourceGlobals)

	args = value.Args

	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		inputs := na.inputList(na.vfsList(toolPath))

		if libInput != 0 {
			inputs = na.inputList(na.vfsList(toolPath), na.vfsList(libInput))
		}

		e.emitReservedNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
			Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS, Resources: value.Resources}),
			Env:  envVarsVCS, Inputs: inputs, Outputs: outs,
			KV:             &implibKV,
			ForeignDepRefs: na.refList(toolRef)}, input: InputSpec{roots: inputs[1:]},
		}, ref)
	})

	for i, out := range outs {
		var leaves []VFS

		if i > 0 {
			leaves = na.vfsList(outs[0])
		}

		e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref, OnUse: pending, ProducerMainOut: outs[0], ClosureLeaves: leaves})
		e.enqueueSrc(SrcMeta{Source: out.any(), Prio: stmtPrioDefault})
	}
}
