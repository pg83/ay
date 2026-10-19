package main

import (
	"path"
	"strings"
)

var (
	javaKV           = KV{P: pkJV, PC: pcLightBlue, ShowOut: true}
	uberjarKV        = KV{P: pkUJ}
	javaRequirements = Requirements{CPU: 4, RAM: 32, Network: nwRestricted}
)

func (e *EmitContext) genJava() *ModuleEmitResult {
	var peers []VFS
	var globals []ResourceDecl

	for _, p := range e.d.peerdirs {
		res := genModule(e.ctx, ModuleInstance{Path: source(p.string()), Kind: KindLib, Platform: e.instance.Platform})

		peers = append(peers, res.JavaClosure...)
		globals = append(globals, res.ResourceGlobalClosure...)
	}

	peers = dedup(peers)

	jd := e.d.java
	resources := []string{"build/platform/python/ymake_python3"}

	if e.d.moduleStmt.Name == tokJavaProgram {
		resources = append(resources, "build/platform/java/jdk/jdk"+jd.jdk, "build/platform/java/uberjar/uberjar"+jd.jdk)
	}

	globals = e.scriptModuleResources(globals, resources...)

	module := e.instance.Path.relString()
	name := path.Base(module)

	if parent := path.Base(path.Dir(module)); parent != "." {
		name = parent + "-" + name
	}

	if len(e.d.moduleStmt.Args) > 0 {
		name = e.d.moduleStmt.Args[0].string()
	}

	out := build(module, "/", name, ".jar")

	if e.d.moduleStmt.Name == tokJavaContrib {
		ref := e.emitJavaContrib(out)

		return e.scriptModuleResult(ref, out, peers, globals)
	}

	if !jd.uberjar {
		throwFmt("JAVA_PROGRAM: only UBERJAR programs are modelled")
	}

	var jars []VFS

	for _, p := range peers {
		if strings.HasSuffix(p.relString(), ".jar") {
			jars = append(jars, p)
		}
	}

	ref := e.emitJavaUberjar(out, jars)
	result := e.scriptModuleResult(ref, out, peers, globals)
	runnable := build(module, "/", name, ".run.cp.jar")
	runRef := e.emitJavaRunnable(runnable, out, peers)

	result.LDPath, result.LDRef = ptr(runnable), runRef

	return result
}

func (e *EmitContext) emitJavaContrib(out VFS) NodeRef {
	jd, na := e.d.java, e.ctx.na

	if jd.jarResource == "" || jd.srcResource == "" {
		throwFmt("JAVA_CONTRIB: JAR_RESOURCE and SRC_RESOURCE are required by the current model")
	}

	outputs := []VFS{out, intern(strings.TrimSuffix(out.string(), ".jar") + "-sources.jar")}

	var cmds []Cmd

	for i, id := range []string{jd.jarResource, jd.srcResource} {
		cmd := e.pythonScriptCmd("fetch_from_sandbox.py", "--resource-file", "$(RESOURCE_ROOT)/sbr/"+id+"/resource", "--resource-id", id, "--copy-to", outputs[i].string())

		cmd.Cwd = build(e.instance.Path.relString())
		cmds = append(cmds, cmd)
	}

	node := Node{
		Module: true, Platform: e.instance.Platform, KV: &fromSandboxKV, Requirements: &fromSandboxRequirements,
		Cmds: na.cmdList(cmds...), Env: envVarsVCS, Resources: usesPython3,
		Inputs: na.inputList(na.vfsList(scriptInputs("fetch_from_sandbox.py", "fetch_from.py")...)), Outputs: na.vfsList(outputs...),
	}

	return e.emitLazyModuleNode(node, nil)
}

func (e *EmitContext) emitJavaUberjar(out VFS, peers []VFS) NodeRef {
	na, jd := e.ctx.na, e.d.java
	module := e.instance.Path.relString()
	bindir := build(module).string()
	jdk := "$(B)/resources/JDK" + jd.jdk + "/bin/"
	sources := bindir + "/all-java.srclst"
	vcs := out.string() + ".__vcs_version__.mf"
	env := na.envList(envVarsVCS[0], EnvVar{Name: envLcAll, Value: strEnUsUtf8.any()})
	prepare := e.pythonScriptCmd("prepare_jar_build.py", "--bindir", bindir, "--moddir", source(module).string(), "--java", sources, "--ya-start-command-file", "--ya-end-command-file")

	prepare.Cwd = build(module)

	cpArgs := []string{"--file", bindir + "/bfg.txt", "-m", "--ya-start-command-file"}

	for _, peer := range peers {
		cpArgs = append(cpArgs, peer.relString())
	}

	cpArgs = append(cpArgs, "--ya-end-command-file")

	javac := e.pythonScriptCmd("run_javac.py", "--with-setup-java-tmpdir", "--sources-list", sources, jdk+"javac", "@"+sources, "-classpath", "$(B)/bfg.jar", "-Xpkginfo:always", "-J-XX:ActiveProcessorCount=4", "-proc:none", "-d", bindir+"/cls", "-g", "-encoding", "UTF-8")

	javac.Cwd, javac.Env = bld, env

	vcsCmd := e.pythonScriptCmd("vcs_info.py", "output-java", "$(VCS)/vcs.json", vcs, ".")

	vcsCmd.Cwd = build(module, "/cls")

	pack := Cmd{CmdArgs: na.chunkList(na.anyList(internAnys([]string{jdk + "jar", "cfvm", strings.TrimSuffix(out.string(), ".jar") + ".lib.jar", vcs, "."})...)), Cwd: build(module, "/cls"), Env: env}

	cmds := []Cmd{prepare, e.pythonScriptCmd("writer.py", cpArgs...), e.pythonScriptCmd("make_manifest_from_bf.py", bindir+"/bfg.txt", "$(B)/bfg.jar"), javac, vcsCmd, pack,
		e.pythonScriptCmd("writer.py", "--file", bindir+"/lint-java.srclst", "-m", "--ya-start-command-file", "--ya-end-command-file")}

	uberArgs := []string{jdk + "java", "-cp", "$(B)/resources/UBERJAR" + jd.jdk + "/devtools-java_shader.jar", "ru.yandex.devtools.emigrante.Main", "--out-jar", out.string()}

	for _, peer := range peers {
		uberArgs = append(uberArgs, "--jar", peer.string())
	}

	cmds = append(cmds, Cmd{CmdArgs: na.chunkList(na.anyList(internAnys(uberArgs)...)), Env: envVarsVCS},
		e.pythonScriptCmd("mkdir.py", bindir+"/_empty/META-INF"), e.pythonScriptCmd("touch.py", bindir+"/_empty/META-INF/MANIFEST.MF"),
		e.pythonScriptCmd("vcs_info.py", "output-java", "$(VCS)/vcs.json", vcs, out.string()),
		Cmd{CmdArgs: na.chunkList(na.anyList(internAnys([]string{jdk + "jar", "ufv", out.string(), "META-INF/MANIFEST.MF"})...)), Cwd: build(module, "/_empty"), Env: envVarsVCS},
		Cmd{CmdArgs: na.chunkList(na.anyList(internAnys([]string{jdk + "jar", "ufvm", out.string(), vcs})...)), Env: envVarsVCS})

	inputs := scriptInputs("prepare_jar_build.py", "process_command_files.py", "java_pack_to_file.py", "autotar_gendirs.py", "resolve_java_srcs.py",
		"writer.py", "make_manifest_from_bf.py", "run_javac.py", "build_java_with_error_prone2.py", "setup_java_tmpdir.py", "javac_daemon_client.py", "JavacDaemon.jar", "vcs_info.py", "mkdir.py", "touch.py")

	inputs = append(inputs, peers...)

	return e.emitLazyModuleNode(Node{
		Module: true, Platform: e.instance.Platform, KV: &javaKV, Requirements: &javaRequirements, Env: env,
		Cmds: na.cmdList(cmds...), Inputs: na.inputList(na.vfsList(inputs...)), Outputs: na.vfsList(out, build(module, "/lint-java.srclst")),
		Resources: na.strs.list(strYMakePython3Name, internStr("JDK"+jd.jdk), internStr("UBERJAR"+jd.jdk)),
	}, peers)
}

func (e *EmitContext) emitJavaRunnable(out, jar VFS, peers []VFS) NodeRef {
	na := e.ctx.na
	module := e.instance.Path.relString()
	name := strings.TrimSuffix(baseName(jar.relString()), ".jar")
	dir := build(module, "/", name)
	cp := intern(out.string() + ".cplst")
	outputs := []VFS{out, cp, build(dir.relString(), "/", name, ".jar")}
	copyArgs := []string{"link_or_copy_to_dir", "--ya-start-command-file", jar.string()}

	for _, ext := range []string{".so", ".dll", ".dylib"} {
		for _, peer := range peers {
			if strings.HasSuffix(peer.relString(), ext) {
				copyArgs = append(copyArgs, peer.string())
				outputs = append(outputs, build(dir.relString(), "/", baseName(peer.relString())))
			}
		}
	}

	copyArgs = append(copyArgs, "--ya-end-command-file", dir.string())

	tar := build(module, "/", name, ".tar")

	outputs = append(outputs, tar)

	tarCmd := e.pythonScriptCmd("find_and_tar.py", tar.string())

	tarCmd.Cwd = dir

	cmds := []Cmd{e.pythonScriptCmd("make_java_classpath_file.py", "--from-args", cp.string(), out.string()),
		e.pythonScriptCmd("fs_tools.py", "md", dir.string()), e.pythonScriptCmd("fs_tools.py", "link_or_copy", jar.string(), out.string()),
		e.pythonScriptCmd("fs_tools.py", copyArgs...), tarCmd}

	inputs := append([]VFS{jar}, peers...)

	inputs = append(inputs, scriptInputs("make_java_classpath_file.py", "process_command_files.py", "fs_tools.py", "find_and_tar.py")...)

	return e.emitLazyModuleNode(Node{
		Module: true, Platform: e.instance.Platform, KV: &uberjarKV, Env: envVarsVCS, Resources: usesPython3,
		Cmds: na.cmdList(cmds...), Inputs: na.inputList(na.vfsList(dedup(inputs)...)), Outputs: na.vfsList(outputs...),
	}, nil)
}
