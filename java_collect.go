package main

import (
	"path"
	"strings"
)

var javaKotlinAllowedVersions = []string{
	"kotlin-allopen", "kotlin-allopen-compiler-plugin", "kotlin-annotation-processing", "kotlin-compiler",
	"kotlin-compiler-embeddable", "kotlin-daemon-client", "kotlin-daemon-embeddable", "kotlin-gradle-plugin-api",
	"kotlin-gradle-plugin-model", "kotlin-lombok-compiler-plugin", "kotlin-main-kts", "kotlin-maven-allopen",
	"kotlin-maven-lombok", "kotlin-maven-noarg", "kotlin-maven-serialization", "kotlin-noarg-compiler-plugin",
	"kotlin-osgi-bundle", "kotlin-reflect", "kotlin-scripting-common", "kotlin-scripting-compiler-embeddable",
	"kotlin-scripting-compiler-impl-embeddable", "kotlin-scripting-ide-services", "kotlin-scripting-jsr223",
	"kotlin-scripting-jvm", "kotlin-scripting-jvm-host", "kotlin-script-runtime", "kotlin-script-util",
	"kotlin-serialization-compiler-plugin", "kotlin-serialization-compiler-plugin-embeddable", "kotlin-stdlib",
	"kotlin-stdlib-common", "kotlin-stdlib-jdk7", "kotlin-stdlib-jdk8", "kotlin-stdlib-js", "kotlin-test",
	"kotlin-test-annotations-common", "kotlin-test-common", "kotlin-test-js", "kotlin-test-junit",
	"kotlin-test-junit5", "kotlin-test-testng",
}

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

const (
	javaKotlinVersion          = "2.3.10"
	javaProtoRuntimeVersion    = "4.33.0"
	javaProtoCommonVersion     = "2.63.2"
	javaGrpcVersion            = "1.51.0"
	javaKotlinGrpcVersion      = "1.3.1"
	javaFlatbuffersVersion     = "24.3.25"
	javaKotlinProtoPeersMarker = "${KOTLIN_PROTO_PEERS}"
	javaKotlinCompilerGlobal   = "${KOTLIN_COMPILER_RESOURCE_GLOBAL}"
	javaErrorProneBaseChecks   = "-Xep:FunctionalInterfaceMethodChanged:WARN -Xep:ReturnValueIgnored:WARN"
	javaErrorProneAddOpens     = "-J--add-opens=jdk.compiler/com.sun.tools.javac.code=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.comp=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.file=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.main=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.model=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.processing=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.tree=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.util=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.jvm=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.api=ALL-UNNAMED -J--add-opens=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED"
	javaErrorProne271Forced    = "-Xep:InlineMeInliner:OFF -Xep:SameNameButDifferent:OFF"
	javaErrorProne2470Forced   = "--should-stop=ifError=FLOW"
)

type javaConfGlobal struct {
	name  ENV
	value STR
}

func (u *javaUnit) compilable() bool {
	return u.kind == juJar || u.kind == juRunnable && !u.contribRunnable || u.kind == juTest
}

func javaConfPrologue(jm *JavaModule, d *JavaData) {
	u := jm.unit

	if u.compilable() {
		d.dm = append(d.dm,
			"contrib/java/com/google/flatbuffers/flatbuffers-java/"+javaFlatbuffersVersion,
			"contrib/java/com/google/protobuf/protobuf-java/"+javaProtoRuntimeVersion,
			"contrib/java/com/google/protobuf/protobuf-javalite/"+javaProtoRuntimeVersion,
			"contrib/java/com/google/protobuf/protobuf-java-util/"+javaProtoRuntimeVersion,
			"contrib/java/com/google/protobuf/protobuf-kotlin/"+javaProtoRuntimeVersion,
		)

		for _, lib := range javaKotlinAllowedVersions {
			d.dm = append(d.dm, "contrib/java/org/jetbrains/kotlin/"+lib+"/"+javaKotlinVersion)
		}
	}

	if u.kind == juJar {
		d.dm = append(d.dm, "contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/"+javaKotlinVersion)
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "build/platform/java/jdk"})
	}

	if u.kind == juExternal {
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "build/platform/java/jdk"})
	}

	switch {
	case u.kind == juTest && jm.stmt == tokJtest:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "devtools/junit-runner"}, JavaPeerSpec{dir: "devtools/jtest-annotations/junit4"})
		d.dm = append(d.dm, "contrib/java/com/google/code/gson/gson/2.10.1", "contrib/java/com/beust/jcommander/1.72", "contrib/java/junit/junit/4.12")
	case u.kind == juTest && jm.stmt == tokJtestFor:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: d.unittestDir}, JavaPeerSpec{dir: "devtools/junit-runner"})
		d.dm = append(d.dm, "contrib/java/com/google/code/gson/gson/2.8.6", "contrib/java/com/beust/jcommander/1.72", "contrib/java/junit/junit/4.12")
	case u.kind == juTest && jm.stmt == tokJunit5:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "devtools/jtest-annotations/junit5"})
	case u.kind == juTest && jm.stmt == tokJunit6:
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "devtools/jtest-annotations/junit6"})
	case u.kind == juJar && jm.stmt == tokJtestFor:
		d.test.depends = append(d.test.depends, "contrib/java/org/sonarsource/scanner/cli/sonar-scanner-cli/2.8")
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "devtools/junit-runner"}, JavaPeerSpec{dir: "build/platform/java/jacoco-agent"}, JavaPeerSpec{dir: d.unittestDir})
	}

	if u.proto {
		d.javaProtobufPeers = []string{"contrib/java/com/google/protobuf/protobuf-java/" + javaProtoRuntimeVersion, javaKotlinProtoPeersMarker}
	}
	if u.fbs {
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "contrib/java/com/google/flatbuffers/flatbuffers-java/" + javaFlatbuffersVersion})
	}
}

func (d *JavaData) kotlinProtoPeers() []string {
	if d.kotlinProto {
		return []string{"contrib/java/com/google/protobuf/protobuf-kotlin/" + javaProtoRuntimeVersion}
	}

	return nil
}

func (d *JavaData) expandJavaProtobufPeers() {
	var out []string

	for _, p := range d.javaProtobufPeers {
		if p == javaKotlinProtoPeersMarker {
			out = append(out, d.kotlinProtoPeers()...)

			continue
		}

		out = append(out, p)
	}

	d.javaProtobufPeers = out
}

func collectJavaData(ctx *GenCtx, jm *JavaModule, stmts []Stmt, env Environment) *JavaData {
	d := &JavaData{jdkReal: ctx.javaDefaultJDK(), env: env, lintLevel: "extended", abiJar: true, srcDirs: []VFS{jm.path}}

	d.javacOpts.base = []string{"-J-XX:ActiveProcessorCount=4"}
	d.javacOpts.cur = d.javacOpts.base

	javaSetJDK(d, env, d.jdkReal)
	env.setString(envMODULE_TAG, jm.unit.tag)

	if jm.unit.proto {
		env.setBool(envJavaProto, true)
	}
	if jm.unit.fbs {
		env.setBool(internEnv("JAVA_FBS"), true)
	}

	for _, st := range stmts {
		if m, ok := st.(*ModuleStmt); ok {
			if m.Schema {
				env.setString(envMODULE_TAG, protoModuleTag(LangJava, true))
			}

			javaModuleArgs(jm, d, expandStmtTokens(m.Args, env))

			break
		}
	}

	javaConfPrologue(jm, d)
	collectJavaStmts(ctx, jm, d, stmts, env)

	for _, value := range strings.Fields(expandStmtToken(env.string(envJavaIgnoreClasspathClash), env)) {
		if value != `""` && value != "''" {
			d.test.ignoreClasspathClash = append(d.test.ignoreClasspathClash, strings.ReplaceAll(value, `${"$"}`, "$"))
		}
	}

	if jm.unit.proto {
		d.expandJavaProtobufPeers()
	}
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

func collectJavaStmts(ctx *GenCtx, jm *JavaModule, d *JavaData, stmts []Stmt, env Environment) {
	for _, s := range stmts {
		switch v := s.(type) {
		case *PeerdirStmt:
			if jm.unit.ignorePeerdir {
				continue
			}

			for _, p := range expandStmtTokens(v.Paths, env) {
				ps := p.string()

				if ps == "ADDINCL" || ps == "GLOBAL" || strings.Contains(ps, "${") {
					continue
				}

				d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: ps, explicit: true})
			}
		case *SetStmt:
			env.setFromString(v.NameEnv, expandAssignedScalarVarRef(v.Value, env))

			if d.rawVars == nil {
				d.rawVars = map[string]string{}
			}

			d.rawVars[v.Name] = d.rawExpand(v.Value)
		case *DefaultVarStmt:
			env.setDefaultString(v.NameEnv, expandAssignedScalarVarRef(v.Value, env))

			if _, ok := d.rawVars[v.VarName]; !ok {
				if d.rawVars == nil {
					d.rawVars = map[string]string{}
				}

				d.rawVars[v.VarName] = d.rawExpand(v.Value)
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

				switch {
				case jm.unit.proto && (extIsProto(ss) || extIsGztproto(ss)):
					d.protoSrcs = append(d.protoSrcs, ss)
				case jm.unit.proto && strings.HasSuffix(ss, ".ev"):
					d.evSrcs = append(d.evSrcs, ss)
				default:
					d.srcs = append(d.srcs, ss)
				}
			}
		case *RunPythonStmt:
			d.runPython = append(d.runPython, expandRunPythonStmt(v, env))
		case *FromSandboxStmt:
			expanded := *v

			expanded.ResourceId = expandStmtTokenAny(v.ResourceId, env)
			expanded.OUTFiles = expandStmtTokens(v.OUTFiles, env)
			expanded.OUTNoAutoFiles = expandStmtTokens(v.OUTNoAutoFiles, env)
			expanded.Renames = expandStmtTokens(v.Renames, env)
			expanded.OutputIncludes = expandStmtTokens(v.OutputIncludes, env)

			d.fromSandboxes = append(d.fromSandboxes, &expanded)
		case *SrcDirStmt:
			for _, dir := range expandStmtTokens(v.Dirs, env) {
				d.srcDirs = append(d.srcDirs, dirKey(dir.string()).source())
			}
		case *AddInclStmt:
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
			raw := v.Args

			if v.LexicalArgs != nil {
				raw = v.LexicalArgs
			}

			rawArgs := anyStrs(raw)

			var args []string

			if v.Raw.string() == "RUN_JAVA_PROGRAM" {
				args = splitJavaCommandWords(expandStmtToken(strings.Join(rawArgs, " "), env))
			} else {
				args = anyStrs(expandStmtTokens(v.Args, env))
			}

			stmt := *v

			stmt.Args = internAnys(args)
			d.recurses = appendConfigurationRecurses(d.recurses, jm.dir, stmt, jm.instance.Platform)
			applyJavaMacro(ctx, jm, d, v.Raw.string(), args, rawArgs, env)
		}
	}
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

func applyJavaMacro(ctx *GenCtx, jm *JavaModule, d *JavaData, name string, args, raw []string, env Environment) {
	u := jm.unit
	arg0 := ""

	if name == "FILES" {
		for _, file := range args {
			if strings.HasPrefix(file, "${ARCADIA_BUILD_ROOT}") || strings.HasPrefix(file, "$(B)/") {
				continue
			}

			d.files = append(d.files, file)
		}

		return
	}

	args = javaDropUnresolved(args)

	if len(args) > 0 {
		arg0 = args[0]
	}

	switch name {
	case "FLATC_FLAGS":
		d.flatcFlags = append(d.flatcFlags, internAnys(args)...)
	case "SET_APPEND":
		if len(args) == 0 {
			return
		}

		value := strings.Join(args[1:], " ")

		if prev, ok := env.lookup(args[0]); ok && prev != "" {
			value = prev + " " + value
		}

		env.setString(internEnv(args[0]), value)

		if d.rawVars == nil {
			d.rawVars = map[string]string{}
		}

		rawValue := d.rawExpand(strings.Join(raw[1:], " "))

		if prev := d.rawVars[args[0]]; prev != "" {
			rawValue = prev + " " + rawValue
		}

		d.rawVars[args[0]] = rawValue

		if args[0] == "_PROTOC_FLAGS" {
			d.protocFlags = append(d.protocFlags, args[1:]...)
		}
	case "JDK_VERSION", "DEFAULT_JDK_VERSION":
		javaSetJDK(d, env, arg0)

		d.hasPerModuleJDK = true
		d.recalcJavacOpts(jm)
	case "WITH_KOTLIN":
		d.withKotlin = true
		d.recalcKotlincOpts(jm)

		env.setBool(envWithKotlinValue, true)
		env.setString(envKotlinDefaultGlob, "**/*.kt")
	case "WITH_KAPT":
		d.withKapt = true

		if arg0 == "GENERATE_KOTLIN" {
			d.kaptAPOptions = append(d.kaptAPOptions, "kapt.kotlin.generated="+jm.path.rel().build().string()+"/kapt_gen_src")
		}
	case "KAPT_OPTS":
		for _, a := range args {
			d.kaptOpts = append(d.kaptOpts, "-P", a)
		}
	case "KAPT_ANNOTATION_PROCESSOR":
		d.kaptProcessors = append(d.kaptProcessors, args...)
	case "KAPT_ANNOTATION_PROCESSOR_CLASSPATH":
		d.kaptAPClasspath = append(d.kaptAPClasspath, args...)

		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}
	case "KAPT_ANNOTATION_PROCESSOR_OPTIONS":
		d.kaptAPOptions = append(d.kaptAPOptions, args...)
	case "KOTLINC_FLAGS":
		d.kotlincOpts.appendStmt(args...)
	case "WITH_KOTLINC_ALLOPEN":
		d.kotlincOpts.appendStmt(javaKotlinPluginOpts("allopen", args)...)
		d.allopen = true
		d.recalcKotlincOpts(jm)
	case "WITH_KOTLINC_LOMBOK":
		d.kotlincOpts.appendStmt(javaKotlinPluginOpts("lombok", args)...)
		d.lombok = true
		d.recalcKotlincOpts(jm)
	case "WITH_KOTLINC_NOARG":
		d.kotlincOpts.appendStmt(javaKotlinPluginOpts("noarg", args)...)
		d.noarg = true
		d.recalcKotlincOpts(jm)
	case "WITH_KOTLINC_SERIALIZATION":
		d.serialization = true
		d.recalcKotlincOpts(jm)
	case "WITH_KOTLINC_DETEKT":
		for _, o := range args {
			d.kotlincOpts.appendStmt("-P", "plugin:detekt-compiler-plugin:"+o)
		}

		d.detekt = true
		d.recalcKotlincOpts(jm)
	case "ENABLE_KOTLIN_ABI_JAR":
		d.abiJar = true
		d.recalcKotlincOpts(jm)
	case "JAVAC_FLAGS":
		d.javacOpts.appendStmt(args...)
	case "ENABLE_PREVIEW":
		d.jvmArgs = append(d.jvmArgs, "--enable-preview")
		d.javacOpts.appendStmt("--enable-preview", "--release", d.jdkReal)
		d.enablePreview = true
	case "USE_ERROR_PRONE":
		d.errorProne = true
		d.recalcJavacOpts(jm)
	case "UBERJAR":
		d.uberjar = true
		d.recalcKotlincOpts(jm)
	case "UBERJAR_HIDING_PREFIX":
		d.uberjarOpts = append(d.uberjarOpts, "--shade-prefix", arg0)
	case "UBERJAR_HIDE_EXCLUDE_PATTERN":
		for _, a := range args {
			d.uberjarOpts = append(d.uberjarOpts, "--shade-exclude", a)
		}
	case "UBERJAR_HIDE_INCLUDE_PATTERN":
		for _, a := range args {
			d.uberjarOpts = append(d.uberjarOpts, "--shade-include", a)
		}
	case "UBERJAR_PATH_EXCLUDE_PREFIX":
		for _, a := range args {
			d.uberjarOpts = append(d.uberjarOpts, "--uber-exclude", a)
		}
	case "UBERJAR_MANIFEST_TRANSFORMER_MAIN":
		d.uberjarOpts = append(d.uberjarOpts, "--manifest-main", arg0)
	case "UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE":
		if len(args) > 1 {
			d.uberjarOpts = append(d.uberjarOpts, "--manifest-attribute", arg0+":'"+strings.Join(args[1:], " ")+"'")
		}
	case "UBERJAR_APPENDING_TRANSFORMER":
		for _, a := range args {
			d.uberjarOpts = append(d.uberjarOpts, "--append-transformer", a)
		}
	case "UBERJAR_SERVICES_RESOURCE_TRANSFORMER":
		d.uberjarOpts = append(d.uberjarOpts, "--service-transformer")
	case "DEPENDENCY_MANAGEMENT":
		d.dm = append(d.dm, args...)
	case "EXCLUDE", "JAVA_RUNTIME_EXCLUDE", "JAVA_TEST_EXCLUDE":
		if name != "EXCLUDE" && !javaMacroIsAlias(jm, name) {
			return
		}

		d.excludes = append(d.excludes, args...)
	case "JAVA_DEPENDENCIES_CONFIGURATION":
	case "JAR_RESOURCE":
		d.jarResource = arg0
	case "SRC_RESOURCE":
		d.srcResource = arg0
	case "LOCAL_JAR":
		d.localJar = arg0
	case "LOCAL_SOURCES_JAR":
		d.localSourcesJar = arg0
	case "JAVA_TEST_PEERDIR", "JAVA_RUNTIME_PEERDIR":
		if !javaMacroIsAlias(jm, name) {
			return
		}

		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}
	case "JAVA_API_PEERDIR":
		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}
	case "JAVA_IMPL_PEERDIR":
		for _, a := range args {
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: a})
		}

		d.implPeers = append(d.implPeers, args...)
	case "JUNIT_TESTS_JAR":
		if u.kind == juTest && len(args) > 0 {
			d.unittestDir = arg0
			d.unittestMod = path.Join(args...)
			d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: arg0})
		}
	case "AUTO_SERVICE":
		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "contrib/java/com/google/auto/service/auto-service-annotations/" + arg0})
	case "USE_ANNOTATION_PROCESSOR":
	case "ANNOTATION_PROCESSOR", "JAR_ANNOTATION_PROCESSOR":
		d.annotationProcs = append(d.annotationProcs, args...)
	case "JAR_MAIN_CLASS":
		d.jarMainClass = arg0
	case "JVM_ARGS":
		d.jvmArgs = append(d.jvmArgs, args...)
	case "SIZE":
		d.test.size = strings.ToLower(arg0)
	case "TIMEOUT":
		d.test.timeout = arg0
	case "TEST_CWD":
		d.test.cwd = arg0
	case "TAG":
		d.test.tags = append(d.test.tags, args...)
	case "REQUIREMENTS":
		d.test.requirements = append(d.test.requirements, args...)
	case "FORK_TESTS":
		d.test.forkMode = "tests"
	case "FORK_SUBTESTS":
		d.test.forkMode = "subtests"
	case "SPLIT_FACTOR":
		d.test.splitFactor = arg0
	case "EXPERIMENTAL_FORK":
		d.test.experimentalFork = true
	case "SYSTEM_PROPERTIES":
		d.test.systemProperties = append(d.test.systemProperties, args...)
	case "TEST_JAVA_CLASSPATH_CMD_TYPE":
		d.test.classpathCmdType = arg0
	case "USE_RECIPE":
		d.test.recipes = append(d.test.recipes, strings.Join(args, " "))
	case "DEPENDS":
		d.test.depends = append(d.test.depends, args...)
	case "DATA":
		d.test.data = append(d.test.data, args...)
	case "DATA_FILES":
		for _, a := range args {
			d.test.data = append(d.test.data, "arcadia/"+a)
		}
	case "JAVA_SRCS", "FULL_JAVA_SRCS":
		if u.kind == juJar && !u.proto {
			spec := parseJavaSrcs(args)
			rawSpec := parseJavaSrcs(d.javaRawArgs(raw))

			spec.salt = javaSrcsSalt(rawSpec)
			spec.varSrcDir = strings.HasPrefix(rawSpec.srcDir, "$") && !strings.HasPrefix(rawSpec.srcDir, "$S")

			d.javaSrcs = append(d.javaSrcs, spec)
		}
	case "DEFAULT_JAVA_SRCS_LAYOUT", "DEFAULT_JUNIT_JAVA_SRCS_LAYOUT":
		if u.kind != juJar || u.proto {
			return
		}

		srcDir, resDir := "src/main/java", "src/main/resources"

		if name == "DEFAULT_JUNIT_JAVA_SRCS_LAYOUT" {
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
	case "RUN_JAVA_PROGRAM":
		d.runJavaPrograms = append(d.runJavaPrograms, JavaRunProgram{args: args, raw: d.javaRawArgs(raw)})
	case "GENERATE_SCRIPT":
		d.generateScripts = append(d.generateScripts, args)
	case "WITH_JDK":
		d.withJDK = true
	case "JAVA_EXTERNAL_DEPENDENCIES":
		d.externalDeps = append(d.externalDeps, args...)
	case "ADD_DLLS_TO_JAR":
		d.addDLLs = true
	case "CHECK_JAVA_DEPS":
		d.checkJavaDeps = strings.ToLower(arg0)
	case "JAVA_IGNORE_CLASSPATH_CLASH_FOR":
		property := []string{envJavaIgnoreClasspathClash.string()}

		applyJavaMacro(ctx, jm, d, "SET_APPEND", append(property, args...), append([]string{envJavaIgnoreClasspathClash.string()}, raw...), env)
	case "ENV":
		d.testEnv = append(d.testEnv, splitShellWords(`"`+strings.Join(raw, " ")+`"`)...)
	case "LINT":
		d.lintLevel = arg0
	case "NO_LINT":
		d.noLint = "none"

		if arg0 == "ktlint" {
			d.noLint = "ktlint"
		}
	case "COLLECT_KOTLIN_LINT_SRCS":
		d.collectKotlinLint = args
	case "STYLE_DETEKT":
		if d.noLint != "none" {
			d.customLints = append(d.customLints, javaCustomLint{linter: javaDetekt, configTypes: javaMacroKeyword(args, "CONFIG_TYPE")})
		}
	case "USE_KTLINT_OLD":
		d.ktlintOld = true
	case "WITH_YA_1931":
		env.setBool(internEnv("_WITH_YA_1931"), true)
	case "NO_YMAKE_PYTHON3":
		d.noYmakePython3 = true
	case "ENABLE":
		for _, a := range args {
			env.setBool(internEnv(a), true)
			javaEnableFlag(d, a, true)
		}
	case "DISABLE_DATA_VALIDATION":
		env.setBool(envValidateData, false)
	case "DISABLE":
		for _, a := range args {
			env.setBool(internEnv(a), false)
			javaEnableFlag(d, a, false)
		}
	case "GRPC":
		d.javaProtoArgs = append(d.javaProtoArgs, JavaProtoPlugin{name: "grpc_java", tool: "build/platform/java/grpc"})
		d.javaProtobufPeers = append(d.javaProtobufPeers, "contrib/java/io/grpc/grpc-stub/"+javaGrpcVersion, "contrib/java/io/grpc/grpc-protobuf/"+javaGrpcVersion)
		d.expandJavaProtobufPeers()
		d.javaProtobufPeers = append(d.javaProtobufPeers, "contrib/java/javax/annotation/javax.annotation-api/1.3.1")
	case "WITH_KOTLIN_GRPC":
		d.kotlinProto = true
		d.javaProtoArgs = append(d.javaProtoArgs, JavaProtoPlugin{kotlinGrpc: true})

		env.setBool(envKotlinProto, true)

		d.peerStmts = append(d.peerStmts, JavaPeerSpec{dir: "build/platform/java/kotlin_grpc"})
		d.javaProtobufPeers = append(d.javaProtobufPeers, "contrib/java/io/grpc/grpc-kotlin-stub/"+javaKotlinGrpcVersion)
		d.expandJavaProtobufPeers()
		d.javaProtobufPeers = append(d.javaProtobufPeers, "contrib/java/com/google/protobuf/protobuf-kotlin/"+javaProtoRuntimeVersion)
		d.expandJavaProtobufPeers()
	case "JAVA_PROTO_PLUGIN":
		javaProtoPluginMacro(d, args)
	case "ALICE_CAPABILITY":
		javaProtoPluginMacro(d, []string{"alice_capability_java", "alice/library/go/java_marker_interface_protoc_plugin", "DEPS", "alice/protos/endpoint/java_interface", "alice/megamind/protos/scenarios/java_interface"})
	case "ALICE_TYPED_CALLBACK":
		javaProtoPluginMacro(d, []string{"alice_typed_callback_java", "alice/library/go/java_marker_interface_protoc_plugin", "DEPS", "alice/protos/api/typed_callbacks/java_marker_interface"})
	case "PROTO_NAMESPACE":
		javaProtoNamespace(d, args[len(args)-1])
	case "EXPORT_YMAPS_PROTO":
		javaProtoNamespace(d, "maps/doc/proto")
	case "PROTOC_FATAL_WARNINGS":
		d.protocFlags = append(d.protocFlags, "--fatal_warnings")
	case "ONLY_TAGS", "INCLUDE_TAGS", "EXCLUDE_TAGS":
		if d.excludeTags == nil {
			d.excludeTags = map[string]bool{}
		}

		if name == "ONLY_TAGS" {
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
			d.excludeTags[a] = name == "EXCLUDE_TAGS"
		}
	case "USE_COMMON_GOOGLE_APIS":
		d.commonGoogleAPIs = true
	}
}

func javaProtoPluginMacro(d *JavaData, args []string) {
	if len(args) < 2 {
		return
	}

	d.javaProtoArgs = append(d.javaProtoArgs, JavaProtoPlugin{name: args[0], tool: args[1]})

	for i := 2; i < len(args); i++ {
		if args[i] == "DEPS" {
			continue
		}

		d.javaProtobufPeers = append(d.javaProtobufPeers, args[i])
	}

	d.expandJavaProtobufPeers()
}

func javaEnableFlag(d *JavaData, name string, on bool) {
	switch name {
	case "SOURCES_JAR":
		d.sourcesJar = on
	case "KOTLIN_PROTO":
		d.kotlinProto = on
	case "DISABLE_SCRIPTGEN":
		d.disableScriptgen = on
	}
}

func javaMacroIsAlias(jm *JavaModule, name string) bool {
	switch name {
	case "JAVA_RUNTIME_PEERDIR", "JAVA_RUNTIME_EXCLUDE":
		return jm.unit.kind == juRunnable
	default:
		return jm.unit.kind == juTest
	}
}

func (jm *JavaModule) varPeers() []string {
	d, u := jm.data, jm.unit
	jdkPeer := "build/platform/java/jdk/jdk" + d.jdkReal

	var out []string

	if u.jni {
		out = append(out, "build/platform/java/jdk", jdkPeer)
	}

	if !d.noYmakePython3 {
		out = append(out, "build/platform/python/ymake_python3")
	}

	switch u.kind {
	case juContrib:
		if d.jarResource == "" && d.localJar == "" {
			out = append(out, "build/platform/java/jdk", jdkPeer)
		}

		if d.srcResource == "" && d.localSourcesJar == "" {
			out = append(out, "build/platform/java/jdk", jdkPeer)
		}
	case juJar:
		out = append(out, jdkPeer)

		switch {
		case jm.stmt == tokJunit5:
			out = append(out, "devtools/junit5-runner", "build/platform/java/jacoco-agent")
		case jm.stmt == tokJunit6:
			out = append(out, "devtools/junit6-runner", "build/platform/java/jacoco-agent")
		case jm.stmt == tokJtest:
			out = append(out, "build/platform/java/jacoco-agent")
		}

		if d.withKotlin {
			out = append(out, "build/platform/java/kotlin", "contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8")
		}

		if d.uberjar {
			out = append(out, "build/platform/java/uberjar", "build/platform/java/uberjar/uberjar"+d.jdkReal)
		}

		if d.errorProne {
			out = append(out, javaErrorProne(d.jdkReal).peerdir)
		}

		switch jm.stmt {
		case tokJunit5:
			out = append(out, "devtools/jtest-annotations/junit5")
		case tokJunit6:
			out = append(out, "devtools/jtest-annotations/junit6")
		case tokJtest:
			out = append(out, "devtools/jtest-annotations/junit4")
		}
	case juRunnable:
		if !u.contribRunnable && !d.disableScriptgen {
			out = append(out, "build/platform/java/scriptgen")
		}
	case juExternal:
		out = append(out, jdkPeer, "build/platform/java/jdk/jdk17")

		if u.proto {
			out = append(out, d.javaProtobufPeers...)

			if d.commonGoogleAPIs {
				out = append(out, "contrib/java/com/google/api/grpc/proto-google-common-protos/"+javaProtoCommonVersion)
			}
		}

		if d.kotlinProto {
			out = append(out, "build/platform/java/kotlin", "contrib/java/org/jetbrains/kotlin/kotlin-stdlib-jdk8/"+javaKotlinVersion)
		}
	}

	return out
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

func javaProtoNamespace(d *JavaData, ns string) {
	d.protoNamespace = ns
	d.protoAddIncl = append(d.protoAddIncl, javaAddIncl{dir: source(ns), global: true})
}

func (d *JavaData) rawExpand(s string) string {
	for i := 0; i < 8 && strings.Contains(s, "${"); i++ {
		var sb strings.Builder

		rest := s

		for {
			start := strings.Index(rest, "${")

			if start < 0 {
				sb.WriteString(rest)

				break
			}

			end := strings.IndexByte(rest[start:], '}')

			if end < 0 {
				sb.WriteString(rest)

				break
			}

			name := rest[start+2 : start+end]

			sb.WriteString(rest[:start])

			switch value, ok := d.rawVars[name]; {
			case ok:
				sb.WriteString(value)
			case javaPathVars[name]:
				sb.WriteString(rest[start : start+end+1])
			default:
				if g, ok := javaConfGlobals[name]; ok {
					sb.WriteString(g)
				}
			}

			rest = rest[start+end+1:]
		}

		if sb.String() == s {
			break
		}

		s = sb.String()
	}

	return s
}

func (d *JavaData) javaRawArgs(raw []string) []string {
	var out []string

	for _, r := range raw {
		out = append(out, strings.Fields(d.rawExpand(r))...)
	}

	return out
}

func javaDropUnresolved(args []string) []string {
	out := args[:0:0]

	for _, a := range args {
		a = strings.ReplaceAll(a, "${$}", "$")

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
