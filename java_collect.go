package main

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

var javaConfGlobals = map[string]string{
	"KOTLIN_VERSION":              javaKotlinVersion,
	"_KOTLIN_VERSION":             javaKotlinVersion,
	"JAVA_PROTO_RUNTIME_VERSION":  javaProtoRuntimeVersion,
	"JAVA_PROTO_COMPILER_VERSION": javaProtoRuntimeVersion,
	"JAVA_PROTO_COMMON_VERSION":   javaProtoCommonVersion,
	"JAVA_GRPC_VERSION":           javaGrpcVersion,
	"KOTLIN_GRPC_VERSION":         javaKotlinGrpcVersion,
	"JAVA_FLATBUFFERS_VERSION":    javaFlatbuffersVersion,
	"JAVA_JUNIT6_VERSION":         "6.0.2",
	"DETEKT_VERSION":              "1.23.7",
	"JDK_LATEST_VERSION":          "26",
	"JAVA_NETTY_NETTY_VERSION":    "4.1.79.Final",
	"JDK_VERSION":                 "",
	"KOTLIN_BOM_FILE":             "${ARCADIA_ROOT}/contrib/java/org/jetbrains/kotlin/kotlin-bom/" + javaKotlinVersion + "/ya.dependency_management.inc",
}

var javaConfGlobalEnv = func() []javaConfGlobal {
	out := make([]javaConfGlobal, 0, len(javaConfGlobals))

	for name, value := range javaConfGlobals {
		out = append(out, javaConfGlobal{name: internEnv(name), value: internStr(value)})
	}

	return out
}()

var javaPathVars = map[string]bool{
	"BINDIR": true, "CURDIR": true, "ARCADIA_ROOT": true, "ARCADIA_BUILD_ROOT": true, "MODDIR": true,
}

var javaUberjarFlagVars = []string{
	"UBERJAR_PREFIX_FLAG",
	"UBERJAR_HIDE_EXCLUDE_FLAGS",
	"UBERJAR_HIDE_INCLUDE_FLAGS",
	"UBERJAR_PATH_EXCLUDES",
	"UBERJAR_MANIFEST_TRANSFORMER_MAIN_FLAG",
	"UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS",
	"UBERJAR_APPENDING_TRANSFORMER_FLAGS",
	"UBERJAR_SERVICES_RESOURCE_TRANSFORMER_FLAG",
}

var javaConfGlobalBindings = func() []confMacroBinding {
	var out []confMacroBinding

	for name, value := range javaConfGlobals {
		out = append(out, confMacroBinding{name: internStr(name), values: []ANY{internAny(value)}, bound: true})
	}

	return out
}()

const (
	javaKotlinVersion        = "2.3.10"
	javaProtoRuntimeVersion  = "4.33.0"
	javaProtoCommonVersion   = "2.63.2"
	javaGrpcVersion          = "1.51.0"
	javaKotlinGrpcVersion    = "1.3.1"
	javaFlatbuffersVersion   = "24.3.25"
	javaKotlinCompilerGlobal = "${KOTLIN_COMPILER_RESOURCE_GLOBAL}"
	javaErrorProneBaseChecks = "-Xep:FunctionalInterfaceMethodChanged:WARN -Xep:ReturnValueIgnored:WARN"
	javaErrorProneAddOpens   = "-J--add-opens=jdk.compiler/com.sun.tools.javac.code=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.comp=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.file=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.main=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.model=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.processing=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.tree=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.util=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.jvm=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.api=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED"
	javaErrorProne271Forced  = "-Xep:InlineMeInliner:OFF -Xep:SameNameButDifferent:OFF"
	javaErrorProne2470Forced = "--should-stop=ifError=FLOW"
)

type javaConfGlobal struct {
	name  ENV
	value STR
}

func (u *javaUnit) compilable() bool {
	return u.kind == juJar || u.kind == juRunnable && !u.contribRunnable || u.kind == juTest
}

func (d *JavaData) sourceCommandPeers(jm *JavaModule, source string, env Environment) {
	ext := strings.TrimPrefix(path.Ext(source), ".")

	if ext == "" || slices.Contains(d.commandExts, ext) {
		return
	}

	d.commandExts = append(d.commandExts, ext)

	for _, peer := range jm.instance.Platform.conf.commandDirs("_SRC:"+ext, env).peerdirs {
		if peer != jm.dir {
			d.commandPeers = append(d.commandPeers, peer)
		}
	}
}

func javaModulePrologue(ctx *GenCtx, jm *JavaModule, d *JavaData, env Environment) {
	sourcesJar := env.is(envBUILD_TYPE, strDebug) && !env.is(envNO_DEBUGINFO, strYes)
	sbom := sbomActive(ctx, jm.instance)

	d.embedSbom = sbom || ctx.sbomEnabled && env.bool(envEmbedSbom)

	for _, flag := range []struct {
		name  ENV
		value bool
	}{
		{envSourcesJar, sourcesJar},
		{envEmbedSbom, sbom},
		{envNeedSbomInfo, sbom},
	} {
		if !env.hasBindingID(flag.name) {
			env.setBool(flag.name, flag.value)
		}
	}

	javaSetJDK(d, env, d.jdkReal)
	jm.instance.Platform.conf.set(env, envJdkRealVersion, nil)

	if jm.unit.proto {
		env.setBool(envJavaProto, true)
	}

	if jm.unit.fbs {
		env.setBool(envJavaFbs, true)
	}
}

func collectJavaData(ctx *GenCtx, jm *JavaModule, stmts []Stmt, env Environment) *JavaData {
	d := &JavaData{jdkReal: ctx.javaDefaultJDK(), env: env, lintLevel: "extended", abiJar: true, srcDirs: []VFS{jm.path}, dm: ctx.javaDMBuf[:0], excludes: ctx.javaExclBuf[:0], rawVars: ctx.javaRawVars}

	d.setVars = SetVars{vals: clearedMap(ctx.javaSetVars.vals), appends: clearedMap(ctx.javaSetVars.appends), parts: ctx.javaSetVars.parts[:0]}

	defer func() { ctx.javaSetVars.parts = d.setVars.parts[:0] }()

	d.javacOpts.base = []string{"-J-XX:ActiveProcessorCount=4"}
	d.javacOpts.cur = d.javacOpts.base
	clear(d.rawVars)
	collectJavaStmts(ctx, jm, d, stmts, env)

	peers, dm := len(d.peerStmts), len(d.dm)

	javaModuleBody(ctx, jm, d, env)

	slices.Reverse(d.peerStmts)
	slices.Reverse(d.peerStmts[:len(d.peerStmts)-peers])
	slices.Reverse(d.peerStmts[len(d.peerStmts)-peers:])
	slices.Reverse(d.dm)
	slices.Reverse(d.dm[:len(d.dm)-dm])
	slices.Reverse(d.dm[len(d.dm)-dm:])

	d.rawVars, d.rawBindings = nil, nil

	if !d.autoincluded {
		throwFmt("%s: module declaration has no END()", jm.dir)
	}

	if d.noLint == "none" || env.bool(envCppAnalysisMode) {
		d.styleChecks = nil
	}

	if ctx.testMode {
		for i := range d.styleChecks {
			if d.styleChecks[i].CPP {
				prepareCPPStyle(&d.styleChecks[i], jm.dir)
				ctx.resolveStyleConfig(&d.styleChecks[i], jm.dir)
			}
		}
	}

	d.sourcesJar = env.bool(envSourcesJar)
	d.ktlintSkipSourceDirs = env.bool(envWithYa1931)
	d.javaProtoCommonVersion = env.string(envJavaProtoCommonVersion)
	d.embedSbom = d.embedSbom || ctx.sbomEnabled && env.bool(envEmbedSbom)
	d.needSbomInfo = env.bool(envNeedSbomInfo)
	d.modver = sbomModuleVersion(env)
	d.protoNamespace = env.string(envProtoNamespace)

	for _, name := range javaUberjarFlagVars {
		value, _ := env.lookup(name)

		d.uberjarOpts = append(d.uberjarOpts, splitJavaCommandWords(expandStmtToken(value, env))...)
	}

	for _, image := range strings.Fields(expandStmtToken(env.string(envDockerImagesValue), env)) {
		if image == `""` || image == "''" {
			continue
		}

		if !strings.HasPrefix(image, "docker://") {
			tag, link, _ := strings.Cut(image, "=")

			image = link + "=" + tag
		}

		d.test.dockerImages = append(d.test.dockerImages, image)
	}

	for _, value := range strings.Fields(expandStmtToken(env.string(envJavaIgnoreClasspathClash), env)) {
		if value != `""` && value != "''" {
			d.test.ignoreClasspathClash = append(d.test.ignoreClasspathClash, strings.ReplaceAll(value, `${"$"}`, "$"))
		}
	}

	bindRunProgramCalls(d.runPrograms, env, &ctx.frames[ctx.frameDepth-1].d.sourceEnv)

	d.validateData = resourceValidationEnabled(env)
	d.fakeModule = env.bool(envFakeModule)
	d.moduleType = env.string(envMODULE_TYPE)
	d.jarResource = env.string(envJarResourceID)
	d.srcResource = env.string(envSrcResourceID)
	d.localJar = env.string(envLocalJarPath)
	d.localSourcesJar = env.string(envLocalSourcesJarPath)
	d.commonGoogleAPIs = !env.is(envCommonGoogleApis, strNone)
	d.varPeers = ctx.varWords(internStr(env.expandVariable(env.string(envPeerdir))))
	d.peerdirTags = javaTags(jm.instance.Platform.conf, env, envPeerdirTags)
	d.moduleTag = env.stringID(envMODULE_TAG)
	d.dmTagsExclude = javaTags(jm.instance.Platform.conf, env, envDependencyManagementTagsExclude)
	d.sbrUIDExt = env.string(envSbrUIDExt)
	d.test.parallelWorkers = env.string(envParallelTestsWithinNodeWorkers)

	if jm.unit.fbs {
		d.env = env.cloneInto(new(EnvStore))
	}

	return d
}

func javaModuleArgs(jm *JavaModule, d *JavaData, args []ANY) {
	if jm.stmt == tokJtestFor {
		if len(args) > 0 {
			d.unittestDir = path.Clean(args[0].string())
		}

		return
	}

	for i := 0; i < len(args); i++ {
		a := args[i].string()

		if a == "PREFIX" {
			if i+1 < len(args) {
				d.prefix = args[i+1].string()
				i++
			}

			continue
		}

		if d.name == "" {
			d.name = a
		}
	}
}

func collectJavaAutoinclude(ctx *GenCtx, jm *JavaModule, d *JavaData, env Environment) {
	if d.autoincluded {
		return
	}

	d.autoincluded = true
	collectJavaStmts(ctx, jm, d, ctx.autoincludeStmts(jm.dir), env)
}

func collectJavaStmts(ctx *GenCtx, jm *JavaModule, d *JavaData, stmts []Stmt, env Environment) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *IncludeStmt:
			ctx.withIncludedStmts(v, env, func(included []Stmt) { collectJavaStmts(ctx, jm, d, included, env) })
		case *MissingIncludeStmt:
			throwFmt("%s:%d: INCLUDE file %s not found", jm.dir, v.Line, v.Path)
		case *ModuleStmt:
			owner := multimoduleOwner(v)
			platform := jm.instance.Platform

			d.confModule = platform.conf.module(owner, multimoduleSub(owner, jm.unit.sub, LangJava, false).string())

			platform.conf.initModule(env, d.confModule, &d.setVars)
			javaModulePrologue(ctx, jm, d, env)

			javaModuleArgs(jm, d, expandSourceArguments(v.Args, env))

			if jm.stmt == tokJtestFor && d.unittestDir != "" {
				env.setString(envUnittestDir, d.unittestDir)
			}

		case *EndStmt:
			collectJavaAutoinclude(ctx, jm, d, env)
		case *PeerdirStmt:
			if jm.unit.ignorePeerdir {
				continue
			}

			for _, p := range expandStmtTokens(v.Paths, env) {
				ps := p.string()

				if ps == "ADDINCL" || ps == "GLOBAL" || strings.Contains(ps, "${") {
					continue
				}

				d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: peerdirModulePath(p).string(), explicit: !v.Implicit})
			}
		case *SetStmt:
			valueID := expandSourceScalarVarRefSTR(v.Value, env)

			env.setFromSTR(v.NameEnv, valueID)
			d.setVars.set(v.NameEnv.str(), valueID)
			jm.instance.Platform.conf.set(env, v.NameEnv, &d.setVars)
			d.setRawVar(v.Name, d.rawExpand(v.Value))
		case *DefaultVarStmt:
			value := expandSourceScalarVarRef(v.Value, env)
			bound := env.hasBindingID(v.NameEnv)

			env.setDefaultString(v.NameEnv, value)

			if !bound {
				d.setVars.set(internStr(v.VarName), internStr(value))
				jm.instance.Platform.conf.set(env, v.NameEnv, &d.setVars)
				d.setRawVar(v.VarName, d.rawExpand(v.Value))
			}
		case *IfStmt:
			taken := v.Then

			if !evalCond(v.Cond, env) {
				taken = v.Else
			}

			collectJavaStmts(ctx, jm, d, taken, env)
		case *SrcsStmt:
			srcs := anyStrs(expandStmtTokens(v.Sources, env))

			if jm.unit.jni {
				srcs = javaJNISwigSrcs(srcs)
			}

			for _, ss := range srcs {
				if ss == "GLOBAL" {
					continue
				}

				d.sourceCommandPeers(jm, ss, env)

				switch {
				case jm.unit.proto && (extIsProto(ss) || extIsGztproto(ss)):
					d.protoSrcs = append(d.protoSrcs, ss)
				case jm.unit.proto && strings.HasSuffix(ss, ".ev"):
					d.evSrcs = append(d.evSrcs, ss)
				default:
					d.srcs = append(d.srcs, ss)
				}
			}
		case *RunProgramStmt:
			rp := captureRunProgram(v, env)

			rp.DeclSeq = d.nextDeclSeq()
			d.runPrograms = append(d.runPrograms, rp)
		case *RunPythonStmt:
			rp := expandRunPythonStmt(v, env)

			rp.DeclSeq = d.nextDeclSeq()
			d.runPython = append(d.runPython, rp)
		case *FromSandboxStmt:
			expanded := *v

			expanded.ResourceId = expandStmtTokenAny(v.ResourceId, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.Renames = expandStmtTokens(v.Renames, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)
			expanded.DeclSeq = d.nextDeclSeq()

			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *SrcDirStmt:
			for _, dir := range expandStmtTokens(v.Dirs, env) {
				d.srcDirs = append(d.srcDirs, parseModulePathVFS(dir.string()))
			}
		case *AddInclStmt:
			for _, dir := range expandConfigVFSPaths(v.ProtoPaths, env) {
				d.protoAddIncl = append(d.protoAddIncl, javaAddIncl{dir: dir})
			}

			for _, dir := range expandConfigVFSPaths(v.ProtoGlobalPaths, env) {
				d.protoAddIncl = append(d.protoAddIncl, javaAddIncl{dir: dir, global: true})
			}

			if jm.unit.fbs {
				for _, dir := range expandConfigVFSPaths(v.FlatcGlobalPaths, env) {
					d.protoAddIncl = append(d.protoAddIncl, javaAddIncl{dir: dir, global: true})
				}

				for _, dir := range expandConfigVFSPaths(v.FlatcPaths, env) {
					d.protoAddIncl = append(d.protoAddIncl, javaAddIncl{dir: dir})
				}
			}
		case *UnknownStmt:
			if d.confModule != nil {
				verdict := d.confModule.verdicts[v.macroName()]

				if verdict&confMacroRestricted != 0 && verdict&confMacroIgnored == 0 {
					ctx.onWarn(Warn{Kind: WarnMisconfiguration, Message: fmt.Sprintf("%s: %s is not allowed for %s modules", jm.dir, v.macroName().string(), jm.stmt.string())})
				}

				if verdict != 0 {
					continue
				}
			}

			if v.Name == tokBundle || v.Name == tokBundleOutput {
				expanded := *v

				expanded.Args = expandStmtTokens(v.Args, env)

				for _, entry := range parseBundleEntries(expanded) {
					entry.DeclSeq = d.nextDeclSeq()
					d.bundles = append(d.bundles, entry)
				}

				continue
			}

			if v.Name == tokCopyFile || v.Name == tokCopyFileWithContext {
				args := expandStmtTokens(v.Args, env)
				entry := parseCopyFileEntry(anyStrs(args), v.Name == tokCopyFileWithContext, v.Line)

				entry.DeclSeq = d.nextDeclSeq()
				d.copyFiles = append(d.copyFiles, entry)
				recordHandledMacro(v.Name, args)

				continue
			}

			if assignment := declarativeMacroAssignment(v, false, env); assignment != nil {
				recordHandledMacro(v.Name, expandStmtTokens(v.Args, env))
				collectJavaStmts(ctx, jm, d, []Stmt{assignment}, env)

				continue
			}

			if list, ok := d.ruleList(jm, v.Raw); ok {
				if list != nil {
					*list = javaAppendResolved(*list, v.Args, ctx.javaExpandRuleArgs(v.Args, env))
				}

				continue
			}

			raw := v.Args

			if v.LexicalArgs != nil {
				raw = v.LexicalArgs
			}

			var args []string

			stmt := *v

			if v.Raw == strRunJavaProgram {
				args = splitJavaCommandWords(expandStmtToken(strings.Join(anyStrs(raw), " "), env))
				stmt.Args = internAnys(args)
			} else if v.Raw == strGenerateScript {
				stmt.Args = expandSourceArguments(raw, env)
				args = anyStrs(stmt.Args)
			} else {
				stmt.Args = expandStmtTokens(v.Args, env)
				args = anyStrs(stmt.Args)
			}

			d.recurses = appendConfigurationRecurses(d.recurses, jm.dir, stmt, jm.instance.Platform)

			switch v.Name {
			case tokStyleCpp, tokStyleCppMaps, tokStyleCppUserSessions, tokStyleCppYt:
				d.collectCPPStyle(ctx.fs, jm.dir, stmt, env)

				continue
			case tokStyleJSON, tokStyleYAML, tokStyleYAMLConf, tokStyleYQL:
				if !styleChecksDisabled(env) {
					d.styleChecks = append(d.styleChecks, explicitStyleCheck(v.Name, env))
				}

				continue
			}

			if !applyJavaMacro(ctx, jm, d, v.Raw, args, raw, env) || javaConfBodyToo(v.Raw) {
				expandJavaConfMacro(ctx, jm, d, stmt, env)
			}
		}
	}
}

func javaConfBodyToo(name STR) bool {
	switch name {
	case kwWITH_KOTLIN, kwUSE_ERROR_PRONE, kwUBERJAR, kwWITH_KOTLIN_GRPC, kwJAVA_PROTO_PLUGIN:
		return true
	}

	return false
}

func javaModuleBody(ctx *GenCtx, jm *JavaModule, d *JavaData, env Environment) {
	if d.confModule == nil {
		return
	}

	platform := jm.instance.Platform

	d.inModuleBody = true

	defer func() { d.inModuleBody = false }()

	for _, call := range platform.conf.bodyCalls(d.confModule) {
		args := platform.conf.expandArgs(env, call.args)

		var scratch [1]Stmt

		stmts := appendMacroCallStmts(scratch[:0], call.name, args, args, 0, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{jm.dir, call.name.string()}, a...)...)
		})

		markImplicitPeers(stmts)
		collectJavaStmts(ctx, jm, d, stmts, env)
	}
}

func expandJavaConfMacro(ctx *GenCtx, jm *JavaModule, d *JavaData, v UnknownStmt, env Environment) {
	platform := jm.instance.Platform
	macro := platform.conf.alias(v.Raw)

	if macro == nil {
		return
	}

	platform.conf.macroCalls(macro, v, env, &d.confMacroDepth, ctx.onWarn, jm.dir, func(name STR, args []ANY) {
		if d.inModuleBody && baseMacro(name) {
			return
		}

		var scratch [1]Stmt

		stmts := appendMacroCallStmts(scratch[:0], name, args, args, v.Line, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{macro.file, name.string()}, a...)...)
		})

		markImplicitPeers(stmts)
		collectJavaStmts(ctx, jm, d, stmts, env)
	})
}

func javaJNISwigSrcs(srcs []string) []string {
	out := make([]string, 0, len(srcs))

	for _, ext := range []string{".java", ".swg"} {
		for _, src := range srcs {
			if strings.HasSuffix(src, ext) {
				out = append(out, src)
			}
		}
	}

	return out
}

func (d *JavaData) swigSrcs() []string {
	var out []string

	for _, src := range d.srcs {
		if extIsSwg(src) {
			out = append(out, src)
		}
	}

	return out
}

func javaKotlinPluginOpts(plugin string, opts []string) []string {
	out := make([]string, 0, 2*len(opts))

	for _, o := range opts {
		out = append(out, "-P", "plugin:org.jetbrains.kotlin."+plugin+":"+o)
	}

	return out
}

func parseJavaSrcs(args []string) JavaSrcsSpec {
	spec := JavaSrcsSpec{srcDir: "."}
	mode := ""

	for i := 0; i < len(args); i++ {
		a := args[i]

		switch a {
		case "RESOURCES":
			spec.resources = true
			mode = ""

			continue
		case "SKIP_CHECK_SRCDIR":
			spec.skipCheckSrcdir = true
			mode = ""

			continue
		case "SRCDIR", "PACKAGE_PREFIX":
			if i+1 < len(args) {
				if a == "SRCDIR" {
					spec.srcDir = args[i+1]
				} else {
					spec.packagePrefix = args[i+1]
				}

				i++
			}

			mode = ""

			continue
		case "EXCLUDE", "FILES":
			mode = a

			continue
		}

		switch mode {
		case "EXCLUDE":
			spec.excludes = append(spec.excludes, a)
		case "FILES":
			spec.files = append(spec.files, a)
		default:
			spec.globs = append(spec.globs, a)
		}
	}

	return spec
}

func applyJavaMacro(ctx *GenCtx, jm *JavaModule, d *JavaData, name STR, args []string, raw []ANY, env Environment) bool {
	u := jm.unit
	arg0 := ""

	if name == kwFILES {
		for _, file := range args {
			if strings.HasPrefix(file, "${ARCADIA_BUILD_ROOT}") || strings.HasPrefix(file, "$(B)/") {
				continue
			}

			d.files = append(d.files, file)
		}

		return true
	}

	if name != strGenerateScript {
		args = javaDropUnresolved(args)
	}

	if len(args) > 0 {
		arg0 = args[0]
	}

	switch name {
	case kwVERSION:
		env.setString(envModver, strings.Join(args, " "))
	case kwLICENSE:
		if ctx.sbomEnabled && env.bool(envNeedSbomInfo) {
			d.sbomComponent = true
		}
	case kwFLATC_FLAGS:
		d.flatcFlags = append(d.flatcFlags, internAnys(args)...)
	case strSetAppend:
		if len(args) == 0 {
			return true
		}

		value := strings.Join(args[1:], " ")

		if prev, ok := env.lookup(args[0]); ok && prev != "" {
			value = prev + " " + value
		}

		env.setString(internEnv(args[0]), value)
		d.setVars.set(internStr(args[0]), internStr(value))
		jm.instance.Platform.conf.set(env, internEnv(args[0]), &d.setVars)

		rawValue := d.rawExpand(strings.Join(anyStrs(raw[1:]), " "))

		if prev := d.rawVars[args[0]]; prev != "" {
			rawValue = prev + " " + rawValue
		}

		d.setRawVar(args[0], rawValue)

		if args[0] == "_PROTOC_FLAGS" {
			d.protocFlags = append(d.protocFlags, args[1:]...)
		}
	case kwJDK_VERSION, kwDEFAULT_JDK_VERSION:
		checkJdkVersion(arg0)
		javaSetJDK(d, env, arg0)
		jm.instance.Platform.conf.set(env, envJdkRealVersion, nil)

		d.hasPerModuleJDK = true
		d.recalcJavacOpts(jm)
	case kwWITH_KOTLIN:
		d.withKotlin = true
		d.recalcKotlincOpts(jm)
	case kwWITH_KAPT:
		d.withKapt = true

		if arg0 == "GENERATE_KOTLIN" {
			d.kaptAPOptions = append(d.kaptAPOptions, "kapt.kotlin.generated="+jm.path.rel().build().string()+"/kapt_gen_src")
		}
	case kwKAPT_OPTS:
		for _, a := range args {
			d.kaptOpts = append(d.kaptOpts, "-P", a)
		}
	case kwKAPT_ANNOTATION_PROCESSOR:
		d.kaptProcessors = append(d.kaptProcessors, args...)
	case kwKAPT_ANNOTATION_PROCESSOR_CLASSPATH:
		d.kaptAPClasspath = append(d.kaptAPClasspath, args...)

		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}
	case kwKAPT_ANNOTATION_PROCESSOR_OPTIONS:
		d.kaptAPOptions = append(d.kaptAPOptions, args...)
	case kwKOTLINC_FLAGS:
		d.kotlincOpts.appendStmt(args...)
	case kwWITH_KOTLINC_ALLOPEN:
		d.kotlincOpts.appendStmt(javaKotlinPluginOpts("allopen", args)...)
		d.allopen = true
		d.recalcKotlincOpts(jm)
	case kwWITH_KOTLINC_LOMBOK:
		d.kotlincOpts.appendStmt(javaKotlinPluginOpts("lombok", args)...)
		d.lombok = true
		d.recalcKotlincOpts(jm)
	case kwWITH_KOTLINC_NOARG:
		d.kotlincOpts.appendStmt(javaKotlinPluginOpts("noarg", args)...)
		d.noarg = true
		d.recalcKotlincOpts(jm)
	case kwWITH_KOTLINC_SERIALIZATION:
		d.serialization = true
		d.recalcKotlincOpts(jm)
	case kwWITH_KOTLINC_DETEKT:
		for _, o := range args {
			d.kotlincOpts.appendStmt("-P", "plugin:detekt-compiler-plugin:"+o)
		}

		d.detekt = true
		d.recalcKotlincOpts(jm)
	case kwENABLE_KOTLIN_ABI_JAR:
		d.abiJar = true
		d.recalcKotlincOpts(jm)
	case kwJAVAC_FLAGS:
		d.javacOpts.appendStmt(args...)
	case kwENABLE_PREVIEW:
		d.jvmArgs = append(d.jvmArgs, "--enable-preview")
		d.javacOpts.appendStmt("--enable-preview", "--release", d.jdkReal)
		d.enablePreview = true
	case kwUSE_ERROR_PRONE:
		d.errorProne = true
		d.recalcJavacOpts(jm)
	case kwUBERJAR:
		d.uberjar = true
		d.recalcKotlincOpts(jm)
	case kwUBERJAR_HIDING_PREFIX:
		env.setString(envUberjarPrefixFlag, "--shade-prefix "+arg0)
	case kwUBERJAR_HIDE_EXCLUDE_PATTERN:
		for _, a := range args {
			appendMacroEnv(env, "UBERJAR_HIDE_EXCLUDE_FLAGS", "--shade-exclude "+a)
		}
	case kwUBERJAR_HIDE_INCLUDE_PATTERN:
		for _, a := range args {
			appendMacroEnv(env, "UBERJAR_HIDE_INCLUDE_FLAGS", "--shade-include "+a)
		}
	case kwUBERJAR_PATH_EXCLUDE_PREFIX:
		for _, a := range args {
			appendMacroEnv(env, "UBERJAR_PATH_EXCLUDES", "--uber-exclude "+a)
		}
	case kwUBERJAR_MANIFEST_TRANSFORMER_MAIN:
		env.setString(envUberjarManifestTransformerMainFlag, "--manifest-main "+arg0)
	case kwUBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE:
		if len(args) > 0 {
			appendMacroEnv(env, "UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS", "--manifest-attribute "+arg0+":'"+strings.Join(args[1:], " ")+"'")
		}
	case kwUBERJAR_APPENDING_TRANSFORMER:
		for _, a := range args {
			appendMacroEnv(env, "UBERJAR_APPENDING_TRANSFORMER_FLAGS", "--append-transformer "+a)
		}
	case kwUBERJAR_SERVICES_RESOURCE_TRANSFORMER:
		env.setString(envUberjarServicesResourceTransformerFlag, "--service-transformer")
	case kwJAVA_DEPENDENCIES_CONFIGURATION:
	case kwJAVA_TEST_PEERDIR, kwJAVA_RUNTIME_PEERDIR:
		if !javaMacroIsAlias(jm, name) {
			return true
		}

		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}
	case kwJAVA_API_PEERDIR:
		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}
	case kwJAVA_IMPL_PEERDIR:
		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}

		d.implPeers = append(d.implPeers, args...)
	case kwJUNIT_TESTS_JAR:
		if u.kind == juTest && len(args) > 0 {
			d.unittestDir = arg0
			d.unittestMod = path.Join(args...)
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: arg0})
		}
	case kwAUTO_SERVICE:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "contrib/java/com/google/auto/service/auto-service-annotations/" + arg0})
	case kwUSE_ANNOTATION_PROCESSOR:
	case kwANNOTATION_PROCESSOR, kwJAR_ANNOTATION_PROCESSOR:
		d.annotationProcs = append(d.annotationProcs, args...)
	case kwJAR_MAIN_CLASS:
		d.jarMainClass = arg0
	case kwJVM_ARGS:
		d.jvmArgs = append(d.jvmArgs, args...)
	case kwSIZE:
		d.test.size = strings.ToLower(arg0)
	case kwTIMEOUT:
		d.test.timeout = arg0
	case kwTEST_CWD:
		d.test.cwd = arg0
	case kwTAG:
		d.test.tags = append(d.test.tags, args...)
	case kwREQUIREMENTS:
		d.test.requirements = append(d.test.requirements, args...)
	case kwFORK_TESTS:
		d.test.forkMode = "tests"
	case kwFORK_SUBTESTS:
		d.test.forkMode = "subtests"
	case kwSPLIT_FACTOR:
		d.test.splitFactor = arg0
	case kwPARALLEL_TESTS_WITHIN_NODE:
		env.setString(envParallelTestsWithinNodeWorkers, arg0)
	case kwEXPERIMENTAL_FORK:
		d.test.experimentalFork = true
	case kwSYSTEM_PROPERTIES:
		d.test.systemProperties = append(d.test.systemProperties, args...)
	case kwTEST_JAVA_CLASSPATH_CMD_TYPE:
		d.test.classpathCmdType = arg0
	case kwUSE_RECIPE:
		d.test.recipes = append(d.test.recipes, strings.Join(args, " "))
	case kwDEPENDS:
		d.test.depends = append(d.test.depends, args...)
	case kwDOCKER_IMAGE:
		applyJavaMacro(ctx, jm, d, strSetAppend, append([]string{envDockerImagesValue.string()}, args...), append([]ANY{envDockerImagesValue.str().any()}, raw...), env)
	case kwDATA:
		d.test.data = append(d.test.data, args...)
	case kwDATA_FILES:
		for _, a := range args {
			d.test.data = append(d.test.data, testDataFile(a))
		}
	case kwJAVA_SRCS, kwFULL_JAVA_SRCS:
		if u.kind == juJar && !u.proto {
			spec := parseJavaSrcs(args)
			rawSpec := parseJavaSrcs(d.javaRawArgs(anyStrs(raw)))

			spec.salt = javaSrcsSalt(rawSpec)
			spec.varSrcDir = strings.HasPrefix(rawSpec.srcDir, "$") && !strings.HasPrefix(rawSpec.srcDir, "$S")

			d.javaSrcs = append(d.javaSrcs, spec)
		}
	case kwDEFAULT_JAVA_SRCS_LAYOUT, kwDEFAULT_JUNIT_JAVA_SRCS_LAYOUT:
		if u.kind != juJar || u.proto {
			return true
		}

		srcDir, resDir := "src/main/java", "src/main/resources"

		if name == kwDEFAULT_JUNIT_JAVA_SRCS_LAYOUT {
			srcDir, resDir = "java", "resources"
		}

		globs := []string{"**/*.java"}

		if d.withKotlin {
			globs = append(globs, "**/*.kt")
		}

		specs := []JavaSrcsSpec{
			{srcDir: srcDir, globs: globs, skipCheckSrcdir: true},
			{resources: true, srcDir: resDir, globs: []string{"**/*"}, skipCheckSrcdir: true},
		}

		for _, spec := range specs {
			spec.salt = javaSrcsSalt(spec)
			d.javaSrcs = append(d.javaSrcs, spec)
		}
	case strRunJavaProgram:
		expanded := javaRunProgramRawArgs(anyStrs(raw), env, d.rawVars)

		if bad := malformedCommandReference(expanded); bad != "" {
			ctx.onWarn(Warn{Kind: WarnSyntax, Message: fmt.Sprintf("in %s: RUN_JAVA_PROGRAM(%s) was not processed properly: could not parse command at %q. Skip RUN_JAVA_PROGRAM", jm.dir, strings.Join(anyStrs(raw), " "), bad)})

			return true
		}

		d.runJavaPrograms = append(d.runJavaPrograms, JavaRunProgram{args: args, raw: expanded})
		d.commandPeers = append(d.commandPeers, jm.instance.Platform.conf.commandDirs(name.string(), env).peerdirs...)
	case strGenerateScript:
		d.generateScripts = append(d.generateScripts, args)
	case kwWITH_JDK:
		d.withJDK = true
	case kwJAVA_EXTERNAL_DEPENDENCIES:
		d.externalDeps = append(d.externalDeps, args...)
	case kwADD_DLLS_TO_JAR:
		d.addDLLs = true
	case kwCHECK_JAVA_DEPS:
		d.checkJavaDeps = strings.ToLower(arg0)
	case kwJAVA_IGNORE_CLASSPATH_CLASH_FOR:
		property := []string{envJavaIgnoreClasspathClash.string()}

		applyJavaMacro(ctx, jm, d, strSetAppend, append(property, args...), append([]ANY{envJavaIgnoreClasspathClash.str().any()}, raw...), env)
	case kwENV:
		d.testEnv = append(d.testEnv, splitShellWords(`"`+strings.Join(anyStrs(raw), " ")+`"`)...)
	case kwLINT:
		d.lintLevel = arg0
	case kwNO_LINT:
		d.noLint = "none"

		if arg0 == "ktlint" {
			d.noLint = "ktlint"
		}
	case kwCOLLECT_KOTLIN_LINT_SRCS:
		d.collectKotlinLint = args
	case kwSTYLE_DETEKT:
		if d.noLint != "none" {
			d.customLints = append(d.customLints, javaCustomLint{linter: javaDetekt, configTypes: javaMacroKeyword(args, "CONFIG_TYPE")})
		}
	case kwUSE_KTLINT_OLD:
		d.ktlintOld = true
	case kwWITH_YA_1931:
		env.setBool(envWithYa1931, true)
	case strEnable:
		for _, a := range args {
			env.setBool(internEnv(a), true)
			javaEnableFlag(d, a, true)
			d.setVars.set(internStr(a), strYes)
			jm.instance.Platform.conf.set(env, internEnv(a), &d.setVars)
		}
	case kwDISABLE_DATA_VALIDATION:
		env.setBool(envValidateData, false)
	case strDisable:
		for _, a := range args {
			env.setBool(internEnv(a), false)
			javaEnableFlag(d, a, false)
			d.setVars.set(internStr(a), strNo)
			jm.instance.Platform.conf.set(env, internEnv(a), &d.setVars)
		}
	case kwGO_GRPC_GATEWAY_SWAGGER_SRCS:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: goGatewayV1Options, explicit: true})
	case kwGO_GRPC_GATEWAY_V2_OPENAPI_SRCS:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: goGatewayV2Options, explicit: true})
	case kwWITH_KOTLIN_GRPC:
		d.javaProtoArgs = append(d.javaProtoArgs, JavaProtoPlugin{kotlinGrpc: true})
	case kwJAVA_PROTO_PLUGIN:
		javaProtoPluginMacro(d, args)
	case kwPROTO_NAMESPACE:
		env.setString(envProtoNamespace, args[len(args)-1])
		d.addProtoInclude(args, true)
	case kwPROTO_ADDINCL:
		d.addProtoInclude(args, false)
	case kwEXPORT_YMAPS_PROTO:
		env.setString(envProtoNamespace, "maps/doc/proto")
		d.addProtoInclude([]string{"maps/doc/proto"}, true)
	case kwPROTOC_FATAL_WARNINGS:
		d.protocFlags = append(d.protocFlags, "--fatal_warnings")
	case kwONLY_TAGS, kwINCLUDE_TAGS, kwEXCLUDE_TAGS:
		if d.excludeTags == nil {
			d.excludeTags = map[string]bool{}
		}

		if name == kwONLY_TAGS {
			clear(d.excludeTags)
			d.onlyTags = true

			tag := "JAVA_PROTO"

			if u.proto {
				tag = env.string(envMODULE_TAG)
			}

			d.excludeTags[tag] = true

			if u.fbs {
				d.excludeTags["JAVA_FBS"] = true
			}
		}

		for _, a := range args {
			d.excludeTags[a] = name == kwEXCLUDE_TAGS
		}
	case kwTASKLET:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: strTaskletV1Api.string(), explicit: true})
	case kwSTYLE_RUFF, kwSTYLE_PYTHON:
		style := "ruff"

		if name == kwSTYLE_PYTHON {
			style = "black"
		}

		d.styleChecks = append(d.styleChecks, pythonStyleCheck(style, env))
	default:
		return false
	}

	return true
}

func javaProtoPluginMacro(d *JavaData, args []string) {
	if len(args) < 2 {
		return
	}

	d.javaProtoArgs = append(d.javaProtoArgs, JavaProtoPlugin{name: args[0], tool: args[1]})
}

func javaEnableFlag(d *JavaData, name string, on bool) {
	switch name {
	case "KOTLIN_PROTO":
		d.kotlinProto = on
	case "DISABLE_SCRIPTGEN":
		d.disableScriptgen = on
	}
}

func javaMacroIsAlias(jm *JavaModule, name STR) bool {
	switch name {
	case kwJAVA_RUNTIME_PEERDIR, kwJAVA_RUNTIME_EXCLUDE:
		return jm.unit.kind == juRunnable
	default:
		return jm.unit.kind == juTest
	}
}

type javaErrorProneInfo struct {
	version  string
	peerdir  string
	resource string
}

func javaErrorProne(jdk string) javaErrorProneInfo {
	switch jdk {
	case "11":
		return javaErrorProneInfo{"2.7.1", "build/platform/java/error_prone/2.7.1", "ERROR_PRONE_2_7_1"}
	case "17":
		return javaErrorProneInfo{"2.10.0", "build/platform/java/error_prone/2.10.0", "ERROR_PRONE_2_10_0"}
	case "21", "22", "23":
		return javaErrorProneInfo{"2.30.0", "build/platform/java/error_prone/2.30.0", "ERROR_PRONE_2_30_0"}
	case "24", "25", "26":
		return javaErrorProneInfo{"2.47.0", "build/platform/java/error_prone/2.47.0", "ERROR_PRONE_2_47_0"}
	}

	return javaErrorProneInfo{"2.7.1", "build/platform/java/error_prone", "ERROR_PRONE"}
}

func javaSetJDK(d *JavaData, env Environment, jdk string) {
	d.jdkReal = jdk

	env.setString(envJdkRealVersion, jdk)
	env.setString(envErrorProneVersion, javaErrorProne(jdk).version)
	env.setString(envKotlinJvmTarget, javaKotlinJVMTarget(jdk))
}

type javaCondVar struct {
	orig []string
	cur  []string
	base []string
}

func (v *javaCondVar) appendStmt(items ...string) {
	v.cur = append(v.cur, items...)
	v.orig = append(v.orig, items...)
}

func (v *javaCondVar) recalc(conds []string) {
	v.cur = append(append(append([]string(nil), v.base...), v.orig...), conds...)
}

func (d *JavaData) recalcKotlincOpts(jm *JavaModule) {
	var conds []string

	if jm.unit.sub == KindJavaFull && d.abiJar && d.withKotlin && !d.uberjar {
		conds = append(conds,
			"-Xplugin="+javaKotlinCompilerGlobal+"/plugins/kotlin-jvm-abi-gen-plugin.jar",
			"-P", "plugin:org.jetbrains.kotlin.jvm.abi:outputDir="+jm.path.rel().build().string()+"/abi",
			"-P", "plugin:org.jetbrains.kotlin.jvm.abi:removePrivateClasses=true")
	}

	if jm.unit.kind == juJar && d.withKotlin {
		if d.allopen {
			conds = append(conds, "-Xplugin="+javaKotlinCompilerGlobal+"/plugins/kotlin-allopen-plugin.jar")
		}

		if d.lombok {
			conds = append(conds, "-Xplugin="+javaKotlinCompilerGlobal+"/plugins/kotlin-lombok-plugin.jar")
		}

		if d.noarg {
			conds = append(conds, "-Xplugin="+javaKotlinCompilerGlobal+"/plugins/kotlin-noarg-plugin.jar")
		}

		if d.serialization {
			conds = append(conds, "-Xplugin="+javaKotlinCompilerGlobal+"/plugins/kotlin-serialization-plugin.jar")
		}
	}

	d.kotlincOpts.recalc(conds)
}

func (d *JavaData) recalcJavacOpts(jm *JavaModule) {
	var conds []string

	if jm.unit.compilable() && d.errorProne {
		conds = append(conds, strings.Fields(javaErrorProneBaseChecks)...)

		switch d.jdkReal {
		case "11", "17", "21", "22", "23", "24", "25", "26":
			conds = append(conds, strings.Fields(javaErrorProneAddOpens)...)
		}

		if javaErrorProne(d.jdkReal).version == "2.7.1" {
			conds = append(conds, strings.Fields(javaErrorProne271Forced)...)
		}
	}

	switch d.jdkReal {
	case "24", "25", "26":
		conds = append(conds, javaErrorProne2470Forced)
	}

	d.javacOpts.recalc(conds)
}

func javaSrcsSalt(spec JavaSrcsSpec) string {
	parts := []string{spec.srcDir}

	parts = append(parts, spec.globs...)
	parts = append(parts, spec.excludes...)

	if spec.packagePrefix != "" {
		parts = append(parts, spec.packagePrefix)
	}

	if spec.resources {
		parts = append(parts, "yes")
	} else {
		parts = append(parts, "no")
	}

	parts = append(parts, spec.files...)

	return strings.Join(parts, " ")
}

func (d *JavaData) addProtoInclude(args []string, exported bool) {
	includes, _, global := protoAddInclPaths(internAnys(args))

	for _, dir := range includes {
		d.protoAddIncl = append(d.protoAddIncl, javaAddIncl{dir: dir, global: exported || global})
	}
}

func (d *JavaData) rawExpand(s string) string {
	if strings.IndexByte(s, '$') < 0 && strings.IndexByte(s, '\\') < 0 {
		return s
	}

	if d.rawBindings == nil {
		d.rawBindings = make([]confMacroBinding, 0, len(d.rawVars)+len(javaConfGlobalBindings))

		for name, value := range d.rawVars {
			d.rawBindings = append(d.rawBindings, confMacroBinding{name: internStr(name), values: []ANY{internAny(value)}, bound: true})
		}

		d.rawBindings = append(d.rawBindings, javaConfGlobalBindings...)
	}

	return substMacroDeeply(s, d.rawBindings, Environment{}, nil, &substPolicy{makefile: true, emptyUnknown: true, literal: javaPathVar})
}

func javaPathVar(name string) bool {
	return javaPathVars[name]
}

func (d *JavaData) setRawVar(name, value string) {
	d.rawVars[name] = value

	if d.rawBindings == nil {
		return
	}

	id := internStr(name)

	for i := range d.rawBindings {
		if d.rawBindings[i].name == id {
			d.rawBindings[i].values = []ANY{internAny(value)}

			return
		}
	}

	d.rawBindings = slices.Insert(d.rawBindings, len(d.rawBindings)-len(javaConfGlobals), confMacroBinding{name: id, values: []ANY{internAny(value)}, bound: true})
}

func (d *JavaData) javaRawArgs(raw []string) []string {
	var out []string

	for _, r := range raw {
		out = append(out, strings.Fields(d.rawExpand(r))...)
	}

	return out
}

func malformedCommandReference(args []string) string {
	for _, arg := range args {
		for i := strings.Index(arg, "${"); i >= 0; {
			end := strings.IndexByte(arg[i+2:], '}')

			if end <= 0 || strings.ContainsFunc(arg[i+2:i+2+end], func(r rune) bool { return r < ' ' || r == '$' || r == '{' }) {
				return arg[i:]
			}

			next := strings.Index(arg[i+2+end:], "${")

			if next < 0 {
				break
			}

			i += 2 + end + next
		}
	}

	return ""
}

func javaRunProgramRawArgs(raw []string, env Environment, rawVars map[string]string) []string {
	active := map[string]bool{}

	var expand func(string) string

	expand = func(value string) string {
		return replaceJavaRunProgramRefs(value, func(reference string) string {
			name := strings.Trim(reference, "${}")

			if javaPathVars[name] {
				return reference
			}

			if active[name] {
				throwFmt("RUN_JAVA_PROGRAM: recursive variable expansion for %s", name)
			}

			value, ok := rawVars[name]

			if !ok {
				value, ok = javaConfGlobals[name]

				if !ok {
					value, ok = env.lookup(name)

					if !ok {
						if strings.HasPrefix(reference, "${") {
							return ""
						}

						return reference
					}
				}
			}

			active[name] = true
			value = expand(value)
			delete(active, name)

			return value
		})
	}

	var out []string

	for _, arg := range raw {
		out = append(out, strings.FieldsFunc(expand(arg), func(r rune) bool { return r == ' ' || r == ',' })...)
	}

	return out
}

func (d *JavaData) ruleList(jm *JavaModule, name STR) (*[]ANY, bool) {
	switch name {
	case kwDEPENDENCY_MANAGEMENT:
		return &d.dm, true
	case kwEXCLUDE:
		return &d.excludes, true
	case kwJAVA_RUNTIME_EXCLUDE, kwJAVA_TEST_EXCLUDE:
		if !javaMacroIsAlias(jm, name) {
			return nil, true
		}

		return &d.excludes, true
	}

	return nil, false
}

type javaRuleToken struct {
	name   string
	id     ENV
	simple bool
}

type javaRuleExpansion struct {
	tok  ANY
	kind EnvKind
	val  STR
}

func javaRuleTokenOf(tok ANY) javaRuleToken {
	s := tok.string()
	at := strings.IndexByte(s, '$')

	if at < 0 || strings.Count(s, "$") != 1 || at+1 >= len(s) || s[at+1] != '{' || at > 0 && s[at-1] == '\\' {
		return javaRuleToken{}
	}

	end := strings.IndexByte(s[at+2:], '}')

	if end <= 0 {
		return javaRuleToken{}
	}

	name := s[at+2 : at+2+end]

	return javaRuleToken{name: name, id: internedEnv(name), simple: true}
}

func (ctx *GenCtx) javaExpandRuleArgs(items []ANY, env Environment) []ANY {
	if !slices.ContainsFunc(items, func(a ANY) bool { return strHasDollar(a.str().any()) }) {
		return items
	}

	out := make([]ANY, 0, len(items))

	for _, item := range items {
		if !strHasDollar(item.str().any()) {
			out = append(out, item)

			continue
		}

		out = append(out, ctx.javaExpandRuleToken(item, env)...)
	}

	return out
}

func (ctx *GenCtx) javaExpandRuleToken(tok ANY, env Environment) []ANY {
	shape, ok := ctx.javaRuleTokens[tok]

	if !ok || shape.simple && shape.id == 0 {
		shape = javaRuleTokenOf(tok)
		ctx.javaRuleTokens[tok] = shape
	}

	if !shape.simple {
		return expandStmtTokens([]ANY{tok}, env)
	}

	key := javaRuleExpansion{tok: tok}

	if shape.id != 0 {
		key.kind, key.val = env.s.lookup(shape.id)
	}

	if words, ok := ctx.javaRuleExpansions[key]; ok {
		if key.kind != envAbsent {
			env.recordResourceUse(shape.id)
		}

		return words
	}

	words := expandStmtTokens([]ANY{tok}, env)

	if key.kind == envVFS || !strHasDollar(key.val.any()) {
		ctx.javaRuleExpansions[key] = slices.Clip(words)
	}

	return words
}

func javaAppendResolved(dst, src, expanded []ANY) []ANY {
	if len(expanded) == 0 {
		return dst
	}

	if &expanded[0] == &src[0] {
		return append(dst, expanded...)
	}

	for _, a := range expanded {
		if !strHasDollar(a.str().any()) {
			dst = append(dst, a)

			continue
		}

		for _, s := range javaDropUnresolved([]string{a.string()}) {
			dst = append(dst, internStr(s).any())
		}
	}

	return dst
}

func javaDropUnresolved(args []string) []string {
	if !slices.ContainsFunc(args, func(arg string) bool { return strings.IndexByte(arg, '$') >= 0 }) {
		return args
	}

	out := make([]string, 0, len(args))

	for _, arg := range args {
		a := strings.ReplaceAll(arg, `${"$"}`, "$")

		for {
			start := strings.Index(a, "${")

			if start < 0 {
				break
			}

			end := strings.IndexByte(a[start:], '}')

			if end < 0 {
				break
			}

			a = a[:start] + a[start+end+1:]
		}

		if a != "" {
			out = append(out, a)
		}
	}

	return out
}

func replaceJavaRunProgramRefs(value string, fn func(string) string) string {
	rs, re, ok := javaRunProgramRefNext(value, 0)

	if !ok {
		return value
	}

	var b strings.Builder

	from := 0

	for ok {
		b.WriteString(value[from:rs])
		b.WriteString(fn(value[rs:re]))
		from = re
		rs, re, ok = javaRunProgramRefNext(value, from)
	}

	b.WriteString(value[from:])

	return b.String()
}
