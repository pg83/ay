package main

import (
	"crypto/md5"
	"encoding/base32"
	"slices"
	"strings"
)

var pyFbsKV = KV{P: pkFP, PC: pcLightGreen}
var pyPysrcKV = KV{P: pkP3, PC: pcLightGreen}

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
	name := pyFbsListID(files) + ".py3.fbs"
	out := build(dir, "/", name, ".pysrc")
	if e.codegen.lookup(out) != nil {
		return
	}
	toolRef, tool := ctx.tool(argContribLibsFlatbuffersFlatc)
	tar := source("build/scripts/tar_sources.py")
	args := na.anyList(tool.any(), internStr("--python").any(), internStr("--no-warnings").any(), internStr("--python-typing").any(), internStr("--gen-mutable").any())
	tail := na.anyList(argDashO.any(), build(dir).any())
	inputs := []VFS{tool, tar}
	var imports [][]VFS
	for _, f := range files {
		src := resolveSourceVFS(ctx, instance, f.Path, d.srcDirs)
		tail = append(tail, src.any())
		inputs = append(inputs, src)
		imports = append(imports, e.scanner.walkClosure(src, d.scanCtx, scanDomainFlatc).bucketList()...)
	}
	node := Node{
		Platform: instance.Platform, Env: envVarsVCS,
		Cmds: na.cmdList(
			Cmd{CmdArgs: na.chunkList(args, d.flatcFlags, flatcIOLeadArgs[:len(flatcIOLeadArgs)-1], e.peers.FlatcIncludeArgs, tail), Cwd: bldRootDirVFS, Env: envVarsVCS},
			Cmd{CmdArgs: na.chunkList(na.anyList(d.tc.Python3.any(), tar.any(), strExts.any(), internStr(".py").any(), strInput.any(), build(dir).any(), strOutput.any(), out.any())), Env: envVarsVCS},
		),
		Inputs: na.inputList(inputs, ctx.scripts[tar.rel()]), Outputs: na.vfsList(out), ForeignDepRefs: na.refList(toolRef), KV: &pyFbsKV, KVExts: []KVExt{{Key: "tared_kind", Val: "nodir"}}, Resources: usesPython3,
	}
	node.Inputs = append(node.Inputs, imports...)
	fp := e.emitNode(node)
	e.register(GeneratedFileInfo{OutputPath: out, ProducerRef: fp})
	cc := build(dir, "/", name, ".py3_raw.cpp")
	script := source("build/scripts/compile_pysrc.py")
	resRef, res := ctx.tool(argToolsRescompiler)
	pyRef, py := ctx.tool(argToolsPy3cc)
	slowRef, slow := ctx.tool(argToolsPy3ccSlow)
	p3 := e.emitNode(Node{
		Platform: instance.Platform, Env: envVarsVCS,
		Cmds:   na.cmdList(Cmd{CmdArgs: na.chunkList(na.anyList(d.tc.Python3.any(), script.any(), strInput.any(), out.any(), strOutput.any(), cc.any(), internStr("--rescompiler").any(), res.any(), tagPy3.any(), internStr("--pycc").any(), py.any(), internStr("--slow-pycc").any(), slow.any())), Cwd: build(dir), Env: envVarsVCS}),
		Inputs: na.inputList(na.vfsList(script, out, res, py, slow), ctx.scripts[script.rel()]), Outputs: na.vfsList(cc), DepRefs: na.refList(fp), ForeignDepRefs: na.refList(resRef, pyRef, slowRef), KV: &pyPysrcKV, Resources: usesPython3,
	})
	e.register(GeneratedFileInfo{OutputPath: cc, ProducerRef: p3, GeneratorRefs: na.refList(resRef, pyRef, slowRef)})
	e.enqueueSrc(SrcMeta{Source: cc.any(), Prio: stmtPrioDefault, Global: true})
}
