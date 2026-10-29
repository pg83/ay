package main

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"path"
	"slices"
	"strings"
)

func javaClosureJars(closure javaClosureSeq, ext string) []VFS {
	var out []VFS

	for e := range closure {
		if strings.HasSuffix(e.path.relString(), ext) {
			out = append(out, e.path)
		}
	}

	return out
}

func (jm *JavaModule) runnableOutputs() []VFS {
	d := jm.data
	dir := build(jm.dir, "/", jm.name)

	if d.uberjar {
		outs := []VFS{jm.target, build(jm.dir, "/", jm.name, ".run.cp.jar.cplst"), build(dir.relString(), "/", jm.name, ".jar"), build(jm.dir, "/", jm.name, ".tar")}

		for _, s := range d.generateScripts {
			for _, gs := range parseJavaGenerateScript(s) {
				outs = append(outs, javaOutDirVFS(jm, gs.out))
			}
		}

		if d.withJDK {
			outs = append(outs, build(jm.dir, "/jdk.tar"))
		}

		return dedupVFSKeepOrder(outs)
	}

	outs := []VFS{jm.target}

	if !jm.unit.contribRunnable && !d.disableScriptgen {
		outs = append(outs, build(jm.dir, "/run.sh"))
	}

	outs = append(outs, build(jm.dir, "/", jm.name, ".run.cp.jar.cplst"), build(jm.dir, "/", jm.name, ".tar"))

	for _, s := range d.generateScripts {
		for _, gs := range parseJavaGenerateScript(s) {
			outs = append(outs, javaOutDirVFS(jm, gs.out))
		}
	}

	if d.withJDK {
		outs = append(outs, build(jm.dir, "/jdk.tar"))
	}

	return dedupVFSKeepOrder(outs)
}

type javaGenScript struct {
	out      string
	template string
	props    []string
}

func parseJavaGenerateScript(args []string) []javaGenScript {
	kv := map[string][]string{}
	key := ""

	for _, a := range args {
		if a == "OUT" || a == "TEMPLATE" {
			key = a

			continue
		}

		kv[key] = append(kv[key], a)
	}

	if _, ok := kv["TEMPLATE"]; !ok {
		kv["TEMPLATE"] = []string{"template.tmpl"}
	}

	var ws []string

	for _, item := range []string{"OUT", "TEMPLATE"} {
		for i, w := range kv[item] {
			if w == "CUSTOM_PROPERTY" {
				ws = append(ws, kv[item][i:]...)
				kv[item] = kv[item][:i]

				break
			}
		}
	}

	var custom [][]string

	for _, w := range ws {
		if w == "CUSTOM_PROPERTY" {
			custom = append(custom, nil)

			continue
		}

		custom[len(custom)-1] = append(custom[len(custom)-1], w)
	}

	var props []string

	for _, p := range custom {
		if len(p) > 1 {
			props = append(props, "-B", base64.StdEncoding.EncodeToString([]byte(p[0]+"="+strings.Join(p[1:], " "))))
		}
	}

	templates := kv["TEMPLATE"]

	var out []javaGenScript

	for i, o := range kv["OUT"] {
		out = append(out, javaGenScript{out: o, template: templates[min(i, len(templates)-1)], props: props})
	}

	return out
}

func (b *javaNodeBuilder) javaScriptgenScripts(jm *JavaModule, globals map[string]VFS, closure javaClosureSeq, jdk string) {
	d := jm.data
	flag := "IS_UBERJAR=no"

	if d.uberjar {
		flag = "IS_UBERJAR=yes"
	}

	for _, s := range d.generateScripts {
		for _, gs := range parseJavaGenerateScript(s) {
			out := javaOutDirVFS(jm, gs.out)
			tmpl := b.ctx.javaArgVFS(jm, gs.template)

			args := []ANY{b.resourcePath(globals, "SCRIPTGEN", "/scriptgen"), strJava.any(), b.resourcePath(globals, jdk, "/bin/java"),
				strOutput.any(), out.any(), strTemplate.any(), tmpl.any(),
				strD2.any(), internStr(flag).any(),
				strD2.any(), internV("JAR_NAME=", jm.name, ".jar").any(),
				strD2.any(), b.ctx.javaClasspathArg(closure, ".jar"),
				strD2.any(), internV("PROJECT_DIR=", jm.dir).any(),
				strD2.any(), internV("JAR_BASENAME=", jm.name).any(),
				strD2.any(), internV("MAIN_CLASS=", d.jarMainClass).any(),
				strD2.any(), internV("ENABLE_PREVIEW_VALUE=", javaYesIf(d.enablePreview)).any(),
			}

			args = append(args, internAnys(gs.props)...)

			b.input(tmpl)
			b.cmd(args, 0)
		}
	}

	if d.withJDK {
		jdkTar := build(jm.dir, "/jdk.tar")
		withJDK := b.resourcePath(globals, "WITH_JDK"+d.jdkReal, "")
		args := b.python("build/scripts/tar_directory.py")

		args = append(args, strTarByPy.any(), strExclude.any(), strResourceInfoJson.any(),
			strExclude.any(), strInstalled.any(), strExclude.any(), strLnk.any(),
			jdkTar.any(), withJDK, withJDK)

		b.cmd(args, 0)
	}
}

func javaYesIf(v bool) string {
	if v {
		return "yes"
	}

	return ""
}

func (ctx *GenCtx) javaClasspathArg(closure javaClosureSeq, ext string) ANY {
	buf := append(ctx.javaScratch[:0], "CLASSPATH="...)

	for e := range closure {
		if rel := e.path.relString(); strings.HasSuffix(rel, ext) {
			buf = append(append(buf, "::"...), path.Base(rel)...)
		}
	}

	ctx.javaScratch = buf

	return internBytes(buf).any()
}

func (ctx *GenCtx) javaEmitRunnable(jm *JavaModule) {
	d := jm.data
	globals := ctx.javaGlobals(jm)
	closure := ctx.javaManagedPeersClosure(jm)
	jdk := javaJDKResource(d.jdkReal)
	cpDir := build(jm.dir, "/", jm.name)
	cplst := build(jm.dir, "/", jm.name, ".run.cp.jar.cplst")
	tarball := build(jm.dir, "/", jm.name, ".tar")
	b := ctx.javaBuilder(jm)

	b.outputs = jm.runnableOutputs()

	if d.uberjar {
		self := build(jm.dir, "/", jm.name, ".jar")
		mk := b.python("build/scripts/make_java_classpath_file.py")

		b.script("build/scripts/process_command_files.py")
		b.cmd(append(mk, strFromArgs.any(), cplst.any(), jm.target.any()), 0)
		b.cmd(b.fsTools(strMd.any(), cpDir.any()), 0)
		b.cmd(b.fsTools(strLinkOrCopy.any(), self.any(), jm.target.any()), 0)

		lc, lateOuts := javaLinkToDir(b.fsTools(strLinkOrCopyToDir.any(), strYaStartCommandFile.any(), self.any()), closure, cpDir, ".so", ".dll", ".dylib")

		b.cmd(append(lc, strYaEndCommandFile.any(), cpDir.any()), 0)

		if ref := b.use(self); ref != 0 {
			b.deps = append(b.deps, ref)
		}

		b.cmd(append(b.python("build/scripts/find_and_tar.py"), tarball.any()), cpDir)
		b.javaScriptgenScripts(jm, globals, closure, jdk)
		b.outputs = append(b.outputs, lateOuts...)
		b.setKV(pkUJ, pcNone, false)
		b.closure(closure, ctx.javaManagedLen(jm))
		b.macroOuts(jm)
		b.emitModule(jm)

		return
	}

	b.cmd(b.fsTools(strMd.any(), cpDir.any()), 0)

	lc, lateOuts := javaLinkToDir(b.fsTools(strLinkOrCopyToDir.any(), strYaStartCommandFile.any()), closure, cpDir, ".jar", ".so", ".dll", ".dylib")

	b.cmd(append(lc, strYaEndCommandFile.any(), cpDir.any()), 0)

	if !jm.unit.contribRunnable && !d.disableScriptgen {
		args := []ANY{b.resourcePath(globals, "SCRIPTGEN", "/scriptgen"), strJava.any(), b.resourcePath(globals, jdk, "/bin/java"),
			strOutput.any(), build(jm.dir, "/run.sh").any(),
			strD2.any(), strGenerateDefaultRunnerYes.any(),
			strD2.any(), internV("JAR_NAME=", jm.name, ".jar").any(),
			strD2.any(), ctx.javaClasspathArg(closure, ""),
			strD2.any(), internV("PROJECT_DIR=", jm.name).any(),
			strD2.any(), internV("JAR_BASENAME=", jm.name).any(),
			strD2.any(), internV("MAIN_CLASS=", d.jarMainClass).any(),
			strD2.any(), internV("ENABLE_PREVIEW=", javaYesIf(d.enablePreview)).any(),
		}

		b.cmd(args, 0)
	}

	runBf := build(jm.dir, "/run-bf.txt")
	w := b.python("build/scripts/writer.py")

	b.script("build/scripts/process_command_files.py")

	w = append(w, strFile.any(), runBf.any(), strQ.any(), strM.any(), strYaStartCommandFile.any())

	for _, p := range javaEntryPaths(closure) {
		w = append(w, internV(jm.name, "/", path.Base(p.relString())).any())
	}

	b.cmd(append(w, strYaEndCommandFile.any()), 0)
	b.cmd(append(b.python("build/scripts/make_manifest_from_bf.py"), runBf.any(), jm.target.any()), 0)

	mk := b.python("build/scripts/make_java_classpath_file.py")

	b.script("build/scripts/process_command_files.py")

	mk = append(mk, strFromArgs.any(), cplst.any(), strYaStartCommandFile.any())

	for _, p := range javaClosureJars(closure, ".jar") {
		mk = append(mk, internV(jm.dir, "/", jm.name, "/", path.Base(p.relString())).any())
	}

	b.cmd(append(mk, strYaEndCommandFile.any()), 0)
	b.cmd(append(b.python("build/scripts/find_and_tar.py"), tarball.any()), cpDir)
	b.javaScriptgenScripts(jm, globals, closure, jdk)
	b.outputs = append(b.outputs, lateOuts...)
	b.closure(closure, ctx.javaManagedLen(jm))
	b.macroOuts(jm)
	b.setKV(pkJP, pcLightBlue, true)
	b.emitModule(jm)
}

func javaLinkToDir(lc []ANY, closure javaClosureSeq, cpDir VFS, exts ...string) ([]ANY, []VFS) {
	var lateOuts []VFS

	for _, ext := range exts {
		for _, p := range javaClosureJars(closure, ext) {
			lc = append(lc, p.any())
			lateOuts = append(lateOuts, build(cpDir.relString(), "/", path.Base(p.relString())))
		}
	}

	return lc, lateOuts
}

func javaEntryPaths(closure javaClosureSeq) []VFS {
	var out []VFS

	for e := range closure {
		out = append(out, e.path)
	}

	return out
}

type javaRunProgram struct {
	args      []string
	in        []string
	inNoParse int
	out       []string
	noauto    []string
	inDirs    []string
	outDir    string
	cwd       string
	tool      string
	tools     []string
	hash      string
	gentar    VFS
}

func parseJavaRunProgram(dir string, rp JavaRunProgram) javaRunProgram {
	r := javaRunProgram{}
	mode := ""

	var opaque []string

	for _, a := range rp.args {
		switch a {
		case "IN", "IN_NOPARSE", "IN_DIR", "OUT", "OUT_NOAUTO", "TOOL", "OUT_DIR", "CLASSPATH", "ENV", "CWD", "STDOUT", "STDOUT_NOAUTO":
			mode = a

			continue
		case "ADD_SRCS_TO_CLASSPATH":
			mode = ""

			continue
		}

		switch mode {
		case "IN":
			r.in = append(r.in, a)
		case "IN_NOPARSE":
			opaque = append(opaque, a)
		case "OUT":
			r.out = append(r.out, a)
		case "OUT_NOAUTO":
			r.noauto = append(r.noauto, a)
		case "IN_DIR":
			r.inDirs = append(r.inDirs, a)
		case "OUT_DIR":
			r.outDir = a
		case "TOOL":
			r.tools = append(r.tools, a)
		case "CLASSPATH":
			r.tool = a
		case "CWD":
			r.cwd = a
		case "":
			r.args = append(r.args, a)
		}
	}

	r.in = append(r.in, opaque...)
	r.inNoParse = len(opaque)

	sum := md5.Sum([]byte(strings.Join(rp.raw, " ")))

	r.hash = fmt.Sprintf("%x", sum[:])

	if r.outDir != "" {
		r.gentar = copyFileOutputVFS(dir, r.outDir+"."+r.hash+".gentar")
	}

	return r
}

func javaOutDirVFS(jm *JavaModule, p string) VFS {
	return copyFileOutputVFS(jm.dir, p)
}

func (ctx *GenCtx) javaArgVFS(jm *JavaModule, p string) VFS {
	return ctx.programInputVFS(jm.instance, jm.data.srcDirs, p)
}

func javaProgramArgVFS(dir, p string) VFS {
	if strings.HasPrefix(p, "$(B)") || strings.HasPrefix(p, "$(S)") {
		return cwdVFS(path.Clean(p))
	}

	return source(path.Clean(dir + "/" + p))
}

func (ctx *GenCtx) javaTool(dir string) *JavaModule {
	name := ctx.moduleName(dir)

	for _, u := range javaModuleUnits[name] {
		if u.kind == juRunnable {
			res := genModule(ctx, ModuleInstance{Path: source(dir), Kind: u.sub, Language: LangJava, Platform: ctx.host, Demand: demandSelf})

			return res.java
		}
	}

	return nil
}

func (ctx *GenCtx) javaToolResult(dir string) *ModuleEmitResult {
	jm := ctx.javaTool(dir)

	if jm == nil {
		throwFmt("gen: tool %s is not a Java program", dir)
	}

	res := *jm.result

	res.LDPath = ptr(jm.target)
	res.LDRef = ctx.javaBuilder(jm).use(jm.target)
	res.ResourceGlobalClosure = ctx.javaResourceGlobals(jm)

	return &res
}

func (ctx *GenCtx) javaRegisterRunPrograms(jm *JavaModule) {
	for _, rp := range jm.data.runJavaPrograms {
		r := parseJavaRunProgram(jm.dir, rp)
		outputs := javaRunProgramOutputs(jm.dir, r)

		if len(outputs) == 0 {
			throwFmt("RUN_JAVA_PROGRAM requires at least one output")
		}

		ref := ctx.emit.reserve()
		pending := ctx.javaPendingEmit(func() { ctx.javaEmitRunProgram(jm, r, ref) })

		for _, output := range outputs {
			ctx.javaReg(jm).register(GeneratedFileSpec{
				OutputPath:      output,
				OwnerModule:     jm.path,
				ProducerRef:     ref,
				ProducerMainOut: outputs[0],
				OnUse:           pending,
			})

			if output != r.gentar && jm.acceptsInput(output) {
				jm.macroOuts = javaPushUniq(jm.macroOuts, output)
			}
		}

		if r.gentar != 0 {
			jm.gentars = append(jm.gentars, r.gentar)
		}
	}
}

func (ctx *GenCtx) javaEmitRunProgram(jm *JavaModule, r javaRunProgram, ref NodeRef) {
	scanner := ctx.scannerFor(jm.instance)
	e := &EmitContext{ctx: ctx, instance: jm.instance, d: &ModuleData{}, scanner: scanner, codegen: scanner.codegen}

	ctx.emitJavaRunProgram(jm.dir, jm.instance.Platform, jm.data.jdkReal, ctx.javaGlobals(jm), r, ref, func(name string) VFS { return ctx.javaArgVFS(jm, name) }, e)
}

func (ctx *GenCtx) emitJavaRunProgram(dir string, platform *Platform, jdk string, globals map[string]VFS, r javaRunProgram, ref NodeRef, input func(string) VFS, e *EmitContext) {
	b := &javaNodeBuilder{ctx: ctx, p: platform, inputContext: e}
	bindir := build(dir)
	outDir := r.outDir
	mkdir := b.python("build/scripts/mkdir.py")

	if outDir != "" {
		mkdir = append(mkdir, internStr(outDir).any())
	}

	b.cmd(mkdir, bindir)

	run := b.python("build/scripts/setup_java_tmpdir.py")

	run = append(run, b.python("build/scripts/stdout2stderr.py")...)
	run = append(run, b.python("build/scripts/fix_java_command_file_cp.py")...)

	run = append(run, strBuildRoot2.any(), strB.any(), b.resourcePath(globals, javaJDKResource(jdk), "/bin/java"),
		strDfileEncodingUtf8.any(), strClasspath.any())

	if tool := ctx.javaTool(r.tool); tool != nil {
		cplst := build(tool.dir, "/", tool.name, ".run.cp.jar.cplst")

		run = append(run, internV("@", cplst.string()).any())

		if dep := ctx.javaBuilder(tool).use(cplst); dep != 0 {
			b.input(tool.target)
			b.deps = append(b.deps, dep)
		}
	}

	ins := map[string]bool{}

	for _, in := range r.in {
		ins[in] = true
	}

	tools := resolveRunProgramAuxTools(ctx, r.tools)
	toolTokens := prAuxToolTokens(tools)

	for _, tool := range tools {
		b.input(tool.bin)
		b.deps = append(b.deps, tool.ref)
	}

	for _, a := range r.args {
		switch {
		case (r.output(a) || ins[a]) && javaRootedArg(a):
			run = append(run, intern(a).any())
		case r.output(a):
			run = append(run, copyFileOutputVFS(dir, a).any())
		case ins[a]:
			run = append(run, input(a).any())
		default:
			argument, _ := replaceAuxToolArg(internStr(a).any(), toolTokens)

			run = append(run, argument)
		}
	}

	cwd := bldRootDirVFS

	if r.cwd != "" {
		cwd = javaProgramArgVFS(dir, r.cwd)
	}

	b.cmd(run, cwd)

	tar := b.python("build/scripts/autotar_gendirs.py")

	tar = append(tar, strPack.any())

	if outDir != "" {
		tar = append(tar, internStr(outDir).any())
	}

	tar = append(tar, strOuts.any())

	if r.gentar != 0 {
		tar = append(tar, r.gentar.any())
	}

	b.cmd(tar, bindir)

	for _, in := range r.in {
		file := input(in)

		b.input(file)
		b.inputRoots = append(b.inputRoots, file)
	}

	parsedCount := len(b.inputRoots) - r.inNoParse

	for _, inDir := range r.inDirs {
		pattern := inDir + "/**/*"

		if !strings.HasPrefix(inDir, "$(") {
			pattern = dir + "/" + pattern
			pattern = "$(S)/" + strings.TrimPrefix(pattern, "/")
		}

		for _, f := range ctx.javaGlob(dir, pattern, newJavaExcludeMatcher(dir, nil)) {
			file := source(f)

			b.input(file)
			b.inputRoots = append(b.inputRoots, file)
		}
	}

	b.textInputs = e.textInputSources(b.inputRoots, parsedCount)

	b.setKV(pkRJ, pcBlue, false)
	b.exts = []KVExt{{Key: "tared_kind", Val: "nodir"}}
	b.outputs = javaRunProgramOutputs(dir, r)
	b.emitTo(ref)
}

func (r *javaRunProgram) output(a string) bool {
	return slices.Contains(r.out, a) || slices.Contains(r.noauto, a)
}

func javaRootedArg(a string) bool {
	return strings.HasPrefix(a, "$(S)/") || strings.HasPrefix(a, "$(B)/")
}

func javaRunProgramOutputs(dir string, r javaRunProgram) []VFS {
	var outputs []VFS

	for _, a := range r.args {
		if r.output(a) && !javaRootedArg(a) {
			outputs = append(outputs, copyFileOutputVFS(dir, a))
		}
	}

	if r.gentar != 0 {
		outputs = append(outputs, r.gentar)
	}

	for _, name := range concat(r.noauto, r.out) {
		outputs = append(outputs, copyFileOutputVFS(dir, name))
	}

	return dedupVFSKeepOrder(outputs)
}
