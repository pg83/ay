package main

import (
	"crypto/md5"
	"fmt"
	"path"
	"slices"
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
	binding int
}

func (ctx *GenCtx) javaSrcsGlobs(jm *JavaModule) []javaSrcsGlob {
	var out []javaSrcsGlob

	for _, spec := range jm.data.javaSrcs {
		sum := md5.Sum([]byte(spec.salt))
		name := fmt.Sprintf("uniq_%x", sum[:])

		g := javaSrcsGlob{
			spec:    spec,
			name:    name,
			srcDir:  spec.srcDir,
			srcTxt:  build(jm.dir, "/misc/", name, ".src.txt"),
			resTxt:  build(jm.dir, "/misc/", name, ".res.txt"),
			binding: len(out),
		}

		for i := range out {
			if out[i].name == name {
				g.binding = i

				break
			}
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

		if g.binding < len(out) {
			g.files = out[g.binding].files
		} else if !g.genSrcs {
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

	for i := range out {
		if out[i].binding != i {
			continue
		}

		count := 0

		for j := i; j < len(out); j++ {
			if out[j].binding == i {
				count++
			}
		}

		if count == 1 || len(out[i].files) == 0 {
			continue
		}

		files := slices.Repeat(out[i].files, count)

		for j := i; j < len(out); j++ {
			if out[j].binding == i {
				out[j].files = files
			}
		}
	}

	return out
}

func javaJoin(dir VFS, rel string) VFS {
	if dir.isBuild() {
		return build(dir.relString(), "/", rel)
	}

	return source(dir.relString(), "/", rel)
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
	b := ctx.javaBuilderReq(jm, &javaReqCPU4)
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

	for _, jsrc := range jm.jsrcs {
		prep = append(prep, jsrc.any())
		b.input(jsrc)
		b.deps = append(b.deps, b.use(jsrc))
	}

	for _, out := range jm.macroOuts {
		if strings.HasSuffix(out.relString(), ".jsrc") {
			prep = append(prep, out.any())
		}
	}

	for _, gt := range jm.gentars {
		prep = append(prep, gt.any())
		b.input(gt)
		b.deps = append(b.deps, b.use(gt))
	}

	for _, out := range jm.macroOuts {
		if strings.HasSuffix(out.relString(), ".gentar") {
			prep = append(prep, out.any())
		}
	}

	for _, g := range globs {
		prep = append(prep, strJsources.any(), g.srcTxt.any(), strResources.any(), g.resTxt.any(), strSrcdir.any(), internStr(g.srcDir).any())

		for _, f := range g.files {
			prep = append(prep, f.any())
		}

		for _, f := range g.spec.files {
			prep = append(prep, javaJoin(g.absDir, f).any())
		}

		b.input(g.files...)
	}

	for _, out := range jm.macroOuts {
		if ext := path.Ext(out.relString()); ext == ".java" || ext == ".kt" {
			prep = append(prep, out.any())
		}
	}

	for _, kind := range []string{"JAR", "SRC_JAR"} {
		for _, g := range globs {
			if !g.genSrcs && !g.spec.varSrcDir || kind == "SRC_JAR" && !d.sourcesJar {
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

		if d.withKapt {
			ctx.javaKaptCmds(jm, b, kt)
		}

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

	if d.addDLLs {
		jni := b.fsTools(strLinkOrCopyToDir.any(), strYaStartCommandFile.any())

		for _, ext := range []string{".so", ".dll", ".dylib"} {
			for _, p := range javaClosureJars(closure, ext) {
				jni = append(jni, p.any())
			}
		}

		b.cmd(append(jni, strYaEndCommandFile.any(), cls.any()), 0)
	}

	packed := jm.target

	if d.uberjar {
		packed = build(jm.dir, "/", jm.name, ".lib.jar")
	}

	b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strCfvm.any(), packed.any(), mf.any(), str4.any()}, cls, lcAll)

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

	if d.uberjar {
		ctx.javaLinkUberjar(jm, b, globals, closure, packed, len(globs) > 0)
	}

	for _, dep := range d.externalDeps {
		if rel, ok := strings.CutPrefix(dep, "$(S)/"); ok {
			b.input(source(rel))
		} else {
			b.input(source(jm.dir, "/", dep))
		}
	}

	b.setKV(pkJV, pcLightBlue, true)
	b.closure(closure)
	b.macroOuts(jm)
	b.emitTo(jm.outRef)
}

func (ctx *GenCtx) javaEmitIJar(jm *JavaModule) {
	full := jm.selfPeers[0]
	d := full.data
	b := ctx.javaBuilder(jm)
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
	b.input(ijar, full.target)
	b.setKV(pkIJ, pcLightBlue, false)
	b.exts = []KVExt{{Key: "exclude_from_idea", Val: "yes"}}
	b.outputs = []VFS{jm.target}
	b.deps = append(b.deps, ref, b.use(full.target))
	b.macroOuts(jm)
	b.emitTo(jm.outRef)
}

func (ctx *GenCtx) javaKaptCmds(jm *JavaModule, b *javaNodeBuilder, compileKt []ANY) {
	d := jm.data
	bindir := jm.path.rel().build().string()
	ap := ctx.javaToolJar(jm, b, "contrib/java/org/jetbrains/kotlin/kotlin-annotation-processing/"+javaKotlinVersion)
	args := b.python("build/scripts/with_kapt_args.py")

	args = append(args, strApClasspath.any())

	for _, dir := range d.kaptAPClasspath {
		if tool := ctx.javaToolJar(jm, b, dir); tool != 0 {
			args = append(args, tool.any())
		}
	}

	args = append(args, strApOptions.any())
	args = append(args, internAnys(d.kaptAPOptions)...)
	args = append(args, str3.any())
	args = append(args, compileKt...)

	args = append(args, internV("-Xplugin=", ap.string()).any(),
		strP.any(), internV("plugin:org.jetbrains.kotlin.kapt3:sources=", bindir, "/kapt_gen_src").any(),
		strP.any(), internV("plugin:org.jetbrains.kotlin.kapt3:classes=", bindir, "/kt_cls").any(),
		strP.any(), internV("plugin:org.jetbrains.kotlin.kapt3:stubs=", bindir, "/kapt_gen_stubs").any(),
		strP.any(), strPluginOrgJetbrainsKotlinKapt3AptmodeStubsandapt.any())

	args = append(args, internAnys(d.kaptOpts)...)

	for _, p := range d.kaptProcessors {
		args = append(args, strP.any(), internV("plugin:org.jetbrains.kotlin.kapt3:processors=", p).any())
	}

	b.cmd(args, bldRootDirVFS, EnvVar{Name: envLcAll, Value: strEnUsUtf8.any()})

	res := b.python("build/scripts/resolve_java_srcs.py")

	res = append(res, strD.any(), internV(bindir, "/kapt_gen_src").any(), strIncludePatterns.any(),
		strJava2.any(), strKt.any(), strResolveKotlin.any(), strAppend.any(),
		strS2.any(), internV(bindir, "/all-java.srclst").any(), strK.any(), internV(bindir, "/misc/all-kt-sources.txt").any(),
		strR.any(), internV(bindir, "/not-used.txt").any())

	b.cmd(res, 0)
}

func (ctx *GenCtx) javaToolJar(jm *JavaModule, b *javaNodeBuilder, dir string) VFS {
	res := ctx.javaPeerResult(jm, dir, false)

	if res == nil || res.java == nil {
		return 0
	}

	if ref := b.use(res.java.target); ref != 0 {
		b.input(res.java.target)
		b.deps = append(b.deps, ref)
	}

	return res.java.target
}

func (ctx *GenCtx) javaLinkUberjar(jm *JavaModule, b *javaNodeBuilder, globals map[string]VFS, closure []JavaClosureEntry, libJar VFS, hasSources bool) {
	d := jm.data
	jdk := javaJDKResource(d.jdkReal)
	bindir := jm.path.rel().build().string()

	args := []ANY{b.resourcePath(globals, jdk, "/bin/java"), strCp.any(), b.resourcePath(globals, "UBERJAR"+d.jdkReal, "/devtools-java_shader.jar"),
		strRuYandexDevtoolsEmigranteMain.any(), strOutJar.any(), jm.target.any()}

	if hasSources {
		args = append(args, strJar2.any(), libJar.any())
	}

	for _, p := range javaClosureJars(closure, ".jar") {
		args = append(args, strJar2.any(), p.any())
	}

	args = append(args, internAnys(d.uberjarOpts)...)
	b.cmd(args, 0)

	mf := build(jm.dir, "/", jm.name, ".jar.__vcs_version__.mf")

	b.cmd(append(b.python("build/scripts/mkdir.py"), internV(bindir, "/_empty/META-INF").any()), 0)
	b.cmd(append(b.python("build/scripts/touch.py"), internV(bindir, "/_empty/META-INF/MANIFEST.MF").any()), 0)
	b.cmd(append(b.python("build/scripts/vcs_info.py"), strOutputJava.any(), bldVcsJson.any(), mf.any(), jm.target.any()), 0)
	b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strUfv.any(), jm.target.any(), strMetaInfManifestMf.any()}, build(jm.dir, "/_empty"))
	b.cmd([]ANY{b.resourcePath(globals, jdk, "/bin/jar"), strUfvm.any(), jm.target.any(), mf.any()}, 0)
}
