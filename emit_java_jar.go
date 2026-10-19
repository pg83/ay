package main

import (
	"crypto/md5"
	"fmt"
	"path"
	"strings"
)

type javaSrcsGlob struct {
	spec    JavaSrcsSpec
	name    string
	srcDir  string
	absDir  VFS
	files   []VFS
	srcTxt  VFS
	resTxt  VFS
	genSrcs bool
}

func (ctx *GenCtx) javaSrcsGlobs(jm *JavaModule) []javaSrcsGlob {
	var out []javaSrcsGlob

	for _, spec := range jm.data.javaSrcs {
		sum := md5.Sum([]byte(spec.salt))
		name := fmt.Sprintf("uniq_%x", sum[:])

		g := javaSrcsGlob{
			spec:   spec,
			name:   name,
			srcDir: spec.srcDir,
			srcTxt: build(jm.dir, "/misc/", name, ".src.txt"),
			resTxt: build(jm.dir, "/misc/", name, ".res.txt"),
		}

		switch {
		case strings.HasPrefix(spec.srcDir, "$(B)/"):
			g.absDir = build(strings.TrimPrefix(spec.srcDir, "$(B)/"))
			g.genSrcs = true
		case strings.HasPrefix(spec.srcDir, "$(S)/"):
			g.absDir = source(strings.TrimPrefix(spec.srcDir, "$(S)/"))
		default:
			g.absDir = source(jm.dir, "/", spec.srcDir)
		}

		if !g.genSrcs {
			ex := newJavaExcludeMatcher(jm.dir, spec.excludes)

			for _, glob := range spec.globs {
				pattern := path.Clean(spec.srcDir + "/" + glob)

				if strings.HasPrefix(spec.srcDir, "$(S)/") {
					pattern = spec.srcDir + "/" + glob
				}

				for _, f := range ctx.javaGlob(jm.dir, pattern, ex) {
					g.files = append(g.files, source(f))
				}
			}
		}

		out = append(out, g)
	}

	return out
}

func javaClassesDir(base VFS, pkg string) VFS {
	if pkg == "" {
		return base
	}

	return build(base.relString(), "/", strings.ReplaceAll(pkg, ".", "/"))
}

func (ctx *GenCtx) javaEmitJar(jm *JavaModule) {
	d := jm.data
	globals := ctx.javaGlobals(jm)
	closure := ctx.javaManagedPeersClosure(jm)
	b := &javaNodeBuilder{ctx: ctx, req: &javaReqCPU4}
	bindir := build(jm.dir)
	moddir := source(jm.dir)
	jdk := javaJDKResource(d.jdkReal)
	srclst := build(jm.dir, "/all-java.srclst")
	ktSrclist := build(jm.dir, "/misc/all-kt-sources.txt")
	cls := build(jm.dir, "/cls")
	ktCls := build(jm.dir, "/kt_cls")
	mf := build(jm.dir, "/", jm.name, ".jar.__vcs_version__.mf")
	globs := ctx.javaSrcsGlobs(jm)
	lcAll := EnvVar{Name: envLcAll, Value: strEnUsUtf8.any()}
	abi := jm.unit.sub == KindJavaFull && d.abiJar && d.withKotlin && !d.uberjar
	strippedJar := build(jm.dir, "/", jm.name, "-stripped.jar")

	b.outputs = []VFS{jm.target}

	prep := b.python("build/scripts/prepare_jar_build.py")

	if d.sourcesJar {
		prep = append(prep, strWithSourcesJar.any())
	}

	prep = append(prep, strBindir.any(), bindir.any(), strModdir.any(), moddir.any(), strJava.any(), srclst.any())

	if d.withKotlin {
		prep = append(prep, strKotlin.any(), ktSrclist.any())
	}

	prep = append(prep, strYaStartCommandFile.any())

	for _, g := range globs {
		prep = append(prep, strJsources.any(), g.srcTxt.any(), strResources.any(), g.resTxt.any(), strSrcdir.any(), internStr(g.srcDir).any())

		for _, f := range g.files {
			prep = append(prep, f.any())
		}

		for _, f := range g.spec.files {
			prep = append(prep, source(jm.dir, "/", f).any())
		}

		b.input(g.files...)
	}

	for _, kind := range []string{"JAR", "SRC_JAR"} {
		for _, g := range globs {
			if !g.genSrcs || kind == "SRC_JAR" && !d.sourcesJar {
				continue
			}

			res := g.resTxt

			if kind == "SRC_JAR" {
				res = g.srcTxt
			}

			prep = append(prep, strDelim.any(), strAppend.any(), strD.any(), internStr(g.srcDir).any(),
				strS2.any(), srclst.any(), strK.any(), ktSrclist.any(), strR.any(), res.any(),
				strIncludePatterns.any())

			prep = append(prep, internAnys(g.spec.globs)...)

			if kind == "SRC_JAR" {
				prep = append(prep, strAllResources.any())
			}

			if len(g.spec.excludes) > 0 {
				prep = append(prep, strExcludePatterns.any())
				prep = append(prep, internAnys(g.spec.excludes)...)
			}

			if d.withKotlin {
				prep = append(prep, strResolveKotlin.any())
			}
		}
	}

	prep = append(prep, strYaEndCommandFile.any())

	for _, s := range []string{"build/scripts/process_command_files.py", "build/scripts/java_pack_to_file.py", "build/scripts/autotar_gendirs.py", "build/scripts/resolve_java_srcs.py"} {
		b.script(s)
	}

	b.cmd(prep, bindir)

	if d.sourcesJar {
		for _, g := range globs {
			b.cmd(b.fsTools(strCopyFiles.any(), g.absDir.any(), javaClassesDir(build(jm.dir, "/src"), g.spec.packagePrefix).any(), g.srcTxt.any()), 0)
		}
	}

	if d.withKotlin {
		ktBfg := build(jm.dir, "/kt_bfg.txt")
		ktCp := build(jm.dir, "/kt_cp.txt")
		w := b.python("build/scripts/writer.py")

		b.script("build/scripts/process_command_files.py")

		w = append(w, strFile.any(), ktBfg.any(), strM.any(), strYaStartCommandFile.any())

		for _, e := range closure {
			if strings.HasSuffix(e.path.relString(), ".jar") {
				w = append(w, e.path.any())
			}
		}

		w = append(w, strYaEndCommandFile.any())
		b.cmd(w, 0)
		b.cmd(append(b.python("build/scripts/make_java_classpath_file.py"), ktBfg.any(), ktCp.any()), 0)
		b.script("build/scripts/process_command_files.py")
		b.cmd(b.fsTools(strMd.any(), ktCls.any()), 0)

		kt := b.python("build/scripts/run_javac.py")

		for _, s := range []string{"build/scripts/build_java_with_error_prone2.py", "build/scripts/setup_java_tmpdir.py", "build/scripts/javac_daemon_client.py", "build/scripts/JavacDaemon.jar"} {
			b.script(s)
		}

		kt = append(kt, strKotlin.any(), strSourcesList.any(), ktSrclist.any(), b.resourcePath(globals, jdk, "/bin/java"))

		switch d.jdkReal {
		case "11", "17", "21", "22", "23":
		default:
			kt = append(kt, strEnableNativeAccessAllUnnamed.any(), strSunMiscUnsafeMemoryAccessAllow.any())
		}

		kt = append(kt, strJar.any(), b.resourcePath(globals, "KOTLIN_COMPILER", "/kotlin-compiler.jar"),
			strNoStdlib.any(), strModuleName.any(), internStr(jm.name).any(),
			internV("@", ktSrclist.string()).any(), strClasspath.any(), internV("@", ktCp.string()).any(),
			strJvmDefaultNoCompatibility.any(), strXconsistentDataClassCopyVisibility.any(),
			strD.any(), ktCls.any())

		kt = append(kt, b.renderKotlincOpts(globals, d.kotlincOpts.cur)...)
		kt = append(kt, strJvmTarget.any(), internStr(javaKotlinJVMTarget(d.jdkReal)).any())
		b.cmd(kt, bldRootDirVFS, lcAll)

		allKt := build(jm.dir, "/all-kt-sources.txt")

		b.cmd(append(b.python("build/scripts/kt_copy.py"), ktSrclist.any(), allKt.any(), strS.any(), strB.any()), 0)
		b.outputs = append(b.outputs, allKt)
	}

	bfg := build(jm.dir, "/bfg.txt")
	w := b.python("build/scripts/writer.py")

	b.script("build/scripts/process_command_files.py")

	w = append(w, strFile.any(), bfg.any(), strM.any(), strYaStartCommandFile.any())

	for _, e := range closure {
		w = append(w, e.path.rel().any())
	}

	if d.withKotlin {
		w = append(w, internV(jm.dir, "/kt_cls").any())
	}

	w = append(w, strYaEndCommandFile.any())
	b.cmd(w, 0)
	b.cmd(append(b.python("build/scripts/make_manifest_from_bf.py"), bfg.any(), bldBfgJar.any()), 0)

	javac := b.python("build/scripts/run_javac.py")

	for _, s := range []string{"build/scripts/build_java_with_error_prone2.py", "build/scripts/setup_java_tmpdir.py", "build/scripts/javac_daemon_client.py", "build/scripts/JavacDaemon.jar"} {
		b.script(s)
	}

	javac = append(javac, strWithSetupJavaTmpdir.any(), strSourcesList.any(), srclst.any())

	if d.errorProne {
		javac = append(javac, strErrorProne.any(), b.resourcePath(globals, javaErrorProne(d.jdkReal).resource, "/error_prone.jar"))
	}

	javac = append(javac, b.resourcePath(globals, jdk, "/bin/javac"), internV("@", srclst.string()).any(), strClasspath.any(), bldBfgJar.any(), strXpkginfoAlways.any())
	javac = append(javac, internAnys(d.javacOpts.cur)...)

	if len(d.annotationProcs) > 0 {
		javac = append(javac, strProcessor.any(), internStr(strings.Join(d.annotationProcs, ",")).any())
	} else {
		javac = append(javac, strProcNone.any())
	}

	javac = append(javac, strD.any(), cls.any(), strG.any(), strEncoding.any(), strUtf8.any())
	b.cmd(javac, bldRootDirVFS, lcAll)

	if abi {
		abiDir := build(jm.dir, "/abi")

		b.cmd(b.fsTools(strMd.any(), abiDir.any()), 0)
		b.cmd(append(b.python("build/conf/copy_kotlin_inline_sam_classes.py"), ktCls.any(), abiDir.any()), 0)
		b.cmd(b.fsTools(strLinkOrCopyAllFiles.any(), cls.any(), abiDir.any()), 0)
		b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strCfm.any(), strippedJar.any(), str4.any()}, abiDir, lcAll)
		b.cmd(append(b.python("build/scripts/canonicalize_java_abi_jar.py"), strippedJar.any()), 0)
		b.outputs = append(b.outputs, strippedJar)
	}

	if d.withKotlin {
		b.cmd(b.fsTools(strCopyAllFiles.any(), ktCls.any(), cls.any()), 0)
	}

	for _, g := range globs {
		b.cmd(b.fsTools(strCopyFiles.any(), g.absDir.any(), javaClassesDir(cls, g.spec.packagePrefix).any(), g.resTxt.any()), 0)
	}

	b.cmd(append(b.python("build/scripts/vcs_info.py"), strOutputJava.any(), bldVcsJson.any(), mf.any(), str4.any()), cls)
	b.deps = append(b.deps, ctx.vcsRef)

	if d.sourcesJar {
		srcJar := build(jm.dir, "/", jm.name, "-sources.jar")

		b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strCfvm.any(), srcJar.any(), mf.any(), str4.any()}, build(jm.dir, "/src"))
		b.outputs = append(b.outputs, srcJar)
	}

	b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strCfvm.any(), jm.target.any(), mf.any(), str4.any()}, cls, lcAll)

	if d.lintLevel != "none" {
		lint := build(jm.dir, "/lint-java.srclst")
		w := b.python("build/scripts/writer.py")

		b.script("build/scripts/process_command_files.py")

		w = append(w, strFile.any(), lint.any(), strM.any(), strYaStartCommandFile.any())

		for _, g := range globs {
			for _, f := range g.files {
				if strings.HasSuffix(f.relString(), ".java") {
					w = append(w, f.rel().any())
				}
			}
		}

		w = append(w, strYaEndCommandFile.any())
		b.cmd(w, 0)
		b.outputs = append(b.outputs, lint)
	}

	for _, dep := range d.externalDeps {
		if rel, ok := strings.CutPrefix(dep, "$(S)/"); ok {
			b.input(source(rel))
		} else {
			b.input(source(jm.dir, "/", dep))
		}
	}

	b.setKV(pkJV, pcLightBlue, true)
	b.deps = append(b.deps, ctx.javaClosureDeps(closure)...)
	b.emitTo(jm.outRef, ctx.target)
}

func (ctx *GenCtx) javaEmitIJar(jm *JavaModule) {
	full := jm.selfPeers[0]
	d := full.data
	b := &javaNodeBuilder{ctx: ctx}
	ref, ijar := ctx.tool(argContribToolsIjar)
	abi := d.abiJar && d.withKotlin && !d.uberjar
	args := []ANY{ijar.any()}
	input := full.target

	if abi {
		args = append(args, strMetadataSource.any(), full.target.any())
		input = build(full.dir, "/", full.name, "-stripped.jar")
	}

	args = append(args, strTargetLabel.any(), internV("//", jm.dir, ":", jm.name).any(), strInjectingRuleKind.any(), strArcadiaIjar.any(), input.any(), jm.target.any())

	b.cmd(args, 0)
	b.cmd(append(b.python("build/scripts/canonicalize_java_abi_jar.py"), strIjar.any(), jm.target.any()), 0)
	b.setKV(pkIJ, pcLightBlue, false)
	b.exts = []KVExt{{Key: "exclude_from_idea", Val: "yes"}}
	b.outputs = []VFS{jm.target}
	b.deps = append(b.deps, ref, ctx.javaUse(full.target))
	b.emitTo(jm.outRef, ctx.target)
}
