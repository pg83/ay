package main

import (
	"slices"
	"strings"
)

func (e *EmitContext) resourceHash(items []string, tag STR) string {
	items = append(slices.Clone(items), joinRel("$S", cleanRel(e.instance.Path.relString())))

	hash, _ := resourceHashInto(nil, items, tag.string())

	return hash
}

func (e *EmitContext) packYasmResourceChunks(items []ResourceItem, p ResourcePack) ([]NodeRef, []VFS) {
	na := e.ctx.na
	module := e.instance.Path.relString()

	var asmFiles, cppFiles []string

	lo := 0

	for lo < len(items) {
		hi := lo + resourceChunkEnd(items[lo:])

		var compressed []string
		var args, outputs, srcgen []ANY
		var srcHash []string

		chunk := items[lo:hi]
		textInputs := e.resourceTextInputs(chunk)

		var inputs, opaque []ANY

		dedupers.with(func(seen *DeDuper) {
			for _, item := range chunk {
				input := item.input()

				if input == 0 || !seen.add(input.strID()) {
					continue
				}

				if slices.Contains(textInputs, input) {
					opaque = append(opaque, input.any())
				} else {
					inputs = append(inputs, input.any())
				}
			}
		})

		for _, item := range chunk {
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
			ToolPath: strToolsRescompressor.any(), Args: compressArgs, INFiles: append(inputs, opaque...), INNoParse: len(opaque),
			OUTNoAutoFiles: append([]ANY{internStr(asm).any()}, outputs...),
			EnvPairs:       []ANY{strYasmTestSuite1.any()},
		})

		srcgen = append(srcgen, internStr(cpp).any())

		for _, entry := range srcHash {
			srcgen = append(srcgen, internStr(entry).any())
		}

		e.emitRunProgram(&RunProgramStmt{
			ToolPath: strToolsRorescompiler.any(), Args: srcgen,
			OUTNoAutoFiles: []ANY{internStr(cpp).any()},
		})

		asmFiles = append(asmFiles, asm)
		cppFiles = append(cppFiles, cpp)
	}

	asmName, cppName := asmFiles[0], cppFiles[0]
	globalAsm := len(asmFiles) > 1
	spec := CompileSpec{ForceCxx: true, CFlags: resourceCxxFlags}
	ccRef := e.ctx.emit.reserve()

	if globalAsm {
		cppName = e.emitResourceJoin(cppFiles, ".auxcpp", p.Tag, false)
		asmName = e.emitResourceJoin(asmFiles, ".yasm", p.Tag, true)

		cppPath := build(module, "/", cppName)
		asmPath := build(module, "/", asmName)
		asmRef := e.ctx.emit.reserve()

		e.enqueueSrc(SrcMeta{Source: cppPath.any(), Global: true, Compile: spec, CompileRef: ccRef})
		e.enqueueSrc(SrcMeta{Source: asmPath.any(), Global: true, CompileRef: asmRef})

		return na.refList(ccRef, asmRef), na.vfsList(e.ccOutputFor(cppPath, spec), e.asmOutputFor(asmPath))
	}

	asmPath := build(module, "/", asmName)

	e.codegen.use(asmPath)

	asmIn := e.ccInputsFor(CompileSpec{})

	asmIn.AddIncl = e.d.asmAddIncl
	asmIn.IncludeView = e.scanner.walkClosure(asmPath, e.d.scanCtx, scanDomainAsm)

	yasmRef, _ := e.ctx.tool(argContribToolsYasm)
	asmRef, asmOut := e.emitASAsm(asmPath, asmIn, yasmRef, false, 0)

	e.collectObj(asmRef, asmOut, SrcMeta{})

	cppPath := build(module, "/", cppName)
	meta := SrcMeta{Source: cppPath.any(), Global: true, Compile: spec, CompileRef: ccRef}

	e.emitOneSource(meta)

	return na.refList(ccRef), na.vfsList(e.ccOutputFor(cppPath, spec))
}

func (e *EmitContext) emitResourceJoin(files []string, suffix string, tag STR, flat bool) string {
	na := e.ctx.na
	name := "join_" + e.resourceHash(files, tag) + suffix
	out := build(e.instance.Path.relString(), "/", name)
	script := buildScriptsGenJoinSrcsPy
	args := []ANY{e.d.tc.Python3, script.any(), out.any(), argYaStartCommandFile.any()}

	if flat {
		script = copyFsToolsVFS
		args = []ANY{e.d.tc.Python3, script.any(), strCat.any(), out.any(), argYaStartCommandFile.any()}
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

	ref := e.emitNode(NodeSpec{Node: Node{Platform: e.instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(args...)), Env: envVarsVCS}),
		Env:  envVarsVCS, Inputs: na.rootInputList(na.vfsList(inputs...), e.ctx.scripts[script.rel()]), Outputs: na.vfsList(out), KV: &jsNodeKV, Resources: usesPython3},
	})

	e.register(GeneratedFileSpec{OutputPath: out, ProducerRef: ref,
		ParsedIncludes: ParsedIncludeSet{parsedIncludesLocal: na.dirList(includes...)}})

	return name
}
