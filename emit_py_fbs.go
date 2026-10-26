package main

import (
	"crypto/md5"
	"encoding/base32"
	"slices"
	"strings"
)

var (
	pyFbsKV    = KV{P: pkFP, PC: pcLightGreen}
	pyPysrcKV  = KV{P: pkP3, PC: pcLightGreen}
	py2PysrcKV = KV{P: pkP2, PC: pcLightGreen}
)

func pyFbsListID(files []PyFbsSource) string {
	items := slices.Clone(files)

	slices.SortFunc(items, func(a, b PyFbsSource) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}

		return strings.Compare(a.Module, b.Module)
	})

	pairs := make([]string, len(items))

	for i, f := range items {
		pairs[i] = "(" + pyFbsRepr(f.Path) + ", " + pyFbsRepr(f.Module) + ")"
	}

	sum := md5.Sum([]byte("[" + strings.Join(pairs, ", ") + "]"))

	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}

func pyFbsRepr(s string) string {
	quote := "'"

	if strings.Contains(s, "'") && !strings.Contains(s, `"`) {
		quote = `"`
	}

	s = strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`, quote, `\`+quote).Replace(s)

	return quote + s + quote
}

func (e *EmitContext) emitPyFbsGroups() {
	for _, group := range e.d.pySrcGroups {
		if len(group.Fbs) == 0 {
			continue
		}

		e.emitPyFbsGroup(group.Fbs)
	}
}

func (e *EmitContext) emitPyFbsGroup(files []PyFbsSource) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	dir := instance.Path.relString()
	version, toolPath, kv := tagPy3, argContribLibsFlatbuffersFlatc, &pyPysrcKV

	if d.usePython2 {
		version, toolPath, kv = tagPy2, argContribDeprecatedFlatc, &py2PysrcKV
	}

	name := pyFbsListID(files) + "." + version.string() + ".fbs"
	out := build(dir, "/", name, ".pysrc")

	if e.codegen.lookup(out) != nil {
		return
	}

	toolRef, tool := ctx.tool(toolPath)
	tar := buildScriptsTarSourcesPy
	args := []ANY{tool.any(), strPython.any()}

	if !d.usePython2 {
		args = append(args, strNoWarnings.any(), strPythonTyping.any())
	}

	args = na.anyList(append(args, strGenMutable.any())...)

	tail := na.anyList(argDashO.any(), build(dir).any())
	inputs := []VFS{tool, tar}

	var imports [][]VFS

	for _, f := range files {
		src := resolveSourceOrGeneratedVFS(ctx, instance, f.Path, d.srcDirs)

		tail = append(tail, src.any())
		inputs = append(inputs, src)

		view := e.scanner.walkClosure(src, d.scanCtx, scanDomainFlatc)

		imports = append(imports, view.bucketList()...)
	}

	node := Node{
		Platform: instance.Platform, Env: envVarsVCS,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(args, d.flatcFlags, flatcIOLeadArgs[:len(flatcIOLeadArgs)-1], e.peers.FlatcIncludeArgs, tail), Cwd: bldRootDirVFS, Env: envVarsVCS},
			Cmd{CmdArgs: na.chunkList(na.anyList(d.tc.Python3.any(), tar.any(), strExts.any(), strPy.any(), strInput.any(), build(dir).any(), strOutput.any(), out.any())), Env: envVarsVCS},
		),
		Inputs: NodeInputs{files: na.inputList(inputs, ctx.scripts[tar.rel()])}, Outputs: na.vfsList(out), ForeignDepRefs: na.refList(toolRef), KV: &pyFbsKV, KVExts: []KVExt{{Key: "tared_kind", Val: "nodir"}}, Resources: usesPython3,
	}

	node.Inputs.files = append(node.Inputs.files, imports...)
	node.Inputs.roots = na.inputList(node.Inputs.files[0][1:], node.Inputs.files[1:]...)

	fp := e.emitNode(node)

	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: fp})

	cc := build(dir, "/", name, ".", version.string(), "_raw.cpp")
	script := buildScriptsCompilePysrcPy
	resRef, res := ctx.tool(argToolsRescompiler)
	pycc := e.pyccTool()
	compileArgs := []ANY{d.tc.Python3.any(), script.any(), strInput.any(), out.any(), strOutput.any(), cc.any(), strRescompiler.any(), res.any(), version.any(), strPycc.any(), pycc.binaries[0].any()}

	if !d.usePython2 {
		compileArgs = append(compileArgs, strSlowPycc.any(), pycc.binaries[1].any())
	}

	toolRefs := na.refList(append([]NodeRef{resRef}, pycc.refs...)...)

	p3 := e.emitNode(Node{
		Platform: instance.Platform, Env: envVarsVCS,
		Cmds:   na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(compileArgs...)), Cwd: build(dir), Env: envVarsVCS}),
		Inputs: NodeInputs{files: na.inputList(na.vfsList(script, out, res), pycc.binaries, ctx.scripts[script.rel()]), roots: na.inputList(na.vfsList(script, out), ctx.scripts[script.rel()])}, Outputs: na.vfsList(cc), DepRefs: na.refList(fp), ForeignDepRefs: toolRefs, KV: kv, Resources: usesPython3,
	})

	e.register(GeneratedFileInfo{OutputPath: cc, ProducerRef: p3, GeneratorRefs: toolRefs})
	e.enqueueSrc(SrcMeta{Source: cc.any(), Prio: stmtPrioDefault, Global: true})
}
