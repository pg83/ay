package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// m3_misc.go — emitters for the five small-kind nodes introduced in PR-M3-E:
//   R5  — ragel5 two-step code generation (.rl → .rl.tmp → .rl5.cpp)
//   JV  — ANTLR4 grammar Java invocation (RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT)
//   CF  — CONFIGURE_FILE template expansion (.cpp.in/.c.in → .cpp/.c)
//   BI  — CREATE_BUILDINFO_FOR build-info header generation
//   PR  — RUN_PROGRAM code-generator invocation
//
// Reference nodes come from /home/pg/monorepo/yatool_orig/sg2.json.

// ─── R5 ──────────────────────────────────────────────────────────────────────

// EmitR5 emits an R5 node for a ragel5 two-step code generation.
// The node has two cmds:
//   cmd[0]: ragel5 -o <tmpPath> <srcPath>
//   cmd[1]: rlgen-cd -G2 -o <cppPath> <tmpPath>
//
// tmpPath  = $(BUILD_ROOT)/<modulePath>/<srcRel>.tmp
// cppPath  = $(BUILD_ROOT)/<modulePath>/<srcRel without .rl>.rl5.cpp
//
// Returns (R5 NodeRef, tmpPath, cppPath).
func EmitR5(
	instance ModuleInstance,
	srcRel string,
	ragel5LD NodeRef,
	rlgenCdLD NodeRef,
	ragel5BinPath string,
	rlgenCdBinPath string,
	emit Emitter,
) (NodeRef, string, string) {
	srcPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel
	tmpPath := "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + ".tmp"
	// Output: strip .rl suffix, append .rl5.cpp.
	cppPath := "$(BUILD_ROOT)/" + instance.Path + "/" + strings.TrimSuffix(srcRel, ".rl") + ".rl5.cpp"

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	cmd0 := Cmd{
		CmdArgs: []string{
			ragel5BinPath,
			"-o",
			tmpPath,
			srcPath,
		},
		Env: env,
	}
	cmd1 := Cmd{
		CmdArgs: []string{
			rlgenCdBinPath,
			"-G2",
			"-o",
			cppPath,
			tmpPath,
		},
		Env: env,
	}

	// inputs = [ragel5 binary, rlgen-cd binary, source .rl]
	inputs := []string{ragel5BinPath, rlgenCdBinPath, srcPath}

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
		Outputs: []string{tmpPath, cppPath},
		KV: map[string]string{
			"p":  "R5",
			"pc": "yellow",
		},
		Tags: []string{"tool"},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform:     string(instance.Target),
		HostPlatform: targetIsX8664(instance),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: map[string][]NodeRef{"tool": depRefs},
	}

	return emit.Emit(node), tmpPath, cppPath
}

// ─── JV ──────────────────────────────────────────────────────────────────────

// jdkResourcePath is the literal JDK17 resource path used in the reference
// sg2.json.  The hash suffix (564746473) is the resource bundle ID and
// is pinned byte-exact for the M3 closure.
const jdkResourcePath = "$(JDK17-564746473)/bin/java"

// antlr4JarPath is the source-relative path to the ANTLR4 jar.
const antlr4JarPath = "$(SOURCE_ROOT)/contrib/java/antlr/antlr4/antlr.jar"

// stdout2stderrPath is the wrapper script that redirects antlr4's stdout to
// stderr (required so the build system captures diagnostic output correctly).
const stdout2stderrPath = "$(SOURCE_ROOT)/build/scripts/stdout2stderr.py"

// python3Path is the system python3 binary, consistent with cp.go.
const python3Path = "/ix/realm/pg/bin/python3"

// EmitJV emits a JV node for a single RUN_ANTLR4_CPP grammar invocation.
// The grammar .g4 file is relative to the module dir.
// Options are extra cmd_args tokens (e.g. ["-package", "NConfReader"]).
// When visitor=true, adds -visitor; when listener=false (default for split),
// adds -no-listener.
//
// Reference cmd_args shape (single-grammar form):
//
//	[python3, stdout2stderr.py, jdk/bin/java, -jar, antlr4.jar,
//	 <grammar.g4>, -Dlanguage=Cpp, -o, $(BUILD_ROOT)/<modulePath>,
//	 <visitor?-visitor:-no-visitor>, <listener?-listener:-no-listener>,
//	 ...options]
//
// Outputs: <grammar>Lexer.cpp, <grammar>Lexer.h, <grammar>Parser.cpp,
//
//	<grammar>Parser.h, <grammar>Visitor.h, <grammar>BaseVisitor.h
//
// Inputs: [grammar.g4, stdout2stderr.py, antlr4.jar]
// cwd: $(BUILD_ROOT)/<modulePath>
func EmitJV(
	instance ModuleInstance,
	grammar string,
	options []string,
	visitor bool,
	listener bool,
	emit Emitter,
) NodeRef {
	grammarAbs := "$(SOURCE_ROOT)/" + instance.Path + "/" + grammar
	outDir := "$(BUILD_ROOT)/" + instance.Path

	cmdArgs := []string{
		python3Path,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		antlr4JarPath,
		grammarAbs,
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
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	inputs := []string{
		grammarAbs,
		stdout2stderrPath,
		antlr4JarPath,
	}

	// Outputs: derive from grammar base name (strip .g4).
	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outputs := []string{
		outDir + "/" + base + "Lexer.cpp",
		outDir + "/" + base + "Lexer.h",
		outDir + "/" + base + "Parser.cpp",
		outDir + "/" + base + "Parser.h",
		outDir + "/" + base + "Visitor.h",
		outDir + "/" + base + "BaseVisitor.h",
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
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}

// EmitJVSplit emits a JV node for a RUN_ANTLR4_CPP_SPLIT invocation that
// takes a lexer grammar and a parser grammar as separate files.
//
// Reference cmd_args shape (split form):
//
//	[python3, stdout2stderr.py, jdk/bin/java, -jar, antlr4.jar,
//	 lexerGrammar.g4, parserGrammar.g4, -Dlanguage=Cpp,
//	 -o, $(BUILD_ROOT)/<modulePath>, -visitor, -no-listener]
//
// Outputs: CmdLexer.cpp, CmdLexer.h, CmdParser.cpp, CmdParser.h,
//
//	CmdParserVisitor.h, CmdParserBaseVisitor.h
func EmitJVSplit(
	instance ModuleInstance,
	lexer string,
	parser string,
	visitor bool,
	listener bool,
	emit Emitter,
) NodeRef {
	lexerAbs := "$(SOURCE_ROOT)/" + instance.Path + "/" + lexer
	parserAbs := "$(SOURCE_ROOT)/" + instance.Path + "/" + parser
	outDir := "$(BUILD_ROOT)/" + instance.Path

	cmdArgs := []string{
		python3Path,
		stdout2stderrPath,
		jdkResourcePath,
		"-jar",
		antlr4JarPath,
		lexerAbs,
		parserAbs,
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
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	inputs := []string{
		lexerAbs,
		parserAbs,
		stdout2stderrPath,
		antlr4JarPath,
	}

	// Outputs: lexer base name + parser base name outputs.
	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outputs := []string{
		outDir + "/" + lexerBase + ".cpp",
		outDir + "/" + lexerBase + ".h",
		outDir + "/" + parserBase + ".cpp",
		outDir + "/" + parserBase + ".h",
		outDir + "/" + visitorBase + "Visitor.h",
		outDir + "/" + visitorBase + "BaseVisitor.h",
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
		Platform: string(instance.Target),
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

// configureFilePyPath is the source-relative path to the configure_file.py
// script used in all CF nodes.
const configureFilePyPath = "$(SOURCE_ROOT)/build/scripts/configure_file.py"

// buildTypeDebug is the BUILD_TYPE configuration variable injected by the
// build system. Hardcoded to DEBUG for the M3 debug build.
const buildTypeDebug = "BUILD_TYPE=DEBUG"

// EmitCF emits a CF node expanding a .cpp.in / .c.in template via
// configure_file.py.  The output strips the .in suffix.
//
// cmd_args shape:
//
//	[python3, configure_file.py,
//	 $(SOURCE_ROOT)/<modulePath>/<srcRel>,
//	 $(BUILD_ROOT)/<modulePath>/<srcRel without .in>,
//	 <cfgVars...>]
//
// cfgVars are derived from DEFAULT(name value) declarations in the
// module's ya.make (passed via in.DefaultVars / in.DefaultVarOrder), plus
// BUILD_TYPE=DEBUG injected for any module that references @BUILD_TYPE@.
//
// Inputs: [configure_file.py, source .cpp.in, ...header closure of the .in]
// Outputs: [$(BUILD_ROOT)/<modulePath>/<srcRel without .in>]
//
// Returns (CF NodeRef, outputPath).
func EmitCF(
	instance ModuleInstance,
	srcRel string,
	in ModuleCCInputs,
	emit Emitter,
) (NodeRef, string) {
	srcAbs := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel
	// Strip .in suffix to get output path.
	outRel := strings.TrimSuffix(srcRel, ".in")
	outAbs := "$(BUILD_ROOT)/" + instance.Path + "/" + outRel

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// Build cfg vars list.  Only include DEFAULT-declared vars whose
	// names appear in @VAR@ references in the source; we use the
	// declaration order from the ya.make.  Also inject BUILD_TYPE=DEBUG
	// if not already present (a global build-system var).
	// PR-M3-F-5: pass the real on-disk source path (SourceRoot/<path>/<srcRel>)
	// so buildCFGVars can read the .in file to scan for @VAR@ references.
	srcDiskPath := in.SourceRoot + "/" + instance.Path + "/" + srcRel
	cfgVars := buildCFGVars(srcDiskPath, in.DefaultVars, in.DefaultVarOrder)

	cmdArgs := []string{
		python3Path,
		configureFilePyPath,
		srcAbs,
		outAbs,
	}
	cmdArgs = append(cmdArgs, cfgVars...)

	// Inputs: script + source + header closure scanned from the .in file.
	// The include scanner handles the .cpp.in just like a .cpp file.
	inputs := make([]string, 0, 2+len(in.IncludeInputs))
	inputs = append(inputs, configureFilePyPath, srcAbs)
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
		Outputs: []string{outAbs},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node), outAbs
}

// cfgVarRefRe matches @VAR_NAME@ substitution markers in .in template files.
var cfgVarRefRe = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)

// buildCFGVars constructs the $CFG_VARS expansion from the module's DEFAULT
// declarations, filtered to only those variables actually referenced as
// @VAR@ in the .in source file and emitted in alphabetical order.
//
// srcDiskPath is the real on-disk path to the .in source (not the
// $(SOURCE_ROOT)/... macro path) so the file can be read to scan for
// @VAR@ references.
//
// ymake's $CFG_VARS logic:
//   - Only DEFAULT-declared vars that appear as @VAR@ in the .in source are
//     emitted (vars not referenced are silently dropped).
//   - Vars are emitted in alphabetical order (not declaration order).
//   - If @BUILD_TYPE@ is referenced but not DEFAULT-declared, ymake injects
//     the global BUILD_TYPE=DEBUG value.
//
// Empirical anchors (sg2.json reference):
//   - build_info.cpp.in references only @BUILD_TYPE@ → ["BUILD_TYPE=DEBUG"].
//   - sandbox.cpp.in references @KOSHER_SVN_VERSION@ and @SANDBOX_TASK_ID@
//     → ["KOSHER_SVN_VERSION=", "SANDBOX_TASK_ID=0"] (alpha order; no
//     BUILD_TYPE injected since @BUILD_TYPE@ is absent in sandbox.cpp.in).
func buildCFGVars(srcDiskPath string, defaultVars map[string]string, defaultVarOrder []string) []string {
	// Scan the .in file for @VAR@ references.  Silently skip unreadable files
	// (generated sources during a warm build may not exist on disk yet).
	//
	// PR-AUDIT-3: legitimate disk read — extracts structured @VAR@ references
	// from a .cpp.in/.c.in template at CF-node-emission time to filter the
	// DEFAULT-declared vars to only those the template actually references.
	// NOT for closure walks. Kept per audit doc §2 D12 scope-note.
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
const yieldLinePyPath = "$(SOURCE_ROOT)/build/scripts/yield_line.py"

// xargsPyPath is the source-relative path to the xargs.py script.
const xargsPyPath = "$(SOURCE_ROOT)/build/scripts/xargs.py"

// buildInfoGenPyPath is the source-relative path to the build_info_gen.py
// script invoked by xargs.py in the BI node.
const buildInfoGenPyPath = "$(SOURCE_ROOT)/build/scripts/build_info_gen.py"

// EmitBI emits a BI node for CREATE_BUILDINFO_FOR(outputHeader).
//
// The BI node has three cmds:
//   cmd[0]: yield_line.py -- <module>/__args <cxx_compiler>
//   cmd[1]: yield_line.py -- <module>/__args <cxx_flags...>
//   cmd[2]: xargs.py -- <module>/__args python3 build_info_gen.py <outputHeader>
//
// The compiler and flags come from the target platform CXX bundle
// (same flags as a target CC compile for this module, minus the
// compiler-invocation-specific args: -c, -o, input path).
//
// kv: {p: BI, pc: yellow, show_out: yes, disable_cache: yes}
// cache: false is NOT emitted (normalize.py drops it per D41/GOALS.md).
// inputs: [yield_line.py, xargs.py, build_info_gen.py]
// outputs: [$(BUILD_ROOT)/<modulePath>/<outputHeader>]
func EmitBI(
	instance ModuleInstance,
	outputHeader string,
	cxxFlags []string,
	emit Emitter,
) NodeRef {
	argsFile := "$(BUILD_ROOT)/" + instance.Path + "/__args"
	outputPath := "$(BUILD_ROOT)/" + instance.Path + "/" + outputHeader

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// cmd[0]: yield the CXX compiler path into __args.
	cmd0Args := []string{
		python3Path,
		yieldLinePyPath,
		"--",
		argsFile,
		cxxCompilerPath,
	}

	// cmd[1]: yield CXX compile flags into __args.
	cmd1Args := make([]string, 0, 4+len(cxxFlags))
	cmd1Args = append(cmd1Args,
		python3Path,
		yieldLinePyPath,
		"--",
		argsFile,
	)
	cmd1Args = append(cmd1Args, cxxFlags...)

	// cmd[2]: xargs.py feeds __args into build_info_gen.py.
	cmd2Args := []string{
		python3Path,
		xargsPyPath,
		"--",
		argsFile,
		python3Path,
		buildInfoGenPyPath,
		outputPath,
	}

	inputs := []string{
		yieldLinePyPath,
		xargsPyPath,
		buildInfoGenPyPath,
	}

	node := &Node{
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
		Outputs: []string{outputPath},
		Tags:    []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs: []NodeRef{},
	}

	return emit.Emit(node)
}

// biFlagsForInstance composes the CXX flag bundle for a BI node.  The
// reference BI node (library/cpp/build_info, aarch64) uses the same flag
// bundle as a target CXX compile for a musl module (the build_info library
// peers library/cpp/string_utils/base64 which chains into musl).
// The flags match the noLibcUndebugBlock sequence (two copies flanking
// catboostOpenSourceDefine) plus the CXX-specific tail.
func biFlagsForInstance() []string {
	flags := make([]string, 0, 100)
	flags = append(flags, debugPrefixMapFlags...)
	flags = append(flags, xclangDebugCompilationDir...)
	flags = append(flags, commonCFlags...)
	flags = append(flags, warningFlags...)
	flags = append(flags, commonDefines...)
	flags = append(flags, "-UNDEBUG")
	flags = append(flags, noLibcWarningSuppressions...)
	flags = append(flags, catboostOpenSourceDefine...)
	flags = append(flags, "-D_musl_")
	flags = append(flags, "-UNDEBUG")
	flags = append(flags, noLibcWarningSuppressions...)
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

// EmitPR emits a PR node for a RUN_PROGRAM macro invocation.
//
// toolBinPath is the BUILD_ROOT-absolute path to the tool binary (derived
// from walking the tool PROGRAM and taking its LDPath).
// toolLDRef is the NodeRef of the tool's LD node.
// stmt holds the parsed RUN_PROGRAM statement.
//
// cmd_args shape:
//
//	[toolBinPath, <args with ${ARCADIA_ROOT}→$(SOURCE_ROOT) substitution>]
//
// IN files in args are expanded to $(SOURCE_ROOT)/<modulePath>/<file>.
// OUT / OUT_NOAUTO files are expanded to $(BUILD_ROOT)/<modulePath>/<file>.
// STDOUT redirects cmd's stdout to $(BUILD_ROOT)/<modulePath>/<file>.
//
// inputs: [toolBinPath, ...IN file abs paths, ...input header closure]
// outputs: [stdout path] or [OUT/OUT_NOAUTO abs paths]
// deps: [toolLDRef, ...inputDepRefs]
// foreign_deps.tool: [toolLDRef]
//
// The node's platform matches the containing module's platform.
func EmitPR(
	instance ModuleInstance,
	stmt *RunProgramStmt,
	toolBinPath string,
	toolLDRef NodeRef,
	inputClosure []string,
	emit Emitter,
) NodeRef {
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}
	for _, kv := range stmt.EnvPairs {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		} else {
			env[kv] = ""
		}
	}

	// Expand positional args: substitute ${ARCADIA_ROOT} → $(SOURCE_ROOT),
	// and expand bare filenames that appear in the IN list to SOURCE_ROOT paths.
	inSet := make(map[string]bool, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inSet[f] = true
	}

	cmdArgs := make([]string, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, toolBinPath)
	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(SOURCE_ROOT)")
		// If the arg is a plain filename (no path sep, no - prefix, no =),
		// and appears in IN list, expand to SOURCE_ROOT abs.
		if inSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = "$(SOURCE_ROOT)/" + instance.Path + "/" + a
		}
		cmdArgs = append(cmdArgs, a)
	}

	// Build inputs list: tool binary + IN files + closure.
	inAbsPaths := make([]string, 0, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inAbsPaths = append(inAbsPaths, "$(SOURCE_ROOT)/"+instance.Path+"/"+f)
	}

	inputs := make([]string, 0, 1+len(inAbsPaths)+len(inputClosure))
	inputs = append(inputs, toolBinPath)
	inputs = append(inputs, inAbsPaths...)
	inputs = append(inputs, inputClosure...)

	// Build outputs list.
	var outputs []string
	var stdoutPath string
	if stmt.StdoutFile != "" {
		stdoutPath = "$(BUILD_ROOT)/" + instance.Path + "/" + stmt.StdoutFile
		outputs = append(outputs, stdoutPath)
	}
	for _, f := range stmt.OUTFiles {
		outputs = append(outputs, "$(BUILD_ROOT)/"+instance.Path+"/"+f)
	}
	for _, f := range stmt.OUTNoAutoFiles {
		outputs = append(outputs, "$(BUILD_ROOT)/"+instance.Path+"/"+f)
	}

	// Build deps: tool LD ref + any input dep refs.
	depRefs := make([]NodeRef, 0, 1)
	if toolLDRef != (NodeRef{}) {
		depRefs = append(depRefs, toolLDRef)
	}

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
	if stmt.CWD != "" {
		cmd.Cwd = stmt.CWD
	}

	// PR node tags: "tool" when emitted in a host (x86_64) context.
	tags := []string{}
	if targetIsX8664(instance) {
		tags = []string{"tool"}
	}

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
		HostPlatform: targetIsX8664(instance),
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: map[string][]NodeRef{"tool": foreignDepTool},
	}

	return emit.Emit(node)
}

// ─── emitMiscNodes ────────────────────────────────────────────────────────────

// emitMiscNodes emits all module-level JV, CF, BI, and PR nodes declared
// in the module's ya.make.  Called from genModule after emitPySrcs.
// The CF emitter for .cpp.in/.c.in sources is already handled in
// emitOneSource; this function handles only the explicit CONFIGURE_FILE()
// macro form (not the implicit .in-source form).
// antlr4RuntimeHeaderPath is the $(SOURCE_ROOT)-rooted path to the
// antlr4 C++ umbrella header included by all ANTLR4-generated .h files.
// F-7-B uses it as the static EmitsIncludes for JV .h outputs.
const antlr4RuntimeHeaderPath = "$(SOURCE_ROOT)/contrib/libs/antlr4_cpp_runtime/src/antlr4-runtime.h"

func emitMiscNodes(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	outDir := "$(BUILD_ROOT)/" + instance.Path
	reg := codegenRegForInstance(ctx, instance)

	// JV: emit one node per ANTLR4 grammar declaration.
	for _, g := range d.antlr4Grammars {
		if g.IsSplit {
			EmitJVSplit(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, ctx.emit)
			// F-7-B: register the .h outputs. ANTLR4-generated headers include
			// antlr4-runtime.h (XPathLexer.h pattern in
			// contrib/libs/antlr4_cpp_runtime/src/tree/xpath/XPathLexer.h).
			if reg != nil {
				lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
				parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
				for _, h := range []string{
					outDir + "/" + lexerBase + ".h",
					outDir + "/" + parserBase + ".h",
					outDir + "/" + parserBase + "Visitor.h",
					outDir + "/" + parserBase + "BaseVisitor.h",
				} {
					reg.Register(&GeneratedFileInfo{
						ProducerKvP:   "JV",
						OutputPath:    h,
						EmitsIncludes: []string{antlr4RuntimeHeaderPath},
					})
				}
			}
		} else {
			EmitJV(instance, g.Grammar, g.Options, g.Visitor, g.Listener, ctx.emit)
			// F-7-B: register .h outputs.
			if reg != nil {
				base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
				for _, h := range []string{
					outDir + "/" + base + "Lexer.h",
					outDir + "/" + base + "Parser.h",
					outDir + "/" + base + "Visitor.h",
					outDir + "/" + base + "BaseVisitor.h",
				} {
					reg.Register(&GeneratedFileInfo{
						ProducerKvP:   "JV",
						OutputPath:    h,
						EmitsIncludes: []string{antlr4RuntimeHeaderPath},
					})
				}
			}
		}
	}

	// CF: emit one node per explicit CONFIGURE_FILE() declaration.
	for _, cf := range d.configureFiles {
		emitExplicitCF(ctx, instance, cf, d, reg)
	}

	// BI: emit one node when CREATE_BUILDINFO_FOR was declared.
	if d.createBuildInfoFor != "" {
		EmitBI(instance, d.createBuildInfoFor, biFlagsForInstance(), ctx.emit)
		// F-7-B: register BI output. buildinfo_data.h is a generated header
		// but its #include content is opaque (build-system metadata); EmitsIncludes nil.
		if reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "BI",
				OutputPath:    outDir + "/" + d.createBuildInfoFor,
				EmitsIncludes: nil,
			})
		}
	}

	// PR: emit one node per RUN_PROGRAM declaration.
	for _, rp := range d.runPrograms {
		emitRunProgram(ctx, instance, rp, d, reg)
	}
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
	in.IncludeInputs = scanIncludesForSource(ctx, instance, cf.Src, in)

	_, cfOut := EmitCF(instance, cf.Src, in, ctx.emit)

	// F-7-B: register the explicit CF output with EmitsIncludes.
	if reg != nil {
		diskPath := ctx.sourceRoot + "/" + instance.Path + "/" + cf.Src
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:   "CF",
			OutputPath:    cfOut,
			EmitsIncludes: cfIncludeDirectives(diskPath),
		})
	}
}

// emitRunProgram emits a PR node for a RUN_PROGRAM declaration.
// It walks the tool PROGRAM as a host instance to get its LD ref/path.
func emitRunProgram(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, d *moduleData, reg *CodegenRegistry) {
	// Walk the tool as a host program.
	toolPath := filepath.Clean(stmt.ToolPath)
	toolInstance := instance.WithHost(ctx.cfg)
	toolInstance.Path = toolPath
	toolInstance.Flags = inferFlagsFromPath(toolPath, true)

	var toolBinPath string
	var toolLDRef NodeRef

	if exc := Try(func() {
		res := genModule(ctx, toolInstance)
		toolLDRef = res.LDRef
		toolBinPath = res.LDPath
	}); exc != nil {
		// Swallow parse errors (tool may not fully parse); use fallback path.
		toolBinPath = "$(BUILD_ROOT)/" + toolPath + "/" + filepath.Base(toolPath)
	}

	// Collect header closure for IN files.
	var inputClosure []string
	// (No scanner pass for PR nodes in M3-E scope — inputs are tracked
	// at the file level, not their transitive header closures, because
	// the in-files are listed explicitly in the RUN_PROGRAM macro.)

	EmitPR(instance, stmt, toolBinPath, toolLDRef, inputClosure, ctx.emit)

	// F-7-B: register PR outputs. PR outputs are generated files (possibly
	// .h headers) but their include content is tool-specific and opaque at
	// gen time. EmitsIncludes is nil — F-7-C will handle transitive lookup
	// if needed.
	if reg != nil {
		for _, f := range stmt.OUTFiles {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PR",
				OutputPath:    "$(BUILD_ROOT)/" + instance.Path + "/" + f,
				EmitsIncludes: nil,
			})
		}
		for _, f := range stmt.OUTNoAutoFiles {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PR",
				OutputPath:    "$(BUILD_ROOT)/" + instance.Path + "/" + f,
				EmitsIncludes: nil,
			})
		}
		if stmt.StdoutFile != "" {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PR",
				OutputPath:    "$(BUILD_ROOT)/" + instance.Path + "/" + stmt.StdoutFile,
				EmitsIncludes: nil,
			})
		}
	}
}
