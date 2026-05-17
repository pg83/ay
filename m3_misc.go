package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// m3_misc.go — emitters for the small-kind nodes:
//   R5  — ragel5 two-step (.rl → .rl.tmp → .rl5.cpp)
//   JV  — ANTLR4 grammar Java invocation
//   CF  — CONFIGURE_FILE (.cpp.in/.c.in → .cpp/.c)
//   BI  — CREATE_BUILDINFO_FOR
//   PR  — RUN_PROGRAM code-generator invocation
// Reference: /home/pg/monorepo/yatool_orig/sg2.json.

// ─── R5 ──────────────────────────────────────────────────────────────────────

// EmitR5 emits an R5 node (two cmds: ragel5 → .tmp; rlgen-cd → .rl5.cpp).
// tmpPath = $(B)/<modulePath>/<srcRel>.tmp
// cppPath = $(B)/<modulePath>/<srcRel without .rl>.rl5.cpp
// Returns (R5 NodeRef, tmpPath, cppPath).
func EmitR5(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath VFS,
	rlgenCdBinPath VFS,
	emit Emitter,
) (NodeRef, VFS, VFS) {
	srcVFS := Source(instance.Path + "/" + srcRel)
	tmpVFS := Build(instance.Path + "/" + srcRel + ".tmp")
	// Output: strip .rl suffix, append .rl5.cpp.
	cppVFS := Build(instance.Path + "/" + strings.TrimSuffix(srcRel, ".rl") + ".rl5.cpp")

	// Pre-materialise the three .String() forms — tmpVFS is referenced
	// in both cmd_args lines, so .String() it once instead of twice.
	tmpPath := tmpVFS.String()
	cppPath := cppVFS.String()
	srcPath := srcVFS.String()

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	cmd0 := Cmd{
		CmdArgs: []string{
			ragel5BinPath.String(),
			"-o",
			tmpPath,
			srcPath,
		},
		Env: env,
	}
	cmd1 := Cmd{
		CmdArgs: []string{
			rlgenCdBinPath.String(),
			"-G2",
			"-o",
			cppPath,
			tmpPath,
		},
		Env: env,
	}

	// inputs = [ragel5 binary, rlgen-cd binary, source .rl]
	inputs := []VFS{ragel5BinPath, rlgenCdBinPath, srcVFS}

	// deps / foreign_deps.tool = both host tool LD refs (in order).
	depRefs := make([]NodeRef, 0, 2)
	if ragel5LD != (NodeRef{}) {
		depRefs = append(depRefs, ragel5LD)
	}
	if rlgenCdLD != (NodeRef{}) {
		depRefs = append(depRefs, rlgenCdLD)
	}

	node := &Node{
		Cmds:    []Cmd{cmd0, cmd1},
		Env:     env,
		Inputs:  inputs,
		Outputs: []VFS{tmpVFS, cppVFS},
		KV: map[string]string{
			"p":  "R5",
			"pc": "yellow",
		},
		// R5-specific: tags=["tool"] unconditionally — every R5 is a
		// host-toolchain ragel5 call regardless of caller axis. Not
		// derived from instance.Platform.Tags.
		Tags: []string{"tool"},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: map[string][]NodeRef{"tool": depRefs},
	}

	return emit.Emit(node), tmpVFS, cppVFS
}

// ─── JV ──────────────────────────────────────────────────────────────────────

// jdkResourcePath is the literal JDK17 resource path used in the reference
// sg2.json.  The hash suffix (564746473) is the resource bundle ID and
// is pinned byte-exact for the M3 closure.
const jdkResourcePath = "$(JDK17-564746473)/bin/java"

// antlr4JarVFS is the source-relative VFS path to the ANTLR4 jar.
var antlr4JarVFS = Source("contrib/java/antlr/antlr4/antlr.jar")

// antlr4JarPath is the legacy string form (used in cmd_args). Equal
// to antlr4JarVFS.String().
var antlr4JarPath = antlr4JarVFS.String()

// stdout2stderr is the wrapper script that redirects antlr4's stdout
// to stderr (required so the build system captures diagnostic output
// correctly).
var stdout2stderrVFS = Source("build/scripts/stdout2stderr.py")
var stdout2stderrPath = stdout2stderrVFS.String()

// EmitJV emits a JV node for a single RUN_ANTLR4_CPP grammar (.g4
// relative to module dir). Options are extra cmd_args tokens.
// visitor=true → -visitor; listener=false (default split) → -no-listener.
//
// cmd_args: [python3, stdout2stderr.py, jdk/bin/java, -jar, antlr4.jar,
// <grammar>, -Dlanguage=Cpp, -o, $(B)/<modulePath>, ...options].
// outputs: <grammar>{Lexer,Parser,Visitor,BaseVisitor}.{cpp,h}.
// inputs: [grammar.g4, stdout2stderr.py, antlr4.jar]; cwd: $(B)/<modulePath>.
func EmitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	emit Emitter,
) NodeRef {
	grammarVFS := Source(instance.Path + "/" + grammar)
	outDirVFS := Build(instance.Path)
	outDir := outDirVFS.String()

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		antlr4JarPath,
		grammarVFS.String(),
		"-Dlanguage=Cpp",
		"-o",
		outDir,
	}
	if visitor {
		cmdArgs = append(cmdArgs, "-visitor")
	}
	if !listener {
		cmdArgs = append(cmdArgs, "-no-listener")
	} else {
		cmdArgs = append(cmdArgs, "-listener")
	}
	cmdArgs = append(cmdArgs, options...)

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	inputs := []VFS{
		grammarVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}

	// Outputs: derive from grammar base name (strip .g4).
	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outPrefix := instance.Path + "/" + base
	outputs := []VFS{
		Build(outPrefix + "Lexer.cpp"),
		Build(outPrefix + "Lexer.h"),
		Build(outPrefix + "Parser.cpp"),
		Build(outPrefix + "Parser.h"),
		Build(outPrefix + "Visitor.h"),
		Build(outPrefix + "BaseVisitor.h"),
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
				Cwd:     outDir,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":        "JV",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Outputs: outputs,
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}

// EmitJVSplit emits a JV node for RUN_ANTLR4_CPP_SPLIT (separate lexer
// + parser .g4 files).
//
// cmd_args: [python3, stdout2stderr.py, jdk/bin/java, -jar, antlr4.jar,
// <lexer>, <parser>, -Dlanguage=Cpp, -o, $(B)/<modulePath>, ...flags].
// Outputs: {Lexer,Parser}.{cpp,h} + ParserVisitor.h, ParserBaseVisitor.h.
func EmitJVSplit(
	instance ModuleInstance,
	lexer string,
	parser string,
	visitor bool,
	listener bool,
	emit Emitter,
) NodeRef {
	lexerVFS := Source(instance.Path + "/" + lexer)
	parserVFS := Source(instance.Path + "/" + parser)
	outDirVFS := Build(instance.Path)
	outDir := outDirVFS.String()

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		antlr4JarPath,
		lexerVFS.String(),
		parserVFS.String(),
		"-Dlanguage=Cpp",
		"-o",
		outDir,
	}
	if visitor {
		cmdArgs = append(cmdArgs, "-visitor")
	}
	if !listener {
		cmdArgs = append(cmdArgs, "-no-listener")
	} else {
		cmdArgs = append(cmdArgs, "-listener")
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	inputs := []VFS{
		lexerVFS,
		parserVFS,
		stdout2stderrVFS,
		antlr4JarVFS,
	}

	// Outputs: lexer base name + parser base name outputs.
	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outPrefix := instance.Path + "/"
	outputs := []VFS{
		Build(outPrefix + lexerBase + ".cpp"),
		Build(outPrefix + lexerBase + ".h"),
		Build(outPrefix + parserBase + ".cpp"),
		Build(outPrefix + parserBase + ".h"),
		Build(outPrefix + visitorBase + "Visitor.h"),
		Build(outPrefix + visitorBase + "BaseVisitor.h"),
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
				Cwd:     outDir,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":        "JV",
			"pc":       "light-blue",
			"show_out": "yes",
		},
		Outputs: outputs,
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}

// ─── CF ──────────────────────────────────────────────────────────────────────

// configureFilePy is the source-relative path to the configure_file.py
// script used in all CF nodes.
var configureFilePyVFS = Source("build/scripts/configure_file.py")
var configureFilePyPath = configureFilePyVFS.String()

// buildTypeDebug is the BUILD_TYPE configuration variable injected by the
// build system. Hardcoded to DEBUG for the M3 debug build.
const buildTypeDebug = "BUILD_TYPE=DEBUG"

// EmitCF emits a CF node expanding a .cpp.in / .c.in template via
// configure_file.py. Output strips the .in suffix.
//
// cmd_args: [python3, configure_file.py, $(S)/<modulePath>/<srcRel>,
// $(B)/<modulePath>/<srcRel without .in>, <cfgVars...>].
// cfgVars derive from DEFAULT(name value) declarations filtered to
// vars actually @VAR@-referenced in the .in; BUILD_TYPE=DEBUG is
// injected when referenced but not DEFAULT-declared.
//
// Returns (CF NodeRef, outputPath).
func EmitCF(
	instance ModuleInstance,
	srcRel string,
	in ModuleCCInputs,
	emit Emitter,
) (NodeRef, VFS) {
	srcVFS := Source(instance.Path + "/" + srcRel)
	// Strip .in suffix to get output path.
	outVFS := Build(instance.Path + "/" + strings.TrimSuffix(srcRel, ".in"))

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// Pass the real on-disk source path (SourceRoot/<path>/<srcRel>)
	// so buildCFGVars can read the .in file to scan for @VAR@ refs.
	srcDiskPath := in.SourceRoot + "/" + instance.Path + "/" + srcRel
	cfgVars := buildCFGVars(srcDiskPath, in.DefaultVars, in.DefaultVarOrder)

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		configureFilePyPath,
		srcVFS.String(),
		outVFS.String(),
	}
	cmdArgs = append(cmdArgs, cfgVars...)

	// Inputs: script + source + header closure scanned from the .in file.
	// The include scanner handles the .cpp.in just like a .cpp file.
	inputs := make([]VFS, 0, 2+len(in.IncludeInputs))
	inputs = append(inputs, configureFilePyVFS, srcVFS)
	inputs = append(inputs, in.IncludeInputs...)

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":  "CF",
			"pc": "yellow",
		},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node), outVFS
}

// cfgVarRefRe matches @VAR_NAME@ substitution markers in .in template files.
var cfgVarRefRe = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)

// buildCFGVars constructs $CFG_VARS from the module's DEFAULT
// declarations, filtered to vars actually @VAR@-referenced in the .in
// source, sorted alphabetically (ymake's order).
//
// srcDiskPath is the on-disk path to the .in (not the $(S)/... macro
// path) so the file is readable to scan for @VAR@.
//
// If @BUILD_TYPE@ is referenced but not DEFAULT-declared, ymake injects
// BUILD_TYPE=DEBUG.
//
// Empirical: build_info.cpp.in → ["BUILD_TYPE=DEBUG"]; sandbox.cpp.in
// → ["KOSHER_SVN_VERSION=", "SANDBOX_TASK_ID=0"].
func buildCFGVars(srcDiskPath string, defaultVars map[string]string, defaultVarOrder []string) []string {
	// Scan the .in file for @VAR@ references. Unreadable → skip
	// (generated sources may not exist on disk yet). This disk read
	// is legitimate: extracts structured @VAR@ refs at CF-emission
	// time, not for closure walks.
	referenced := map[string]bool{}

	if data, err := os.ReadFile(srcDiskPath); err == nil {
		for _, m := range cfgVarRefRe.FindAllSubmatch(data, -1) {
			referenced[string(m[1])] = true
		}
	}

	// Collect DEFAULT-declared vars that are actually referenced.
	var vars []string
	declaredSet := map[string]bool{}

	for _, name := range defaultVarOrder {
		if !referenced[name] {
			continue
		}

		val, ok := defaultVars[name]
		if !ok {
			continue
		}

		vars = append(vars, name+"="+val)
		declaredSet[name] = true
	}

	// Inject global BUILD_TYPE=DEBUG when @BUILD_TYPE@ is referenced but
	// not declared via DEFAULT.
	if referenced["BUILD_TYPE"] && !declaredSet["BUILD_TYPE"] {
		vars = append(vars, buildTypeDebug)
	}

	// ymake emits CFG_VARS in alphabetical order.
	sort.Strings(vars)

	return vars
}

// ─── BI ──────────────────────────────────────────────────────────────────────

// yieldLinePyPath is the source-relative path to the yield_line.py script.
var yieldLinePyVFS = Source("build/scripts/yield_line.py")
var yieldLinePyPath = yieldLinePyVFS.String()

// xargsPyPath is the source-relative path to the xargs.py script.
var xargsPyVFS = Source("build/scripts/xargs.py")
var xargsPyPath = xargsPyVFS.String()

// buildInfoGenPyPath is the source-relative path to the build_info_gen.py
// script invoked by xargs.py in the BI node.
var buildInfoGenPyVFS = Source("build/scripts/build_info_gen.py")
var buildInfoGenPyPath = buildInfoGenPyVFS.String()

// EmitBI emits a BI node for CREATE_BUILDINFO_FOR(outputHeader).
// Three cmds:
//
//	cmd[0]: yield_line.py -- <module>/__args <cxx_compiler>
//	cmd[1]: yield_line.py -- <module>/__args <cxx_flags...>
//	cmd[2]: xargs.py -- <module>/__args python3 build_info_gen.py <out>
//
// Flags come from the target CXX bundle (same as a target CC for this
// module, minus -c, -o, input path).
//
// cache:false at top level matches REF; normalize.py drops it during
// M1/M2 canonicalization so byte-exact hashes are unaffected.
// inputs: [yield_line.py, xargs.py, build_info_gen.py];
// outputs: [$(B)/<modulePath>/<outputHeader>].
func EmitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []string,
	emit Emitter,
) NodeRef {
	outPrefix := instance.Path + "/"
	argsFileVFS := Build(outPrefix + "__args")
	outVFS := Build(outPrefix + outputHeader)
	argsFile := argsFileVFS.String()

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// cmd[0]: yield the CXX compiler path into __args.
	cmd0Args := []string{
		instance.Platform.Tools.Python3,
		yieldLinePyPath,
		"--",
		argsFile,
		instance.Platform.Tools.CXX,
	}

	// cmd[1]: yield CXX compile flags into __args.
	cmd1Args := make([]string, 0, 4+len(cxxFlags))
	cmd1Args = append(cmd1Args,
		instance.Platform.Tools.Python3,
		yieldLinePyPath,
		"--",
		argsFile,
	)
	cmd1Args = append(cmd1Args, cxxFlags...)

	// cmd[2]: xargs.py feeds __args into build_info_gen.py.
	cmd2Args := []string{
		instance.Platform.Tools.Python3,
		xargsPyPath,
		"--",
		argsFile,
		instance.Platform.Tools.Python3,
		buildInfoGenPyPath,
		outVFS.String(),
	}

	inputs := []VFS{
		yieldLinePyVFS,
		xargsPyVFS,
		buildInfoGenPyVFS,
	}

	cacheFalse := false
	node := &Node{
		Cache: &cacheFalse,
		Cmds: []Cmd{
			{CmdArgs: cmd0Args, Env: env},
			{CmdArgs: cmd1Args, Env: env},
			{CmdArgs: cmd2Args, Env: env},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"disable_cache": "yes",
			"p":             "BI",
			"pc":            "yellow",
			"show_out":      "yes",
		},
		Outputs: []VFS{outVFS},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}

// biFlagsForInstance composes the CXX flag bundle for a BI node.
// Matches a target CXX compile for a musl module (build_info peers
// base64 which chains into musl): noLibcUndebugBlock × 2 flanking
// catboostOpenSourceDefine plus the CXX tail. On aarch64,
// `-mno-outline-atomics` sits between `-UNDEBUG` and the warning
// suppressions in each noLibcUndebugBlock half. Reference:
// library/cpp/build_info/buildinfo_data.h on default-linux-aarch64.
func biFlagsForInstance(targetP *Platform) []string {
	bundle := compileFlagBundleFor(targetP)
	flags := make([]string, 0, 100)
	flags = append(flags, debugPrefixMapFlags...)
	flags = append(flags, xclangDebugCompilationDir...)
	flags = append(flags, bundle.CFlags...)
	flags = append(flags, warningFlags...)
	flags = append(flags, bundle.Defines...)
	flags = append(flags, bundle.NoLibcBlock...)
	flags = append(flags, catboostOpenSourceDefine...)
	flags = appendAutoPeerAndCPUFeatures(flags, bundle, []string{"-D_musl_"})
	flags = append(flags, bundle.NoLibcBlock...)
	flags = append(flags, cxxStandardFlag)
	// CXX warning extensions (from appendCxxStdAndOwn).
	flags = append(flags,
		"-Wimport-preprocessor-directive-pedantic",
		"-Woverloaded-virtual",
		"-Wno-ambiguous-reversed-operator",
		"-Wno-defaulted-function-deleted",
		"-Wno-deprecated-anon-enum-enum-conversion",
		"-Wno-deprecated-enum-enum-conversion",
		"-Wno-deprecated-enum-float-conversion",
		"-Wno-deprecated-volatile",
		"-Wno-pessimizing-move",
		"-Wno-undefined-var-template",
	)
	flags = append(flags, "-nostdinc++")
	flags = append(flags, catboostOpenSourceDefine...)
	flags = append(flags, "-nostdinc++")
	return flags
}

// ─── PR ──────────────────────────────────────────────────────────────────────

// EmitPR emits a PR node for a RUN_PROGRAM invocation.
//
// toolBinPath: BUILD_ROOT-absolute path to the tool binary (from
// walking the tool PROGRAM's LDPath). toolLDRef: the tool's LD node.
//
// cmd_args: [toolBinPath, <args with ${ARCADIA_ROOT}→$(S)>]. Bare
// filenames matching IN/OUT/OUT_NOAUTO/STDOUT are expanded to
// $(S)/.../ or $(B)/.../ respectively. inputs: tool + IN abs paths +
// closure. outputs: STDOUT or OUT/OUT_NOAUTO abs paths.
// deps/foreign_deps.tool carry toolLDRef.
type prEmitResult struct {
	Ref    NodeRef
	Inputs []VFS
}

func EmitPR(
	instance ModuleInstance,
	srcDir *string,
	stmt *RunProgramStmt,
	toolBinPath VFS,
	toolLDRef NodeRef,
	inputClosure []VFS,
	extraDepRefs []NodeRef,
	emit Emitter,
) prEmitResult {
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}
	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		} else {
			env[kv] = ""
		}
	}

	// Expand positional args: ${ARCADIA_ROOT}→$(S), ${MODDIR}→
	// instance.Path; bare IN/OUT/OUT_NOAUTO/STDOUT filenames → abs.
	inSet := make(map[string]bool, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inSet[f] = true
	}
	outSet := make(map[string]bool, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles)+1)
	for _, f := range stmt.OUTFiles {
		outSet[f] = true
	}
	for _, f := range stmt.OUTNoAutoFiles {
		outSet[f] = true
	}
	if stmt.StdoutFile != nil {
		outSet[*stmt.StdoutFile] = true
	}

	cmdArgs := make([]string, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, toolBinPath.String())
	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(S)")
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path)
		a = strings.ReplaceAll(a, "$CURDIR", Source(instance.Path).String())
		// If the arg is a plain filename (no path sep, no - prefix, no =),
		// and appears in IN list, expand to SOURCE_ROOT abs.
		if inSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = Source(runProgramSourceRel(instance, srcDir, a)).String()
		} else if outSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			// Bare OUT / OUT_NOAUTO / STDOUT basenames are rewritten to
			// $(B)/<modulePath>/<basename> so the consumer
			// references the generated artifact's absolute path.
			a = Build(instance.Path + "/" + a).String()
		}
		cmdArgs = append(cmdArgs, a)
	}

	// Inputs: tool + IN files + closure, deduped (IN files also
	// reached transitively via closure appear once, matching REF).
	inAbsPaths := make([]VFS, 0, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inAbsPaths = append(inAbsPaths, Source(runProgramSourceRel(instance, srcDir, f)))
	}

	inputs := make([]VFS, 0, 1+len(inAbsPaths)+len(inputClosure))
	seen := make(map[VFS]struct{}, 1+len(inAbsPaths)+len(inputClosure))
	appendUnique := func(p VFS) {
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		inputs = append(inputs, p)
	}
	appendUnique(toolBinPath)
	for _, p := range inAbsPaths {
		appendUnique(p)
	}
	for _, p := range inputClosure {
		appendUnique(p)
	}

	// Build outputs list.
	var outputs []VFS
	var stdoutPath string
	if stmt.StdoutFile != nil {
		stdoutVFS := Build(instance.Path + "/" + *stmt.StdoutFile)
		stdoutPath = stdoutVFS.String()
		outputs = append(outputs, stdoutVFS)
	}
	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, Build(instance.Path+"/"+f))
	}
	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, Build(instance.Path+"/"+f))
	}

	// Build deps: tool LD ref + any input dep refs.
	depRefs := make([]NodeRef, 0, 1+len(extraDepRefs))
	if toolLDRef != (NodeRef{}) {
		depRefs = append(depRefs, toolLDRef)
	}
	// Thread codegen-producer refs reached via the PR's inputClosure
	// (e.g. cross-module EN whose _serialized.h is in an IN header's
	// closure). REF places these in deps[] alongside toolLDRef.
	depRefs = append(depRefs, extraDepRefs...)

	// foreignDepRefs.tool = [toolLDRef].
	foreignDepTool := make([]NodeRef, 0, 1)
	if toolLDRef != (NodeRef{}) {
		foreignDepTool = append(foreignDepTool, toolLDRef)
	}

	cmd := Cmd{
		CmdArgs: cmdArgs,
		Env:     env,
	}
	if stdoutPath != "" {
		cmd.Stdout = stdoutPath
	}
	if stmt.CWD != nil {
		cmd.Cwd = expandRunProgramCWD(instance, *stmt.CWD)
	}

	// Empty instance.Platform.Tags must stay non-nil so JSON
	// serialises as `[]`, not `null`.
	tags := instance.Platform.Tags

	node := &Node{
		Cmds:    []Cmd{cmd},
		Env:     env,
		Inputs:  inputs,
		Outputs: outputs,
		KV: map[string]string{
			"p":        "PR",
			"pc":       "yellow",
			"show_out": "yes",
		},
		Tags:         tags,
		HostPlatform: instance.Platform.IsHost,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: map[string][]NodeRef{"tool": foreignDepTool},
	}

	return prEmitResult{
		Ref:    emit.Emit(node),
		Inputs: append([]VFS(nil), inputs...),
	}
}

func runProgramSourceRel(instance ModuleInstance, srcDir *string, rel string) string {
	if srcDir != nil {
		return *srcDir + "/" + rel
	}

	return instance.Path + "/" + rel
}

func expandRunProgramCWD(instance ModuleInstance, cwd string) string {
	cwd = strings.ReplaceAll(cwd, "$BINDIR", Build(instance.Path).String())
	cwd = strings.ReplaceAll(cwd, "$CURDIR", Source(instance.Path).String())
	cwd = strings.ReplaceAll(cwd, "${ARCADIA_BUILD_ROOT}", "$(B)")
	cwd = strings.ReplaceAll(cwd, "${ARCADIA_ROOT}", "$(S)")

	return cwd
}

// ─── emitMiscNodes ────────────────────────────────────────────────────────────

// emitMiscNodes (defined below) emits all module-level JV, CF, BI, PR
// nodes. The implicit-CF path for .cpp.in/.c.in sources is in
// emitOneSource.
//
