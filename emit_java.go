package main

import (
	"path"
	"strings"
)

var (
	javaReqCPU4       = Requirements{CPU: 4, Network: nwRestricted, RAM: 32}
	javaReqNetworkAll = Requirements{CPU: 1, Network: nwFull, RAM: 32}
)

type javaNodeBuilder struct {
	inputContext *EmitContext
	inputRoots   []VFS
	textInputs   []VFS
	ctx          *GenCtx
	p            *Platform
	cmds         []Cmd
	inputs       []VFS
	modulePeers  []VFS
	outputs      []VFS
	deps         []NodeRef
	resources    []STR
	kv           KV
	exts         []KVExt
	req          *Requirements
	env          EnvVars
}

func (b *javaNodeBuilder) useResource(name string) {
	id := internStr(name)

	for _, r := range b.resources {
		if r == id {
			return
		}
	}

	b.resources = append(b.resources, id)
}

func (b *javaNodeBuilder) cmd(args []ANY, cwd VFS, extra ...EnvVar) {
	env := make(EnvVars, 0, 1+len(extra))

	env = append(env, envVarsVCS...)
	env = append(env, extra...)

	for _, e := range extra {
		found := false

		for i := range b.env {
			if b.env[i].Name == e.Name {
				b.env[i] = e
				found = true
			}
		}

		if !found {
			b.env = append(b.env, e)
		}
	}

	b.cmds = append(b.cmds, Cmd{CmdArgs: b.ctx.na.chunkList(b.ctx.na.anyList(args...)), Cwd: cwd, Env: env})
}

func (b *javaNodeBuilder) input(vs ...VFS) {
	b.inputs = append(b.inputs, vs...)
}

func (b *javaNodeBuilder) script(rel string) VFS {
	v := source(rel)

	b.inputs = append(b.inputs, v)

	return v
}

func (b *javaNodeBuilder) python(script string) []ANY {
	b.useResource("YMAKE_PYTHON3")

	return []ANY{javaPython3.any(), b.script(script).any()}
}

func (b *javaNodeBuilder) fsTools(args ...ANY) []ANY {
	out := b.python("build/scripts/fs_tools.py")

	b.script("build/scripts/process_command_files.py")

	return append(out, args...)
}

func (b *javaNodeBuilder) setKV(p ProcKind, pc PColor, showOut bool) {
	b.kv.P = p
	b.kv.PC = pc
	b.kv.ShowOut = b.kv.ShowOut || showOut
}

func (ctx *GenCtx) javaBuilderReq(jm *JavaModule, req *Requirements) *javaNodeBuilder {
	b := ctx.javaBuilder(jm)

	b.req = req

	return b
}

func (ctx *GenCtx) javaBuilder(jm *JavaModule) *javaNodeBuilder {
	return &javaNodeBuilder{ctx: ctx, p: jm.instance.Platform}
}

func (ctx *GenCtx) javaReg(jm *JavaModule) *CodegenRegistry {
	return ctx.scannerForPlatform(jm.instance.Platform).codegen
}

func (b *javaNodeBuilder) use(out VFS) NodeRef {
	if info := b.ctx.scannerForPlatform(b.p).codegen.use(out); info != nil {
		return info.ProducerRef
	}

	return 0
}

func (b *javaNodeBuilder) emitTo(ref NodeRef) {
	platform := b.p
	na := b.ctx.na
	env := envVarsVCS

	if len(b.env) > 0 {
		env = append(append(EnvVars{}, envVarsVCS...), b.env...)
	}

	kv := new(KV)

	*kv = b.kv

	node := Node{
		Platform:     platform,
		Cmds:         na.cmdList(b.cmds...),
		Env:          env,
		Inputs:       NodeInputs{files: na.inputList(dedupVFSKeepOrder(b.inputs))},
		KV:           kv,
		KVExts:       b.exts,
		Outputs:      na.vfsList(b.outputs...),
		Requirements: b.req,
		DepRefs:      na.refList(dedupRefs(b.deps)...),
		Resources:    b.resources,
	}

	if len(b.modulePeers) > 0 {
		node.Inputs.modulePeers = na.vfsList(dedupVFSKeepOrder(b.modulePeers)...)
		node.Inputs.scanner = b.ctx.scannerForPlatform(platform)
		node.Inputs.na = na
		node.Inputs.deferred = true
	}

	if b.inputContext != nil {
		node.Inputs.roots = na.inputList(b.inputRoots)
		node.Inputs.text = b.textInputs
		b.inputContext.resolveNodeCodegenDeps(&node)
	}

	b.ctx.emit.emitReservedNodePtr(&node, ref)
}

func dedupVFSKeepOrder(vs []VFS) []VFS {
	seen := make(map[VFS]bool, len(vs))
	out := vs[:0:0]

	for _, v := range vs {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}

	return out
}

func (jm *JavaModule) outputs() []VFS {
	d := jm.data

	switch jm.unit.kind {
	case juContrib, juExternal:
		outs := []VFS{jm.target, build(jm.dir, "/", jm.name, "-sources.jar")}

		if jm.unit.kind == juContrib && d.sbomComponent {
			outs = append(outs, build(jm.dir, "/", jm.name, ".component.sbom"))
		}

		return outs
	case juIJar, juTest:
		return []VFS{jm.target}
	case juRunnable:
		return jm.runnableOutputs()
	case juJar:
		outs := []VFS{jm.target}

		if d.withKotlin {
			outs = append(outs, build(jm.dir, "/all-kt-sources.txt"))
		}

		if jm.unit.sub == KindJavaFull && d.abiJar && d.withKotlin && !d.uberjar {
			outs = append(outs, build(jm.dir, "/", jm.name, "-stripped.jar"))
		}

		if d.sourcesJar {
			outs = append(outs, build(jm.dir, "/", jm.name, "-sources.jar"))
		}

		if d.lintLevel != "none" {
			outs = append(outs, build(jm.dir, "/lint-java.srclst"))
		}

		return outs
	}

	return nil
}

func (ctx *GenCtx) javaRegisterOutputs(jm *JavaModule, frame *ModuleFrame) {
	if jm.unit.fbs {
		ctx.javaRegisterFbsSources(jm)
	}

	if jm.unit.kind == juJar {
		ctx.javaRegisterSwigJsrcs(jm)
		ctx.javaRegisterRunPrograms(jm)
	}

	ctx.javaRegisterProducers(jm, frame)
	ctx.javaRegisterFiles(jm)

	outs := jm.outputs()

	if len(outs) == 0 {
		return
	}

	jm.outRef = ctx.emit.reserve()

	pending := ctx.javaPendingEmit(func() { ctx.javaEmit(jm) })
	reg := ctx.javaReg(jm)

	for _, out := range outs {
		reg.register(GeneratedFileInfo{
			OutputPath:      out,
			OwnerModule:     jm.path,
			ProducerRef:     jm.outRef,
			ProducerMainOut: outs[0],
			OnUse:           pending,
		})
	}
}

func (jm *JavaModule) acceptsInput(out VFS) bool {
	ext := path.Ext(out.relString())

	switch jm.unit.kind {
	case juJar:
		if ext != ".jsrc" && ext != ".java" && ext != ".jar" && ext != ".mf" && ext != ".gentar" && ext != ".kt" {
			return false
		}
	case juExternal:
		if ext != ".jsrc" && ext != ".java" && ext != ".jar" && ext != ".mf" {
			return false
		}
	}

	return true
}

func (ctx *GenCtx) javaRegisterFiles(jm *JavaModule) {
	reg := ctx.javaReg(jm)

	for _, file := range jm.data.files {
		dst := copyFileOutputVFS(jm.dir, file)

		if !jm.acceptsInput(dst) {
			continue
		}

		jm.macroOuts = append(jm.macroOuts, dst)

		if reg.lookup(dst) != nil {
			continue
		}

		src := copyFileInputVFS(ctx.fs, jm.path, file)
		ref := ctx.emit.reserve()

		pending := ctx.javaPendingEmit(func() {
			node := composeCPNode(jm.instance, src, dst, nil, ModuleToolchain{Python3: javaPython3}, ctx.scripts, ctx.na)

			ctx.emit.emitReservedNodePtr(&node, ref)
		})

		reg.register(GeneratedFileInfo{
			OutputPath:      dst,
			SourcePath:      src,
			OwnerModule:     jm.path,
			ProducerRef:     ref,
			ProducerMainOut: dst,
			OnUse:           pending,
		})
	}
}

func (ctx *GenCtx) javaRegisterProducers(jm *JavaModule, frame *ModuleFrame) {
	if len(jm.data.fromSandboxes) == 0 && len(jm.data.runPython) == 0 && len(jm.data.copyFiles) == 0 && len(jm.data.runPrograms) == 0 && len(jm.data.bundles) == 0 {
		return
	}

	d := &frame.d

	d.reset()

	d.moduleStmt = &ModuleStmt{Name: jm.stmt}
	d.unit.Type = jm.stmt
	d.fromSandboxes = append(d.fromSandboxes, jm.data.fromSandboxes...)
	d.runPython = append(d.runPython, jm.data.runPython...)
	d.copyFiles = append(d.copyFiles, jm.data.copyFiles...)
	d.runPrograms = append(d.runPrograms, jm.data.runPrograms...)
	d.bundles = append(d.bundles, jm.data.bundles...)
	d.srcDirs = append(d.srcDirs, jm.data.srcDirs...)
	d.tc = ModuleToolchain{Python3: javaPython3}
	d.cc.TC = d.tc
	d.scanCtx = newModuleScanContext(ctx, jm.instance, d, nil, nil, nil, nil)

	frame.peerCtx = PeerContext{ResourceGlobals: ctx.javaResourceGlobals(jm)}

	e := newEmitContextIn(frame, ctx, jm.instance, d, &frame.peerCtx)

	e.emitDeclaredProducers(nil)

	add := func(tokens []ANY) {
		for _, token := range tokens {
			out := copyFileOutputVFS(jm.dir, token.string())

			if !jm.acceptsInput(out) {
				continue
			}

			jm.macroOuts = javaPushUniq(jm.macroOuts, out)
		}
	}

	for _, entry := range d.copyFiles {
		add([]ANY{internAny(entry.Dst)})
	}

	for _, stmt := range d.fromSandboxes {
		add(stmt.OUTFiles)
		add(stmt.OUTNoAutoFiles)
	}

	for _, entry := range d.bundles {
		add([]ANY{internAny(entry.Name)})
	}

	for _, stmt := range d.runPrograms {
		add(stmt.OUTFiles)

		if stmt.StdoutFile != nil && !stmt.StdoutNoAuto {
			add([]ANY{*stmt.StdoutFile})
		}
	}

	for _, stmt := range d.runPython {
		add(stmt.OUTFiles)
		add(stmt.OUTNoAutoFiles)

		if stmt.StdoutFile != nil {
			add([]ANY{*stmt.StdoutFile})
		}
	}
}

func (b *javaNodeBuilder) macroOuts(jm *JavaModule) {
	for _, out := range jm.macroOuts {
		b.input(out)
		b.deps = append(b.deps, b.use(out))
	}
}

func (ctx *GenCtx) javaPendingEmit(fn func()) *PendingEmit {
	p := ctx.na.pending.one()

	*p = PendingEmit{fn: fn, publication: ctx.emit.pendingPublication()}

	return p
}

func (ctx *GenCtx) javaEmit(jm *JavaModule) {
	switch jm.unit.kind {
	case juContrib:
		ctx.javaEmitContrib(jm)
	case juExternal:
		ctx.javaEmitExternal(jm)
	case juJar:
		ctx.javaEmitJar(jm)
	case juIJar:
		ctx.javaEmitIJar(jm)
	case juRunnable:
		ctx.javaEmitRunnable(jm)
	case juTest:
		ctx.javaEmitTest(jm)
	}
}

func (ctx *GenCtx) javaEmitTest(jm *JavaModule) {
	closure := ctx.javaManagedPeersClosure(jm)
	runBf := build(jm.dir, "/run-bf.txt")
	b := ctx.javaBuilder(jm)
	w := b.python("build/scripts/writer.py")

	b.script("build/scripts/process_command_files.py")

	w = append(w, strFile.any(), runBf.any(), strQ.any(), strM.any(), strYaStartCommandFile.any())

	for _, p := range javaClosureJars(closure, ".jar") {
		w = append(w, p.any())
	}

	b.cmd(append(w, strYaEndCommandFile.any()), 0)
	b.cmd(append(b.python("build/scripts/make_manifest_from_bf.py"), runBf.any(), jm.target.any()), 0)
	b.closure(closure)
	b.macroOuts(jm)
	b.setKV(pkJT, pcNone, false)
	b.outputs = []VFS{jm.target}
	b.emitModule(jm)
}

func (ctx *GenCtx) javaResourceGlobals(jm *JavaModule) []ResourceDecl {
	var out []ResourceDecl

	seen := map[STR]bool{}

	add := func(decls []ResourceDecl) {
		for _, decl := range decls {
			if !seen[decl.GlobalVar] {
				seen[decl.GlobalVar] = true
				out = append(out, decl)
			}
		}
	}

	add(jm.globals)

	for _, e := range ctx.javaManagedPeersClosure(jm) {
		if e.java != nil {
			add(e.java.globals)
		} else {
			add(e.foreign.privateResourceGlobals)
		}
	}

	return out
}

func (ctx *GenCtx) javaGlobals(jm *JavaModule) map[string]VFS {
	out := map[string]VFS{}

	for _, decl := range ctx.javaResourceGlobals(jm) {
		out[strings.TrimSuffix(decl.GlobalVar.string(), resourceGlobalSuffix)] = decl.Value
	}

	return out
}

func (b *javaNodeBuilder) resourcePath(globals map[string]VFS, name, suffix string) ANY {
	v, ok := globals[name]

	if !ok {
		return internStr(suffix).any()
	}

	b.useResource(name)

	return internV(v.string(), suffix).any()
}

func javaJDKResource(jdk string) string {
	return "JDK" + jdk
}

func (ctx *GenCtx) javaEmitContrib(jm *JavaModule) {
	d := jm.data
	b := ctx.javaBuilder(jm)
	bindir := build(jm.dir)
	srcJar := build(jm.dir, "/", jm.name, "-sources.jar")
	globals := ctx.javaGlobals(jm)
	jdk := javaJDKResource(d.jdkReal)

	b.outputs = jm.outputs()

	fetch := func(id string, out VFS) {
		args := b.python("build/scripts/fetch_from_sandbox.py")

		b.script("build/scripts/fetch_from.py")

		args = append(args,
			strResourceFile.any(), internV("$(RESOURCE_ROOT)/sbr/", id, "/resource").any(),
			strResourceId.any(), internStr(id).any(),
			strCopyTo.any(), out.any())

		b.cmd(args, bindir)
		b.setKV(pkSB, pcYellow, true)
		b.req = &javaReqNetworkAll
	}

	switch {
	case d.jarResource != "":
		fetch(d.jarResource, jm.target)
	case d.localJar != "":
		local := source(jm.dir, "/", d.localJar)

		b.input(local)
		b.cmd(b.fsTools(strCopy.any(), local.any(), jm.target.any()), 0)
		b.setKV(pkFS, pcYellow, false)
	default:
		mf := build(jm.dir, "/", jm.name, ".jar.__vcs_version__.mf")

		b.cmd(append(b.python("build/scripts/vcs_info.py"), strOutputJava.any(), bldVcsJson.any(), mf.any()), 0)
		b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strCfvm.any(), jm.target.any(), mf.any(), str4.any()}, bindir)
		b.setKV(pkFS, pcYellow, false)
		b.deps = append(b.deps, ctx.vcsRef)
	}

	switch {
	case d.srcResource != "":
		fetch(d.srcResource, srcJar)
	case d.localSourcesJar != "":
		local := source(jm.dir, "/", d.localSourcesJar)

		b.input(local)
		b.cmd(b.fsTools(strCopy.any(), local.any(), srcJar.any()), 0)
		b.setKV(pkFS, pcYellow, false)
	default:
		fake := build(jm.dir, "/fake-src")

		b.cmd(b.fsTools(strMd.any(), fake.any()), 0)
		b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strCfvm2.any(), srcJar.any(), str4.any()}, fake)
		b.setKV(pkFS, pcYellow, false)
	}

	if d.sbomComponent {
		version := d.modver

		if version == "" {
			version = "unknown"
		}

		args := append(b.python(sbomGenScriptRel), strOutput.any(), build(jm.dir, "/", jm.name, ".component.sbom").any(),
			strType.any(), strLibrary.any(), strPath.any(), internStr(jm.dir).any(),
			strVer.any(), internStr(version).any(), strLang.any(), strJava4.any())

		b.cmd(args, 0)
	}

	b.emitModule(jm)
}

func (ctx *GenCtx) javaEmitExternal(jm *JavaModule) {
	d := jm.data
	globals := ctx.javaGlobals(jm)
	closure := ctx.javaManagedPeersClosure(jm)
	srcJar := build(jm.dir, "/", jm.name, "-sources.jar")
	b := ctx.javaBuilderReq(jm, &javaReqCPU4)
	autoInputs := append([]VFS{}, jm.macroOuts...)

	if jm.unit.proto {
		scanCtx := newScanContext(ctx.na, ctx.parsers, nil, nil, nil, includeScannerBasePaths())

		scanCtx.protoInclude = ctx.na.vfsList(jm.protoInclude...)

		protoSources := make([]VFS, 0, len(d.protoSrcs))

		var generated []VFS

		for _, src := range d.protoSrcs {
			input := resolveSourceOrGeneratedVFS(ctx, jm.instance, src, d.srcDirs)

			if extIsGztproto(src) {
				generated = append(generated, ctx.gztProtoSource(jm.instance, input, scanCtx, jm.protoInclude))
			} else {
				protoSources = append(protoSources, input)
			}
		}

		protoSources = append(protoSources, generated...)

		for _, input := range protoSources {
			autoInputs = append(autoInputs, ctx.javaEmitProtoPB(jm, globals, scanCtx, input))
		}

		for _, src := range d.evSrcs {
			autoInputs = append(autoInputs, ctx.javaEmitProtoPB(jm, globals, scanCtx, ctx.javaEmitEvProto(jm, scanCtx, resolveSourceVFS(ctx, jm.instance, src, d.srcDirs))))
		}
	}

	jdk := javaJDKResource(d.jdkReal)
	extJDK := "JDK17"

	if d.hasPerModuleJDK {
		extJDK = jdk
	}

	args := b.python("build/scripts/compile_java.py")

	args = append(args,
		strYaStartCommandFile.any(),
		strJavaBin.any(), b.resourcePath(globals, extJDK, "/bin/java"),
		strJavacBin.any(), b.resourcePath(globals, extJDK, "/bin/javac"),
		strJarBin.any(), b.resourcePath(globals, jdk, "/bin/jar"),
		strKotlinCompiler.any(), b.resourcePath(globals, "KOTLIN_COMPILER", "/kotlin-compiler.jar"),
		strJarOutput.any(), jm.target.any(),
		strSrcsJarOutput.any(), srcJar.any(),
	)

	for _, in := range autoInputs {
		args = append(args, in.any())
	}

	args = append(args, strDelim.any())
	args = append(args, internAnys(d.javacOpts.cur)...)
	args = append(args, strSource.any(), str11.any(), strTarget.any(), str11.any(), strDelim.any())

	for _, e := range closure {
		args = append(args, e.path.any())
	}

	args = append(args, strDelim.any(), strNoStdlib.any(), strModuleName.any(), internStr(jm.name).any())
	args = append(args, b.renderKotlincOpts(globals, d.kotlincOpts.cur)...)
	args = append(args, strJvmTarget.any(), internStr(javaKotlinJVMTarget(d.jdkReal)).any(), strYaEndCommandFile.any())

	b.cmd(args, bldRootDirVFS)
	b.input(autoInputs...)

	for _, s := range []string{"build/scripts/java_command_file.py", "build/scripts/process_command_files.py", "build/scripts/javac_daemon_client.py", "build/scripts/JavacDaemon.jar", "build/scripts/canonicalize_java_abi_jar.py"} {
		b.script(s)
	}

	b.setKV(pkJV, pcLightBlue, true)
	b.outputs = []VFS{jm.target, srcJar}
	b.closure(closure)

	for _, in := range autoInputs {
		b.deps = append(b.deps, b.use(in))
	}

	b.emitModule(jm)
}

func javaKotlinJVMTarget(jdk string) string {
	switch jdk {
	case "8":
		return "1.8"
	case "26":
		return "25"
	}

	return jdk
}

func (b *javaNodeBuilder) closure(closure []JavaClosureEntry) {
	for _, e := range closure {
		ref := NodeRef(0)

		switch {
		case e.java != nil:
			ref = b.use(e.path)
		case e.foreign.LDPath != nil:
			ref = e.foreign.LDRef

			if lazy := b.use(e.path); lazy != 0 {
				ref = lazy
			}
		}

		if ref != 0 {
			b.inputs = append(b.inputs, e.path)
			b.deps = append(b.deps, ref)
		}
	}
}

func (ctx *GenCtx) javaEmitEvProto(jm *JavaModule, scanCtx *ScanContext, ev VFS) VFS {
	rel := ev.relString()
	out := build(jm.dir, "/", strings.TrimSuffix(path.Base(rel), path.Ext(rel)), "_ev.proto")
	b := ctx.javaBuilder(jm)

	b.cmd(b.fsTools(strCopy.any(), ev.any(), out.any()), 0)
	b.input(ev)

	view := ctx.scannerForPlatform(jm.instance.Platform).walkClosure(ev, scanCtx, scanDomainProto)

	for _, bucket := range view.bucketList() {
		b.input(bucket...)
	}

	b.setKV(pkEV, pcYellow, false)
	b.outputs = []VFS{out}

	ref := ctx.emit.reserve()

	ctx.javaReg(jm).register(GeneratedFileInfo{
		OutputPath:      out,
		OwnerModule:     jm.path,
		ProducerRef:     ref,
		ProducerMainOut: out,
		OnUse:           ctx.javaPendingEmit(func() { b.emitTo(ref) }),
	})

	return out
}

func (ctx *GenCtx) javaEmitProtoPB(jm *JavaModule, globals map[string]VFS, scanCtx *ScanContext, protoVFS VFS) VFS {
	d := jm.data
	protoRel := protoVFS.relString()
	out := build(strings.TrimSuffix(protoRel, path.Ext(protoRel)), ".jsrc")
	b := ctx.javaBuilder(jm)
	javaOut := strBJavaOut.any()
	args := b.python("build/scripts/tared_protoc.py")

	args = append(args,
		strTarOutput.any(), out.any(),
		strProtocOutDir.any(), javaOut,
		b.resourcePath(globals, "JAVA_PROTOC", "/bin/protoc"),
		internV("-I=./", d.protoNamespace).any(),
		strIB2.any(), strIS2.any(),
	)

	for _, inc := range jm.protoInclude {
		args = append(args, internV("-I=", inc.string()).any())
	}

	args = append(args, strIS2.any(), strJavaOutBJavaOut.any())

	if d.kotlinProto {
		args = append(args, strKotlinOutBJavaOut.any())
	}

	args = append(args, internAnys(d.protocFlags)...)
	args = append(args, internStr(protoRel).any())

	var extraEnv []EnvVar

	for _, plugin := range d.javaProtoArgs {
		if plugin.kotlinGrpc {
			kgrpc := b.resourcePath(globals, "KOTLIN_GRPC", "/grpc_kotlin")

			extraEnv = append(extraEnv,
				EnvVar{Name: envJavaHome, Value: b.resourcePath(globals, javaJDKResource(d.jdkReal), "")},
				EnvVar{Name: envKotlinGrpcJar, Value: internV(kgrpc.string(), "/protoc-gen-grpc-kotlin-", javaKotlinGrpcVersion, ".jar").any()},
			)

			args = append(args, internV("--plugin=protoc-gen-kotlin_grpc=", kgrpc.string(), "/grpc_kotlin").any(), strKotlinGrpcOutBJavaOut.any())

			continue
		}

		ref, bin := ctx.tool(internArg(plugin.tool))

		b.deps = append(b.deps, ref)
		b.input(bin)
		args = append(args, internV("--plugin=protoc-gen-", plugin.name, "=", bin.string()).any(), internV("--", plugin.name, "_out=$(B)/java_out").any())
	}

	if protoVFS.isBuild() {
		b.cmd(args, bldRootDirVFS, extraEnv...)
	} else {
		b.cmd(args, srcRootDirVFS, extraEnv...)
	}

	b.input(protoVFS)
	b.deps = append(b.deps, b.use(protoVFS))

	view := ctx.scannerForPlatform(jm.instance.Platform).walkClosure(protoVFS, scanCtx, scanDomainProto)

	for _, bucket := range view.bucketList() {
		b.input(bucket...)

		for _, input := range bucket {
			b.deps = append(b.deps, b.use(input))
		}
	}

	b.setKV(pkPB, pcYellow, false)
	b.outputs = []VFS{out}

	ref := ctx.emit.reserve()
	pending := ctx.javaPendingEmit(func() { b.emitTo(ref) })

	ctx.javaReg(jm).register(GeneratedFileInfo{
		OutputPath:      out,
		OwnerModule:     jm.path,
		ProducerRef:     ref,
		ProducerMainOut: out,
		OnUse:           pending,
	})

	return out
}

func (b *javaNodeBuilder) renderKotlincOpts(globals map[string]VFS, opts []string) []ANY {
	out := make([]ANY, 0, len(opts))

	for _, o := range opts {
		if strings.Contains(o, javaKotlinCompilerGlobal) {
			kc := b.resourcePath(globals, "KOTLIN_COMPILER", "")

			o = strings.ReplaceAll(o, javaKotlinCompilerGlobal, kc.string())
		}

		out = append(out, internStr(o).any())
	}

	return out
}
