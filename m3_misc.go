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
// antlr4RuntimeHeaderVFS is the $(S)-rooted antlr4 C++ umbrella
// header included by all ANTLR4-generated .h files; used as static
// EmitsIncludes for JV .h outputs.
var antlr4RuntimeHeaderVFS = Source("contrib/libs/antlr4_cpp_runtime/src/antlr4-runtime.h")
var antlr4RuntimeHeaderPath = antlr4RuntimeHeaderVFS.String()

// antlr4FsToolsVFS / antlr4ProcCmdVFS are build-script helpers
// threaded into JV-derived CP/CC inputs, slotted after the JV primary
// output and before the grammar .g4 files.
var antlr4FsToolsVFS = Source("build/scripts/fs_tools.py")
var antlr4ProcCmdVFS = Source("build/scripts/process_command_files.py")
var antlr4FsToolsPath = antlr4FsToolsVFS.String()
var antlr4ProcCmdFiles = antlr4ProcCmdVFS.String()

// emitMiscNodes emits all module-level JV, CF, BI, PR nodes. With
// non-nil consumerInputs, also emits the downstream CP+CC chain for
// each JV grammar .cpp output, returning per-CC (refs, outputs,
// memberInputs) for the caller's AR-member accumulators.
func emitMiscNodes(ctx *genCtx, instance ModuleInstance, d *moduleData, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS, memberInputsList [][]VFS) {
	outPrefix := instance.Path + "/"
	reg := codegenRegForInstance(ctx, instance)

	// JV: emit one node per ANTLR4 grammar declaration.
	for _, g := range d.antlr4Grammars {
		if g.IsSplit {
			jvRef := EmitJVSplit(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, ctx.emit)
			// Register .h outputs. ANTLR4-generated headers include
			// antlr4-runtime.h; JV-generated .h also carries the
			// antlr4 toolchain witnesses (antlr.jar, stdout2stderr.py,
			// .g4 sources) and the sibling .cpp output.
			// ProducerRef = jvRef so consumer CC reaching a JV-generated
			// .h transitively threads JV into deps[].
			lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
			parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
			if reg != nil {
				lexerG4 := Source(instance.Path + "/" + g.Lexer)
				parserG4 := Source(instance.Path + "/" + g.Parser)
				lexerCpp := Build(outPrefix + lexerBase + ".cpp")
				parserCpp := Build(outPrefix + parserBase + ".cpp")
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", lexerCpp, nil, jvRef)
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", parserCpp, nil, jvRef)
				witnessIncludes := []VFS{
					antlr4RuntimeHeaderVFS,
					lexerCpp,
					stdout2stderrVFS,
					antlr4JarVFS,
					lexerG4,
					parserG4,
				}
				for _, suffix := range []string{
					lexerBase + ".h",
					parserBase + ".h",
					parserBase + "Visitor.h",
					parserBase + "BaseVisitor.h",
				} {
					parsed := make([]includeDirective, 0, len(witnessIncludes))
					for _, include := range witnessIncludes {
						parsed = append(parsed, includeDirective{kind: includeQuoted, target: include.Rel})
					}
					registerBoundGeneratedParsedOutput(ctx, instance, "JV", Build(outPrefix+suffix), parsed, jvRef)
				}
			}
			// PR-M3-antlr-g4-cpp: emit CP+CC for each grammar .cpp output.
			if consumerInputs != nil {
				// JV inputs (grammar files + scripts + jar) are the JV node's Inputs.
				jvInputs := []VFS{
					Source(instance.Path + "/" + g.Lexer),
					Source(instance.Path + "/" + g.Parser),
					stdout2stderrVFS,
					antlr4JarVFS,
				}
				jvPrimary := Build(outPrefix + lexerBase + ".cpp")
				cpccPairs := []struct{ cpp, h VFS }{
					{Build(outPrefix + lexerBase + ".cpp"), Build(outPrefix + lexerBase + ".h")},
					{Build(outPrefix + parserBase + ".cpp"), Build(outPrefix + parserBase + ".h")},
				}
				refs, outs, inputs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)
				ccRefs = append(ccRefs, refs...)
				ccOutputs = append(ccOutputs, outs...)
				memberInputsList = append(memberInputsList, inputs...)
			}
		} else {
			jvRef := EmitJV(instance, g.Grammar, g.Options, g.Visitor, g.Listener, ctx.emit)
			// Register .h outputs (same witness set as split path).
			// ProducerRef = jvRef.
			base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
			if reg != nil {
				grammarG4 := Source(instance.Path + "/" + g.Grammar)
				lexerCpp := Build(outPrefix + base + "Lexer.cpp")
				parserCpp := Build(outPrefix + base + "Parser.cpp")
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", lexerCpp, nil, jvRef)
				registerBoundGeneratedParsedOutput(ctx, instance, "JV", parserCpp, nil, jvRef)
				witnessIncludes := []VFS{
					antlr4RuntimeHeaderVFS,
					lexerCpp,
					stdout2stderrVFS,
					antlr4JarVFS,
					grammarG4,
				}
				for _, suffix := range []string{
					base + "Lexer.h",
					base + "Parser.h",
					base + "Visitor.h",
					base + "BaseVisitor.h",
				} {
					parsed := make([]includeDirective, 0, len(witnessIncludes))
					for _, include := range witnessIncludes {
						parsed = append(parsed, includeDirective{kind: includeQuoted, target: include.Rel})
					}
					registerBoundGeneratedParsedOutput(ctx, instance, "JV", Build(outPrefix+suffix), parsed, jvRef)
				}
			}
			// PR-M3-antlr-g4-cpp: emit CP+CC for each grammar .cpp output.
			if consumerInputs != nil {
				jvInputs := []VFS{
					Source(instance.Path + "/" + g.Grammar),
					stdout2stderrVFS,
					antlr4JarVFS,
				}
				jvPrimary := Build(outPrefix + base + "Lexer.cpp")
				cpccPairs := []struct{ cpp, h VFS }{
					{Build(outPrefix + base + "Lexer.cpp"), Build(outPrefix + base + "Lexer.h")},
					{Build(outPrefix + base + "Parser.cpp"), Build(outPrefix + base + "Parser.h")},
				}
				refs, outs, inputs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)
				ccRefs = append(ccRefs, refs...)
				ccOutputs = append(ccOutputs, outs...)
				memberInputsList = append(memberInputsList, inputs...)
			}
		}
	}

	// CF: emit one node per explicit CONFIGURE_FILE() declaration.
	for _, cf := range d.configureFiles {
		emitExplicitCF(ctx, instance, cf, d, reg)
	}

	// BI: emit one node when CREATE_BUILDINFO_FOR was declared.
	if d.createBuildInfoFor != nil {
		biRef := EmitBI(instance, *d.createBuildInfoFor, biFlagsForInstance(instance.Platform), ctx.emit)
		// Register BI output (buildinfo_data.h). The BI-script trio
		// (build_info_gen.py + xargs.py + yield_line.py) flows up into
		// CC consumers via EmitsIncludes. ProducerRef = biRef so the
		// consumer CC carries BI in deps[].
		if reg != nil {
			registerBoundGeneratedParsedOutput(ctx, instance, "BI", Build(outPrefix+*d.createBuildInfoFor), []includeDirective{
				{kind: includeQuoted, target: buildInfoGenPyVFS.Rel},
				{kind: includeQuoted, target: xargsPyVFS.Rel},
				{kind: includeQuoted, target: yieldLinePyVFS.Rel},
			}, biRef)
		}
	}

	// RUN_PROGRAM emission lives in emitRunProgramsForAR (gen.go) —
	// ahead of AR so PR outputs with CC-compilable extensions can be
	// threaded into the module's AR member list. No-op here.
	_ = d.runPrograms
	return
}

// emitJVDownstreamCPCC emits one CP + one CC for each (cpp, h) pair
// from a JV grammar invocation. Returns per-CC (refs, outputPaths,
// memberInputs).
//
// Pattern: JV outputs CmdLexer.cpp → CP renames to CmdLexer.g4.cpp
// → CC compiles CmdLexer.g4.cpp.o.
//
// CP inputs: [jvPrimary, (srcCpp if != primary), fsTools, procCmd,
// jvInputs..., antlr4-runtime closure...].
// CC inputs add srcH between jvPrimary and fsTools.
//
// outputIncludes carries repo-relative headers from the macro's
// OUTPUT_INCLUDES; rebased to $(S)/... and added to the CP .g4.cpp
// EmitsIncludes so CC scan walks the transitive closure.
func emitJVDownstreamCPCC(
	ctx *genCtx,
	instance ModuleInstance,
	jvRef NodeRef,
	jvPrimary VFS,
	jvInputs []VFS,
	cpccPairs []struct{ cpp, h VFS },
	outputIncludes []string,
	in ModuleCCInputs,
) (ccRefs []NodeRef, ccOutputs []VFS, memberInputsList [][]VFS) {
	reg := codegenRegForInstance(ctx, instance)

	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h

		// Derive the .g4.cpp name: replace .cpp suffix with .g4.cpp.
		base := strings.TrimSuffix(filepath.Base(srcCpp.Rel), ".cpp")
		g4CppPath := Build(instance.Path + "/" + base + ".g4.cpp")
		g4CppRel := base + ".g4.cpp"

		// Register .g4.cpp so walkClosure resolves its transitive
		// antlr4-runtime.h chain and the macro's OUTPUT_INCLUDES
		// (rebased to $(S)/...). Scanner walks each entry transitively.
		if reg != nil {
			emits := make([]includeDirective, 0, 1+len(outputIncludes))
			emits = append(emits, includeDirective{kind: includeQuoted, target: antlr4RuntimeHeaderVFS.Rel})
			for _, h := range outputIncludes {
				emits = append(emits, includeDirective{kind: includeQuoted, target: h})
			}
			registerGeneratedParsedOutput(ctx, instance, "CP", g4CppPath, emits)
		}

		// Compute the include closure from the g4.cpp (through the registry).
		ccIn := in
		ccIn.IsGenerated = true
		ccIn.HasGenerator = false
		ccIn.ExtraDepRefs = nil
		closure := walkClosure(ctx, instance, g4CppPath, ccIn)

		// CP node inputs: [jvPrimary, (srcCpp if != primary), fsTools, procCmd, jvInputs..., closure...]
		cpInputs := make([]VFS, 0, 2+len(jvInputs)+len(closure)+2)
		cpInputs = append(cpInputs, jvPrimary)
		if srcCpp != jvPrimary {
			cpInputs = append(cpInputs, srcCpp)
		}
		cpInputs = append(cpInputs, antlr4FsToolsVFS, antlr4ProcCmdVFS)
		cpInputs = append(cpInputs, jvInputs...)
		cpInputs = append(cpInputs, closure...)

		// The closure minus the cp-specific prefix is the antlr4 content.
		// Pass only the closure part to EmitJVCPG4 (it assembles the prefix itself).
		cpRef := EmitJVCPG4(instance, srcCpp, g4CppPath, jvRef, jvPrimary, jvInputs, closure, ctx.emit)

		// CC node inputs: EmitCC with IsGenerated=true sets inputPath=g4CppPath.
		// IncludeInputs = [jvPrimary, srcH, fsTools, procCmd, jvInputs..., closure...]
		ccIncludeInputs := make([]VFS, 0, 3+len(jvInputs)+len(closure)+2)
		ccIncludeInputs = append(ccIncludeInputs, jvPrimary)
		ccIncludeInputs = append(ccIncludeInputs, srcH)
		ccIncludeInputs = append(ccIncludeInputs, antlr4FsToolsVFS, antlr4ProcCmdVFS)
		ccIncludeInputs = append(ccIncludeInputs, jvInputs...)
		ccIncludeInputs = append(ccIncludeInputs, closure...)

		ccIn.IncludeInputs = ccIncludeInputs
		// Deps: [jvRef, cpRef] — matching reference sg2.json shape.
		ccIn.HasGenerator = true
		ccIn.Generator = jvRef
		ccIn.ExtraDepRefs = []NodeRef{cpRef}
		// ANTLR4-generated .g4.cpp files have per-rule unused locals;
		// `-Wno-unused-variable` silences the `-Werror` diagnostic.
		// Slotted by the composer between macroPrefixMapFlags and the
		// input path (sg2.json TConfLexer.g4.cpp.o cmd_args[144..145]).
		ccIn.PerSourceCFlags = []string{"-Wno-unused-variable"}

		ccRef, ccOut := EmitCC(instance, g4CppRel, ccIn, ctx.host, ctx.emit)

		// AR memberInputs: SOURCE_ROOT closure entries only. fs_tools.py
		// and process_command_files.py are CP-step helpers witnessed
		// in REF on the AR rollup (libdevtools-ymake-lang.a).
		memberInputs := make([]VFS, 0, len(closure)+2)
		memberInputs = append(memberInputs, antlr4FsToolsVFS, antlr4ProcCmdVFS)
		for _, p := range closure {
			if p.IsBuild() {
				continue
			}
			memberInputs = append(memberInputs, p)
		}

		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)
		memberInputsList = append(memberInputsList, memberInputs)
		_ = cpInputs // assembled inside EmitJVCPG4; kept for clarity
	}

	return
}

// emitRunProgramsForAR emits PR nodes ahead of the module's AR step and,
// for each PR output whose extension is a CC-compilable source kind,
// emits a downstream CC consuming the registered BUILD_ROOT source.
//
// Implements the PR→CC terminal case. A RUN_PROGRAM with
// `STDOUT/OUT foo.cpp` emits the .cpp under $(B)/<instance.Path>/foo.cpp
// and the consuming CC compiles it into foo.cpp.o which the AR/LD
// archives alongside the module's regular SRCS. Mirrors upstream
// ymake's auto-promote of compilable-extension RUN_PROGRAM outputs.
//
// Empirical: devtools/ymake/symbols emits dep_types.h_dumper.cpp via
// STDOUT, then archives dep_types.h_dumper.cpp.o.
//
// Returns per-CC (refs, outputs, memberInputs) for the caller's
// AR-member accumulators.
type runProgramsForARResult struct {
	CCRefs       []NodeRef
	CCOutputs    []VFS
	MemberInputs [][]VFS
}

func emitRunProgramsForAR(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) *runProgramsForARResult {
	if len(d.runPrograms) == 0 {
		return nil
	}

	reg := codegenRegForInstance(ctx, instance)
	res := &runProgramsForARResult{}

	for _, rp := range d.runPrograms {
		prRef := emitRunProgram(ctx, instance, rp, d, reg, in)
		// Record (output filename → PR NodeRef) so ARCHIVE() in the
		// same module can wire the AR's dep set to the producing PR.
		if d.prOutputProducer == nil {
			d.prOutputProducer = map[string]NodeRef{}
		}
		for _, f := range rp.OUTFiles {
			d.prOutputProducer[f] = prRef
		}
		for _, f := range rp.OUTNoAutoFiles {
			d.prOutputProducer[f] = prRef
		}
		if rp.StdoutFile != nil {
			d.prOutputProducer[*rp.StdoutFile] = prRef
		}

		// Classify outputs by extension. CC-compilable outputs
		// trigger a downstream CC; opaque outputs (.pyc etc.) stay
		// as registry-only entries.
		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, rp.OUTFiles...)
		// OUT_NOAUTO suppresses auto-promote-to-source upstream, so
		// rp.OUTNoAutoFiles is skipped for CC dispatch even when its
		// extension is .cpp/.c/...
		if rp.StdoutFile != nil {
			outs = append(outs, *rp.StdoutFile)
		}

		for _, out := range outs {
			if !isCCSourceExt(out) {
				continue
			}

			ccRef, ccOut, ccIns := emitPRDownstreamCC(ctx, instance, out, prRef, in)
			res.CCRefs = append(res.CCRefs, ccRef)
			res.CCOutputs = append(res.CCOutputs, ccOut)
			res.MemberInputs = append(res.MemberInputs, ccIns)
		}
	}

	return res
}

// ─── ARCHIVE (PR-M3-unpaired-got-closure) ───────────────────────────────────

// archiverToolPath is the upstream host-tool that the ARCHIVE() macro
// invokes per `build/ymake.core.conf:4142-4145` (`$ARCH_TOOL`). Pinned to
// the M3 layout: tools/archiver builds a single binary named `archiver`.
const archiverToolPath = "tools/archiver"

// emitArchives emits one AR node per `ARCHIVE(NAME <out> [DONTCOMPRESS]
// files...)` declaration. Invokes the host archiver binary (resolved
// by walking tools/archiver as a host PROGRAM).
//
// cmd_args: `archiver -q -x [-p] <file1>: [<file2>:] -o <out>`. Each
// file gets a trailing colon (upstream `${suf=\:;input}`); PR-produced
// files resolve to BUILD_ROOT, others to $(S)/<modulePath>/<file>.
//
// Inputs: PR outputs ($(B)) + archiver tool + producer-PR IN files
// ($(S)). Deps: producer-PR NodeRef + archiver LDRef.
func emitArchives(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.archives) == 0 {
		return
	}

	// Walk the archiver as a host program to resolve its binary path + LD
	// ref. Mirrors emitRunProgram's tool-walk shape; archiver lives at
	// tools/archiver and is hard-pinned by const archiverToolPath above.
	toolInstance := NewToolInstance(ctx.host, archiverToolPath)
	toolInstance.Flags = inferFlagsFromPath(archiverToolPath, true)

	var (
		toolBinPath = Build(archiverToolPath + "/archiver")
		toolLDRef   NodeRef
	)
	if exc := Try(func() {
		res := genModule(ctx, toolInstance)
		toolLDRef = res.LDRef
		if res.LDPath != nil {
			toolBinPath = *res.LDPath
		}
	}); exc != nil {
		// Tool walk failure surfaces as a fallback path; matches
		// emitRunProgram's pattern (the build will still record the
		// node, but with the conventional binary path).
	}

	// Aggregate SOURCE_ROOT-rooted IN files contributed by every PR
	// in this module — REF includes the full upstream set in each
	// ARCHIVE's inputs[], not just the producing PR's IN list. Sort +
	// dedup once.
	var prInSources []VFS
	{
		seen := map[VFS]struct{}{}
		for _, rp := range d.runPrograms {
			for _, f := range rp.INFiles {
				p := Source(instance.Path + "/" + f)
				if _, dup := seen[p]; dup {
					continue
				}
				seen[p] = struct{}{}
				prInSources = append(prInSources, p)
			}
		}
		SortVFS(prInSources)
	}

	reg := codegenRegForInstance(ctx, instance)
	for _, a := range d.archives {
		emitArchive(instance, a, d, toolBinPath, toolLDRef, prInSources, ctx.emit, reg)
	}
}

// emitArchive emits a single AR node for one ARCHIVE() declaration.
// Helper for emitArchives; split out so the tool-walk + shared input
// aggregation runs once per module rather than once per ARCHIVE.
func emitArchive(
	instance ModuleInstance,
	a archiveEntry,
	d *moduleData,
	toolBinPath VFS,
	toolLDRef NodeRef,
	prInSources []VFS,
	emit Emitter,
	reg *CodegenRegistry,
) {
	archiveVFS := Build(instance.Path + "/" + a.Name)
	archivePath := archiveVFS.String()

	// Build cmd_args. Each archived file is rendered with a trailing
	// colon per upstream `${suf=\:;input:Files}`.
	cmdArgs := make([]string, 0, 4+len(a.Files)+2)
	cmdArgs = append(cmdArgs, toolBinPath.String(), "-q", "-x")
	if a.DontCompress {
		cmdArgs = append(cmdArgs, "-p")
	}

	// Track the unique producer PRs so we can wire deps to all of them
	// and add their BUILD_ROOT outputs to the AR's input set.
	producerRefs := []NodeRef{}
	producerSet := map[NodeRef]struct{}{}
	pathPerFile := make([]VFS, 0, len(a.Files))
	pathStrPerFile := make([]string, 0, len(a.Files))

	for _, f := range a.Files {
		// When the file matches a PR output of this module, resolve to
		// the producer's BUILD_ROOT-rooted path and record the PR
		// NodeRef for dep wiring. Otherwise treat as SOURCE_ROOT-
		// relative to the module dir.
		isPRProduced := false
		if d.prOutputProducer != nil {
			if ref, ok := d.prOutputProducer[f]; ok {
				isPRProduced = true
				if _, dup := producerSet[ref]; !dup {
					producerSet[ref] = struct{}{}
					producerRefs = append(producerRefs, ref)
				}
			}
		}

		rel := instance.Path + "/" + f
		var absVFS VFS
		if isPRProduced {
			absVFS = Build(rel)
		} else {
			absVFS = Source(rel)
		}
		absStr := absVFS.String()

		pathPerFile = append(pathPerFile, absVFS)
		pathStrPerFile = append(pathStrPerFile, absStr)
		cmdArgs = append(cmdArgs, absStr+":")
	}
	cmdArgs = append(cmdArgs, "-o", archivePath)

	// REF's AR inputs include every upstream-PR output that is
	// lexicographically ≤ the archive's explicitly-referenced file.
	// ARCHIVE on `sitecustomize.pyc` lists sibling `__res.pyc`; the
	// inverse (ARCHIVE on `__res.pyc`) does NOT include the later
	// sibling. The lex gate keeps input ordering stable across
	// multiple archives sharing a producer.
	prSiblingOutputs := make([]VFS, 0)
	{
		// Largest archived file path (string form) becomes the upper
		// bound — siblings whose .String() form is strictly greater
		// are excluded. pathStrPerFile reuses the .String() materialised
		// while building cmd_args, so we don't re-allocate here.
		maxArchived := ""
		for _, s := range pathStrPerFile {
			if s > maxArchived {
				maxArchived = s
			}
		}
		seen := map[VFS]struct{}{}
		for _, p := range pathPerFile {
			seen[p] = struct{}{}
		}
		for _, rp := range d.runPrograms {
			rpProduces := false
			for _, f := range rp.OUTFiles {
				if _, ok := producerSet[d.prOutputProducer[f]]; ok {
					rpProduces = true
					break
				}
			}
			if !rpProduces {
				for _, f := range rp.OUTNoAutoFiles {
					if _, ok := producerSet[d.prOutputProducer[f]]; ok {
						rpProduces = true
						break
					}
				}
			}
			if !rpProduces && rp.StdoutFile != nil {
				if _, ok := producerSet[d.prOutputProducer[*rp.StdoutFile]]; ok {
					rpProduces = true
				}
			}
			if !rpProduces {
				continue
			}
			collect := func(rel string) {
				v := Build(instance.Path + "/" + rel)
				if v.String() > maxArchived {
					return
				}
				if _, dup := seen[v]; dup {
					return
				}
				seen[v] = struct{}{}
				prSiblingOutputs = append(prSiblingOutputs, v)
			}
			for _, f := range rp.OUTFiles {
				collect(f)
			}
			for _, f := range rp.OUTNoAutoFiles {
				collect(f)
			}
			if rp.StdoutFile != nil {
				collect(*rp.StdoutFile)
			}
		}
	}

	// inputs: each archived file's resolved path, then any sibling PR
	// outputs from the same producer (preserving REF's "all PR outputs
	// appear in every consumer's inputs" shape — sitecustomize.pyc.inc
	// lists __res.pyc even though it only archives sitecustomize.pyc),
	// then the tool binary, then the producer PR's source `IN` files
	// (rebased to SOURCE_ROOT, pre-aggregated by caller). Dedup
	// against the per-file slot.
	inputs := make([]VFS, 0, len(pathPerFile)+len(prSiblingOutputs)+1+len(prInSources))
	// Build a global lexical-order set of BUILD_ROOT entries:
	// pathPerFile ∪ prSiblingOutputs, sorted by canonical String form,
	// so REF's "alphabetical merge of producer outputs" shape lines up.
	buildRootSet := map[VFS]struct{}{}
	for _, p := range pathPerFile {
		buildRootSet[p] = struct{}{}
	}
	for _, p := range prSiblingOutputs {
		buildRootSet[p] = struct{}{}
	}
	buildRootSorted := make([]VFS, 0, len(buildRootSet))
	for p := range buildRootSet {
		buildRootSorted = append(buildRootSorted, p)
	}
	SortVFS(buildRootSorted)
	inputs = append(inputs, buildRootSorted...)
	inputs = append(inputs, toolBinPath)
	inSet := map[VFS]struct{}{}
	for _, p := range inputs {
		inSet[p] = struct{}{}
	}
	for _, p := range prInSources {
		if _, dup := inSet[p]; dup {
			continue
		}
		inSet[p] = struct{}{}
		inputs = append(inputs, p)
	}

	depRefs := make([]NodeRef, 0, len(producerRefs)+1)
	depRefs = append(depRefs, producerRefs...)
	if toolLDRef != (NodeRef{}) {
		depRefs = append(depRefs, toolLDRef)
	}

	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(S)"}

	// PR-M3-platform-pair-step5: tags + host_platform + platform from
	// the Platform pair passed by caller. Empty `instance.Platform.Tags` keeps
	// the slice non-nil so JSON serialises as `[]`, not `null`.
	tags := instance.Platform.Tags

	n := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:    env,
		Inputs: inputs,
		KV: map[string]string{
			"p":  "AR",
			"pc": "light-red",
		},
		Outputs:      []VFS{archiveVFS},
		Platform:     string(instance.Platform.Target),
		HostPlatform: instance.Platform.IsHost,
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		Tags: tags,
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		DepRefs: depRefs,
	}

	arRef := emit.Emit(n)

	// Register the AR's output (.pyc.inc header for runtime_py3) in
	// the codegen registry. Consumer CCs (e.g. __res.cpp) carry the
	// .pyc.inc path via runtimePy3CCExtraInputs;
	// resolveCodegenDepRefs lifts ProducerRef into deps[].
	// EmitsIncludes is nil — .pyc.inc is a RESOURCE-packed C array,
	// not C-readable.
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    "AR",
			OutputPath:     archiveVFS,
			ProducerRef:    arRef,
			HasProducerRef: true,
		})
	}
}

// isCCSourceExt reports whether path names a CC-compilable source.
// PR outputs with these extensions become implicit module sources
// (.c/.cpp/.cc/.cxx). .S/.s/.asm are excluded — PR currently produces
// no assembly outputs and the AS path has its own toolchain
// prerequisites (yasm walk).
func isCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") ||
		strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") ||
		strings.HasSuffix(p, ".c")
}

// emitPRDownstreamCC emits the CC compiling a PR-generated source.
// IsGenerated=true; IncludeInputs from WalkBuildRootClosure over the
// registered output (populated by emitRunProgram with EmitsIncludes=
// nil — opaque tool output, empty closure today).
//
// PR-emitted source: $(B)/<instance.Path>/<out>; composeCCPaths
// yields $(B)/<instance.Path>/<out>.o (flat for slash-free <out>).
func emitPRDownstreamCC(ctx *genCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS, []VFS) {
	// Thread prRef as the downstream CC's leading dep. walkClosure
	// skips the root so a registry probe alone can't surface PR; REF
	// places PR as CC's leading dep (dep_types.h_dumper.cpp.o → PR).
	return emitCodegenDownstreamCC(ctx, instance, out, nil, []NodeRef{prRef}, in)
}

// emitCodegenDownstreamCC emits the downstream CC for a codegen
// producer's `.cpp/.cc/.cxx/.c` output (EN's `.h_serialized.cpp`,
// PR's STDOUT/OUT .cpp, etc.). The producer's .cpp lives at
// $(B)/<instance.Path>/<cppRel> and the owning module compiles it as
// an implicit AR member.
//
//   - IsGenerated=true → composeCCPaths roots input/output under $(B).
//   - IncludeInputs from walkClosure() over the registered .cpp;
//     producer MUST register EmitsIncludes in the codegen registry
//     first.
//   - depPrefix prepends cross-codegen dep entries the reference
//     places ahead of the primary source (EN consumers prepend cross-EN
//     `_serialized.cpp` + `_serialized.h`; PR has no cross-deps).
func emitCodegenDownstreamCC(ctx *genCtx, instance ModuleInstance, cppRel string, depPrefix []VFS, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS, []VFS) {
	cppPath := Build(instance.Path + "/" + cppRel)

	closure := walkClosure(ctx, instance, cppPath, in)

	// Prepend depPrefix into IncludeInputs so CC's Inputs[] carries
	// the cross-codegen dep paths ahead of the consumer's own .cpp
	// (sg2.json export_json_debug.h_serialized.cpp.o inputs[0..1] =
	// the cross-EN dep .cpp + .h). Dedup against the scanner closure
	// — cross-EN .h already arrives via the registry's `_serialized.h`
	// EmitsIncludes; only the .cpp reliably needs the explicit prepend.
	includeInputs := make([]VFS, 0, len(depPrefix)+len(closure))
	seen := make(map[VFS]struct{}, len(depPrefix)+len(closure))
	for _, p := range depPrefix {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		includeInputs = append(includeInputs, p)
	}
	for _, p := range closure {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		includeInputs = append(includeInputs, p)
	}

	ccIn := in
	ccIn.IsGenerated = true
	ccIn.IncludeInputs = includeInputs
	ccIn.ExtraDepRefs = depRefs

	// Append codegen-producer refs reached via the .cpp's transitive
	// include closure (PB/EV peers pulled in by .pb.h / .ev.pb.h).
	// Filter out anything already in depRefs to avoid duplication.
	extra := resolveCodegenDepRefs(ctx, instance, includeInputs, depRefs...)
	if len(extra) > 0 {
		ccIn.ExtraDepRefs = append(append([]NodeRef(nil), depRefs...), extra...)
	}

	ref, outPath := EmitCC(instance, cppRel, ccIn, ctx.host, ctx.emit)

	// AR member-inputs: SOURCE_ROOT-rooted closure entries only.
	// REF (libdevtools-ymake.a) carries the EN-downstream CC's include-
	// closure .h entries but never the BUILD_ROOT `_serialized.{cpp,h}`
	// outputs themselves — those are wired implicitly via the .o
	// archive members.
	ccInputs := make([]VFS, 0, len(closure))
	for _, p := range closure {
		if p.IsBuild() {
			continue
		}
		ccInputs = append(ccInputs, p)
	}

	return ref, outPath, ccInputs
}

// emitExplicitCF emits a CF node for an explicit CONFIGURE_FILE(src dst)
// declaration (not triggered by a .cpp.in source in SRCS).
func emitExplicitCF(ctx *genCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *moduleData, reg *CodegenRegistry) {
	// Build a minimal ModuleCCInputs for CF emission — only DefaultVars
	// and the scanner context matter; the compilation flags are not used.
	in := ModuleCCInputs{
		DefaultVars:     d.defaultVars,
		DefaultVarOrder: d.defaultVarOrder,
		SourceRoot:      ctx.sourceRoot,
	}

	// Scan the .in file for its header closure (same as a .cpp source).
	srcPath := cf.Src
	if !strings.Contains(srcPath, "/") {
		srcPath = instance.Path + "/" + cf.Src
	}
	in.IncludeInputs = walkClosure(ctx, instance, resolveSourceVFS(ctx, instance, cf.Src, in.SrcDir), in)

	cfRef, cfOut := EmitCF(instance, cf.Src, in, ctx.emit)

	// F-7-B: register the explicit CF output with EmitsIncludes.
	if reg != nil {
		diskPath := ctx.sourceRoot + "/" + instance.Path + "/" + cf.Src
		registerBoundGeneratedParsedOutput(ctx, instance, "CF", cfOut, cfIncludeDirectives(diskPath), cfRef)
	}
}

// emitRunProgram emits a PR node for a RUN_PROGRAM declaration.
// It walks the tool PROGRAM as a host instance to get its LD ref/path.
func emitRunProgram(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, d *moduleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	// Walk the tool as a host program.
	toolPath := filepath.Clean(stmt.ToolPath)
	toolInstance := NewToolInstance(ctx.host, toolPath)
	toolInstance.Flags = inferFlagsFromPath(toolPath, true)

	toolBinPath := Build(toolPath + "/" + filepath.Base(toolPath))
	var toolLDRef NodeRef
	var toolInducedDeps []string

	if exc := Try(func() {
		res := genModule(ctx, toolInstance)
		toolLDRef = res.LDRef
		if res.LDPath != nil {
			toolBinPath = *res.LDPath
		}
		toolInducedDeps = res.InducedDeps
	}); exc != nil {
		// Swallow parse errors (tool may not fully parse); use fallback path.
	}

	// Register PR outputs FIRST so the closure walk below resolves
	// each output's $(B) path through the codegen registry.
	//
	// For CC-compilable outputs (.cpp/.cc/.cxx/.c): populate
	// EmitsIncludes with SOURCE_ROOT-rooted IN + OUTPUT_INCLUDES so
	// the downstream CC's closure picks up the transitive header set.
	// Non-compilable outputs (.h, .pyc) leave EmitsIncludes nil —
	// content is opaque without invoking the tool.
	//
	// The tool's INDUCED_DEPS(<ext> headers...) names headers the
	// tool injects into every output of the listed extensions; treat
	// as additional EmitsIncludes (e.g. struct2fieldcalc declares
	// INDUCED_DEPS(h+cpp field_calc_int.h) → scanner walks
	// field_calc_int.h → field_calc.h → autoarray.h).
	if reg != nil {
		for _, f := range stmt.OUTFiles {
			registerGeneratedParsedOutput(ctx, instance, "PR", Build(instance.Path+"/"+f), prEmitsIncludes(instance, d.srcDir, f, stmt, toolInducedDeps))
		}
		for _, f := range stmt.OUTNoAutoFiles {
			registerGeneratedParsedOutput(ctx, instance, "PR", Build(instance.Path+"/"+f), prEmitsIncludes(instance, d.srcDir, f, stmt, toolInducedDeps))
		}
		if stmt.StdoutFile != nil {
			registerGeneratedParsedOutput(ctx, instance, "PR", Build(instance.Path+"/"+*stmt.StdoutFile), prEmitsIncludes(instance, d.srcDir, *stmt.StdoutFile, stmt, toolInducedDeps))
		}
	}

	// Fold the transitive include closure of each CC-compilable
	// output into THIS PR node's inputs[] — REF's PR carries the full
	// closure (dep_types.h_dumper.cpp PR holds 1500 entries). Driven
	// by the registry: each output's EmitsIncludes is the SOURCE_ROOT
	// IN/OUTPUT_INCLUDES set; scanner follows real `#include`s from
	// there. Non-CC outputs contribute nothing.
	inputClosure := prInputClosure(ctx, instance, stmt, moduleInputs)

	// Resolve codegen-producer refs reached via the PR's inputClosure.
	// PR's deps[] must include any cross-module EN/PB/EV producer
	// whose generated header appears in the PR's transitive input
	// set (dep_types.h_dumper.cpp PR depends on diag's EN
	// stats_enums.h_serialized.cpp via dep_types.h → stats_enums.h).
	prExtraDepRefs := resolveCodegenDepRefs(ctx, instance, inputClosure, toolLDRef)

	prResult := EmitPR(instance, d.srcDir, stmt, toolBinPath, toolLDRef, inputClosure, prExtraDepRefs, ctx.emit)
	prRef := prResult.Ref
	if d.prOutputInputs == nil {
		d.prOutputInputs = map[string][]VFS{}
	}
	for _, f := range stmt.OUTFiles {
		d.prOutputInputs[f] = append([]VFS(nil), prResult.Inputs...)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		d.prOutputInputs[f] = append([]VFS(nil), prResult.Inputs...)
	}
	if stmt.StdoutFile != nil {
		d.prOutputInputs[*stmt.StdoutFile] = append([]VFS(nil), prResult.Inputs...)
	}

	// Backfill PR ProducerRef so the downstream CC's
	// resolveCodegenDepRefs threads PR into deps[]. Registry entries
	// above were created with HasProducerRef=false (ref not yet
	// known); SetProducerRef fills it atomically.
	if reg != nil {
		for _, f := range stmt.OUTFiles {
			bindGeneratedOutput(ctx, instance, Build(instance.Path+"/"+f), prRef)
		}
		for _, f := range stmt.OUTNoAutoFiles {
			bindGeneratedOutput(ctx, instance, Build(instance.Path+"/"+f), prRef)
		}
		if stmt.StdoutFile != nil {
			bindGeneratedOutput(ctx, instance, Build(instance.Path+"/"+*stmt.StdoutFile), prRef)
		}
	}

	return prRef
}

// prInputClosure returns the union of transitive include closures
// over every CC-compilable PR output (OUT / OUT_NOAUTO / STDOUT).
// Scanner walks registered EmitsIncludes (SOURCE_ROOT IN +
// OUTPUT_INCLUDES) and follows `#include`s from there. Non-CC outputs
// have nil EmitsIncludes and contribute nothing.
func prInputClosure(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, moduleInputs ModuleCCInputs) []VFS {
	// Use the consuming module's full scan-input bag so peer headers
	// reachable from the PR output's EmitsIncludes chain resolve.
	// Mirrors emitEnumSrcs (gen.go).
	scanIn := ModuleCCInputs{
		AddIncl:           moduleInputs.AddIncl,
		PeerAddInclGlobal: moduleInputs.PeerAddInclGlobal,
		SrcDir:            moduleInputs.SrcDir,
		SourceRoot:        ctx.sourceRoot,
	}

	var out []VFS
	walkOne := func(rel string) {
		buildRootPath := Build(instance.Path + "/" + rel)
		sub := walkClosure(ctx, instance, buildRootPath, scanIn)
		out = append(out, sub...)
	}

	for _, f := range stmt.OUTFiles {
		if !isCCSourceExt(f) {
			continue
		}
		walkOne(f)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		if !isCCSourceExt(f) {
			continue
		}
		walkOne(f)
	}
	if stmt.StdoutFile != nil && isCCSourceExt(*stmt.StdoutFile) {
		walkOne(*stmt.StdoutFile)
	}

	if len(out) == 0 {
		return nil
	}

	out = mergeDedupVFS(out, nil)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// prEmitsIncludes returns the EmitsIncludes set to register for a PR
// output named `outFile`. For CC-compilable outputs the convention is
// that the generated source textually `#include`s its IN files and any
// OUTPUT_INCLUDES-declared headers; for everything else the content is
// opaque and we return nil. PR-AUDIT-5.
//
// PR-M3-runprogram-closure: toolInducedDeps carries the tool PROGRAM's
// module-level INDUCED_DEPS(...) header list (repo-relative). Append
// those to the seed-include set so the include scanner reaches the
// transitive closure of headers the tool injects into its outputs.
func prEmitsIncludes(instance ModuleInstance, srcDir *string, outFile string, stmt *RunProgramStmt, toolInducedDeps []string) []includeDirective {
	if !isCCSourceExt(outFile) {
		return nil
	}

	includes := make([]includeDirective, 0, len(stmt.INFiles)+len(stmt.OutputIncludes)+len(toolInducedDeps))

	// IN files are module-relative; rebase to SOURCE_ROOT.
	for _, f := range stmt.INFiles {
		includes = append(includes, includeDirective{kind: includeQuoted, target: runProgramSourceRel(instance, srcDir, f)})
	}

	// OUTPUT_INCLUDES entries are repo-relative.
	for _, f := range stmt.OutputIncludes {
		includes = append(includes, includeDirective{kind: includeQuoted, target: f})
	}

	// Tool-declared INDUCED_DEPS (repo-relative).
	for _, f := range toolInducedDeps {
		includes = append(includes, includeDirective{kind: includeQuoted, target: f})
	}

	return includes
}
