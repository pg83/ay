package main

import (
	"slices"
	"strings"
)

func (e *EmitContext) resourceHash(items []string, tag STR) string {
	items = append(slices.Clone(items), "$S/"+e.instance.Path.relString())

	hash, _ := resourceHashInto(nil, items, tag.string())

	return hash
}

func (e *EmitContext) packYasmResourceChunks(items []ResourceItem, p ResourcePack) ([]NodeRef, []VFS) {
	na := e.ctx.na
	module := e.instance.Path.relString()

	var asmFiles, cppFiles []string

	lo := 0

	for _, hi := range resourceChunkEnds(items, false) {
		var compressed []string
		var args, inputs, outputs, srcgen []ANY
		var srcHash []string

		for _, item := range items[lo:hi] {
			label := "ro_" + e.resourceHash([]string{item.Path, item.Key}, p.Tag)
			output := label + ".rodata"

			if item.Path == "-" {
				key, value, _ := strings.Cut(item.Key, "=")
				_, rendered, _ := strings.Cut(item.Cmd.string(), "=")

				compressed = append(compressed, "-", value, output)
				args = append(args, str2.any(), internStr(rendered).any(), internStr(output).any())
				srcHash = append(srcHash, label+"="+key)
			} else {
				compressed = append(compressed, item.Path, output)
				args = append(args, item.input().any(), internStr(output).any())
				inputs = append(inputs, item.input().any())
				outputs = append(outputs, internStr(output).any())
				srcHash = append(srcHash, label+"="+item.Key)
			}
		}

		lo = hi

		asm := e.resourceHash(compressed, p.Tag) + ".yasm"
		cpp := e.resourceHash(srcHash, p.Tag) + ".auxcpp"
		compressArgs := []ANY{internStr(asm).any()}

		if e.instance.Platform.OS == OSDarwin || e.instance.Platform.OS == "ios" {
			compressArgs = append(compressArgs, strPrefix.any())
		}

		compressArgs = append(compressArgs, argReplaceBB.any(), argReplaceSS.any(), argReplaceToolRootT.any())
		compressArgs = append(compressArgs, args...)

		e.emitRunProgram(&RunProgramStmt{
			DeclSeq:  e.resourceDeclSeq,
			ToolPath: strToolsRescompressor.any(), Args: compressArgs, INFiles: inputs,
			OUTNoAutoFiles: append([]ANY{internStr(asm).any()}, outputs...),
			EnvPairs:       []ANY{strYasmTestSuite1.any()},
		})

		srcgen = append(srcgen, internStr(cpp).any())

		for _, entry := range srcHash {
			srcgen = append(srcgen, internStr(entry).any())
		}

		e.emitRunProgram(&RunProgramStmt{
			DeclSeq:  e.resourceDeclSeq,
			ToolPath: strToolsRorescompiler.any(), Args: srcgen,
			OUTNoAutoFiles: []ANY{internStr(cpp).any()},
		})

		asmFiles = append(asmFiles, asm)
		cppFiles = append(cppFiles, cpp)
	}

	asmName, cppName := asmFiles[0], cppFiles[0]
	globalAsm := len(asmFiles) > 1

	if globalAsm {
		cppName = e.emitResourceJoin(cppFiles, ".auxcpp", p.Tag, false)
		asmName = e.emitResourceJoin(asmFiles, ".yasm", p.Tag, true)
	}

	asmPath := build(module, "/", asmName)

	e.codegen.use(asmPath)

	asmIn := e.ccInputsFor(CompileSpec{})

	asmIn.AddIncl = e.d.asmAddIncl
	asmIn.IncludeView = e.scanner.walkClosure(asmPath, e.d.scanCtx, scanDomainAsm)

	yasmRef, _ := e.ctx.tool(argContribToolsYasm)
	asmRef, asmOut := e.emitASAsm(asmPath, asmIn, yasmRef, false)
	priority := stmtPrioDefault
	asmMeta := SrcMeta{Global: globalAsm, Prio: priority, Seq: e.resourceDeclSeq}

	if !globalAsm {
		e.collectObj(asmRef, asmOut, asmMeta)
	}

	cppPath := build(module, "/", cppName)
	ccRef := e.ctx.emit.reserve()
	spec := CompileSpec{ForceCxx: true, CFlags: na.anyList(argX.any(), argC.any())}
	meta := SrcMeta{Source: cppPath.any(), Prio: priority, Seq: e.resourceDeclSeq, Compile: spec, CompileRef: ccRef}

	e.enqueueSrc(meta)

	e.resourceMetas = append(e.resourceMetas, meta)

	if globalAsm {
		e.resourceMetas = append(e.resourceMetas, asmMeta)

		return na.refList(ccRef, asmRef), na.vfsList(e.ccOutputFor(cppPath, spec), asmOut)
	}

	return na.refList(ccRef), na.vfsList(e.ccOutputFor(cppPath, spec))
}

func (e *EmitContext) emitResourceJoin(files []string, suffix string, tag STR, flat bool) string {
	na := e.ctx.na
	name := "join_" + e.resourceHash(files, tag) + suffix
	out := build(e.instance.Path.relString(), "/", name)
	script := buildScriptsGenJoinSrcsPy
	args := []ANY{e.d.tc.Python3.any(), script.any(), out.any(), argYaStartCommandFile.any()}

	if flat {
		script = copyFsToolsVFS
		args = []ANY{e.d.tc.Python3.any(), script.any(), strCat.any(), out.any(), argYaStartCommandFile.any()}
	}

	var inputs []VFS
	var includes []IncludeDirective

	for i := len(files) - 1; i >= 0; i-- {
		input := build(e.instance.Path.relString(), "/", files[i])

		e.codegen.use(input)
		inputs = append(inputs, input)
		includes = append(includes, IncludeDirective{kind: includeQuoted, target: includeTarget(input.rel().any())})

		if flat {
			args = append(args, input.any())
		} else {
			args = append(args, input.rel().any())
		}
	}

	args = append(args, argYaEndCommandFile.any())

	ref := e.emitNode(Node{
		Platform: e.instance.Platform,
		Cmds:     na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS}),
		Env:      envVarsVCS, Inputs: na.inputList(na.vfsList(inputs...), e.ctx.scripts[script.rel()]),
		Outputs: na.vfsList(out), KV: &jsNodeKV, Resources: usesPython3,
	})

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: ref,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: na.dirList(includes...)}})

	return name
}
