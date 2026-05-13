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
	inputs := []VFS{ParseVFSOrSource(ragel5BinPath), ParseVFSOrSource(rlgenCdBinPath), ParseVFSOrSource(srcPath)}

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
		Outputs: []VFS{ParseVFSOrSource(tmpPath), ParseVFSOrSource(cppPath)},
		KV: map[string]string{
			"p":  "R5",
			"pc": "yellow",
		},
		// R5-specific: tags=["tool"] unconditionally (every R5 invocation
		// is a host-toolchain ragel5 call, regardless of the calling
		// module's axis — both x86_64 and aarch64 R5 variants carry the
		// tool tag in REF). This is intrinsic to R5; not derived from
		// instance.Platform.Tags.
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

	return emit.Emit(node), tmpPath, cppPath
}

// ─── JV ──────────────────────────────────────────────────────────────────────

// jdkResourcePath is the literal JDK17 resource path used in the reference
// sg2.json.  The hash suffix (564746473) is the resource bundle ID and
// is pinned byte-exact for the M3 closure.
const jdkResourcePath = "$(JDK17-564746473)/bin/java"

// antlr4JarPath is the source-relative path to the ANTLR4 jar.
const antlr4JarPath = "$(SOURCE_ROOT)/contrib/java/antlr/antlr4/antlr.jar"

var antlr4JarVFS = Source("contrib/java/antlr/antlr4/antlr.jar")

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

	inputs := []VFS{
		ParseVFSOrSource(grammarAbs),
		ParseVFSOrSource(stdout2stderrPath),
		ParseVFSOrSource(antlr4JarPath),
	}

	// Outputs: derive from grammar base name (strip .g4).
	base := strings.TrimSuffix(filepath.Base(grammar), ".g4")
	outputs := []VFS{
		ParseVFSOrSource(outDir + "/" + base + "Lexer.cpp"),
		ParseVFSOrSource(outDir + "/" + base + "Lexer.h"),
		ParseVFSOrSource(outDir + "/" + base + "Parser.cpp"),
		ParseVFSOrSource(outDir + "/" + base + "Parser.h"),
		ParseVFSOrSource(outDir + "/" + base + "Visitor.h"),
		ParseVFSOrSource(outDir + "/" + base + "BaseVisitor.h"),
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

	inputs := []VFS{
		ParseVFSOrSource(lexerAbs),
		ParseVFSOrSource(parserAbs),
		ParseVFSOrSource(stdout2stderrPath),
		ParseVFSOrSource(antlr4JarPath),
	}

	// Outputs: lexer base name + parser base name outputs.
	lexerBase := strings.TrimSuffix(filepath.Base(lexer), ".g4")
	parserBase := strings.TrimSuffix(filepath.Base(parser), ".g4")
	visitorBase := parserBase
	outputs := []VFS{
		ParseVFSOrSource(outDir + "/" + lexerBase + ".cpp"),
		ParseVFSOrSource(outDir + "/" + lexerBase + ".h"),
		ParseVFSOrSource(outDir + "/" + parserBase + ".cpp"),
		ParseVFSOrSource(outDir + "/" + parserBase + ".h"),
		ParseVFSOrSource(outDir + "/" + visitorBase + "Visitor.h"),
		ParseVFSOrSource(outDir + "/" + visitorBase + "BaseVisitor.h"),
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
	inputs := make([]VFS, 0, 2+len(in.IncludeInputs))
	inputs = append(inputs, ParseVFSOrSource(configureFilePyPath), ParseVFSOrSource(srcAbs))
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
		Outputs: []VFS{ParseVFSOrSource(outAbs)},
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
// cache: false is set as a top-level node field to match REF; the
// normalizer drops it during M1/M2 canonicalization (per
// normalize.py step 3) so the byte-exact M1/M2 hashes are
// unaffected, while the L3 comparator (which preserves the field)
// now matches REF on the BI node.
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

	inputs := []VFS{
		ParseVFSOrSource(yieldLinePyPath),
		ParseVFSOrSource(xargsPyPath),
		ParseVFSOrSource(buildInfoGenPyPath),
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
		Outputs: []VFS{ParseVFSOrSource(outputPath)},
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

// biFlagsForInstance composes the CXX flag bundle for a BI node.  The
// reference BI node (library/cpp/build_info, aarch64) uses the same flag
// bundle as a target CXX compile for a musl module (the build_info library
// peers library/cpp/string_utils/base64 which chains into musl).
// The flags match the noLibcUndebugBlock sequence (two copies flanking
// catboostOpenSourceDefine) plus the CXX-specific tail. On aarch64 the
// noLibcUndebugBlock prefix carries `-mno-outline-atomics` between
// `-UNDEBUG` and the warning suppressions tail (matches REF
// library/cpp/build_info/buildinfo_data.h on default-linux-aarch64).
//
// PR-M3-platform-pair-step6: dispatches on `instance.Platform.Target` (aarch64-
// specific codegen flag, not host/target axis).
func biFlagsForInstance(targetP *Platform) []string {
	flags := make([]string, 0, 100)
	flags = append(flags, debugPrefixMapFlags...)
	flags = append(flags, xclangDebugCompilationDir...)
	flags = append(flags, commonCFlags...)
	flags = append(flags, warningFlags...)
	flags = append(flags, commonDefines...)
	flags = append(flags, "-UNDEBUG")
	if targetP.Target == PlatformDefaultLinuxAArch64 {
		flags = append(flags, "-mno-outline-atomics")
	}
	flags = append(flags, noLibcWarningSuppressions...)
	flags = append(flags, catboostOpenSourceDefine...)
	flags = append(flags, "-D_musl_")
	flags = append(flags, "-UNDEBUG")
	if targetP.Target == PlatformDefaultLinuxAArch64 {
		flags = append(flags, "-mno-outline-atomics")
	}
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
	inputClosure []VFS,
	extraDepRefs []NodeRef,
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
	// substitute ${MODDIR} → instance.Path, and expand bare filenames
	// referenced by IN / OUT / OUT_NOAUTO / STDOUT to absolute paths.
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
	if stmt.StdoutFile != "" {
		outSet[stmt.StdoutFile] = true
	}

	cmdArgs := make([]string, 0, 1+len(stmt.Args))
	cmdArgs = append(cmdArgs, toolBinPath)
	for _, a := range stmt.Args {
		a = strings.ReplaceAll(a, "${ARCADIA_ROOT}", "$(SOURCE_ROOT)")
		a = strings.ReplaceAll(a, "${MODDIR}", instance.Path)
		// If the arg is a plain filename (no path sep, no - prefix, no =),
		// and appears in IN list, expand to SOURCE_ROOT abs.
		if inSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			a = "$(SOURCE_ROOT)/" + instance.Path + "/" + a
		} else if outSet[a] && !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			// Bare OUT / OUT_NOAUTO / STDOUT basenames are rewritten to
			// $(BUILD_ROOT)/<modulePath>/<basename> so the consumer
			// references the generated artifact's absolute path.
			a = "$(BUILD_ROOT)/" + instance.Path + "/" + a
		}
		cmdArgs = append(cmdArgs, a)
	}

	// Build inputs list: tool binary + IN files + closure. Dedup so an
	// IN file that is also reached transitively via the closure (it
	// appears in the registered EmitsIncludes set seeded from IN) is
	// listed once, matching the reference multiset shape.
	inAbsPaths := make([]VFS, 0, len(stmt.INFiles))
	for _, f := range stmt.INFiles {
		inAbsPaths = append(inAbsPaths, Source(instance.Path+"/"+f))
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
	appendUnique(ParseVFSOrSource(toolBinPath))
	for _, p := range inAbsPaths {
		appendUnique(p)
	}
	for _, p := range inputClosure {
		appendUnique(p)
	}

	// Build outputs list.
	var outputs []VFS
	var stdoutPath string
	if stmt.StdoutFile != "" {
		stdoutPath = "$(BUILD_ROOT)/" + instance.Path + "/" + stmt.StdoutFile
		outputs = append(outputs, Build(instance.Path+"/"+stmt.StdoutFile))
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
	// PR-M3-L0-cascade-close-v2: thread codegen-producer refs reached through
	// the PR's inputClosure (e.g. cross-module EN whose _serialized.h is in
	// the closure of an IN header consumed by the PR tool). REF places these
	// in the PR node's deps[] alongside toolLDRef.
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
	if stmt.CWD != "" {
		cmd.Cwd = stmt.CWD
	}

	// PR-M3-platform-pair-step5: tags + host_platform are baseline data
	// from `targetP`. Empty `instance.Platform.Tags` keeps the slice non-nil so
	// the JSON serialises as `[]`, not `null`.
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

// antlr4FsToolsPath / antlr4ProcCmdFiles are the build-script helpers
// threaded into JV-derived CP/CC node inputs (matching the reference
// sg2.json shape where every g4.cpp CP/CC inputs these paths after the
// JV primary output and before the grammar .g4 files).
const antlr4FsToolsPath = "$(SOURCE_ROOT)/build/scripts/fs_tools.py"
const antlr4ProcCmdFiles = "$(SOURCE_ROOT)/build/scripts/process_command_files.py"

// VFS-typed variants of antlr4FsToolsPath / antlr4ProcCmdFiles used by the
// VFS-internal flow (PR-M3-vfs-typed-paths). cmd_args still consume the
// string forms above.
var antlr4FsToolsVFS = Source("build/scripts/fs_tools.py")
var antlr4ProcCmdVFS = Source("build/scripts/process_command_files.py")

// emitMiscNodes emits all module-level JV, CF, BI, and PR nodes declared
// in the module's ya.make. When consumerInputs is non-nil, also emits the
// downstream CP + CC chain for each JV grammar .cpp output (the .g4.cpp
// rename + compile), returning per-CC (refs, outputPaths, memberInputs)
// for the caller to fold into the enclosing AR member accumulators.
func emitMiscNodes(ctx *genCtx, instance ModuleInstance, d *moduleData, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS, memberInputsList [][]VFS) {
	outDir := "$(BUILD_ROOT)/" + instance.Path
	reg := codegenRegForInstance(ctx, instance)

	// JV: emit one node per ANTLR4 grammar declaration.
	for _, g := range d.antlr4Grammars {
		if g.IsSplit {
			jvRef := EmitJVSplit(instance, g.Lexer, g.Parser, g.Visitor, g.Listener, ctx.emit)
			// F-7-B: register the .h outputs. ANTLR4-generated headers include
			// antlr4-runtime.h (XPathLexer.h pattern in
			// contrib/libs/antlr4_cpp_runtime/src/tree/xpath/XPathLexer.h).
			// PR-M3-final-codegen-registry-expansion: the JV-generated .h also
			// carries the antlr4 toolchain witnesses (antlr.jar, stdout2stderr.py
			// script, the .g4 sources) and the sibling .cpp output. Verified on
			// devtools/ymake/lang/cmd_parser.cpp.o and confreader.cpp.o.
			// PR-M3-L0-cascade-close-v2: ProducerRef = jvRef so the consumer CC
			// reaching a JV-generated .h transitively threads JV into its deps[].
			lexerBase := strings.TrimSuffix(filepath.Base(g.Lexer), ".g4")
			parserBase := strings.TrimSuffix(filepath.Base(g.Parser), ".g4")
			if reg != nil {
				lexerG4 := "$(SOURCE_ROOT)/" + instance.Path + "/" + g.Lexer
				parserG4 := "$(SOURCE_ROOT)/" + instance.Path + "/" + g.Parser
				lexerCpp := outDir + "/" + lexerBase + ".cpp"
				witnessIncludes := []string{
					antlr4RuntimeHeaderPath,
					lexerCpp,
					stdout2stderrPath,
					antlr4JarPath,
					lexerG4,
					parserG4,
				}
				for _, h := range []string{
					outDir + "/" + lexerBase + ".h",
					outDir + "/" + parserBase + ".h",
					outDir + "/" + parserBase + "Visitor.h",
					outDir + "/" + parserBase + "BaseVisitor.h",
				} {
					reg.Register(&GeneratedFileInfo{
						ProducerKvP:    "JV",
						OutputPath:     h,
						EmitsIncludes:  witnessIncludes,
						ProducerRef:    jvRef,
						HasProducerRef: true,
					})
				}
			}
			// PR-M3-antlr-g4-cpp: emit CP+CC for each grammar .cpp output.
			if consumerInputs != nil {
				// JV inputs (grammar files + scripts + jar) are the JV node's Inputs.
				jvInputs := []VFS{
					Source(instance.Path + "/" + g.Lexer),
					Source(instance.Path + "/" + g.Parser),
					ParseVFSOrSource(stdout2stderrPath),
					ParseVFSOrSource(antlr4JarPath),
				}
				jvPrimary := outDir + "/" + lexerBase + ".cpp"
				cpccPairs := []struct{ cpp, h string }{
					{outDir + "/" + lexerBase + ".cpp", outDir + "/" + lexerBase + ".h"},
					{outDir + "/" + parserBase + ".cpp", outDir + "/" + parserBase + ".h"},
				}
				refs, outs, inputs := emitJVDownstreamCPCC(ctx, instance, jvRef, jvPrimary, jvInputs, cpccPairs, g.OutputIncludes, *consumerInputs)
				ccRefs = append(ccRefs, refs...)
				ccOutputs = append(ccOutputs, outs...)
				memberInputsList = append(memberInputsList, inputs...)
			}
		} else {
			jvRef := EmitJV(instance, g.Grammar, g.Options, g.Visitor, g.Listener, ctx.emit)
			// F-7-B: register .h outputs.
			// PR-M3-final-codegen-registry-expansion: same witness set as
			// the split path (antlr.jar + stdout2stderr.py + .g4 source +
			// sibling Lexer.cpp).
			// PR-M3-L0-cascade-close-v2: ProducerRef = jvRef.
			base := strings.TrimSuffix(filepath.Base(g.Grammar), ".g4")
			if reg != nil {
				grammarG4 := "$(SOURCE_ROOT)/" + instance.Path + "/" + g.Grammar
				lexerCpp := outDir + "/" + base + "Lexer.cpp"
				witnessIncludes := []string{
					antlr4RuntimeHeaderPath,
					lexerCpp,
					stdout2stderrPath,
					antlr4JarPath,
					grammarG4,
				}
				for _, h := range []string{
					outDir + "/" + base + "Lexer.h",
					outDir + "/" + base + "Parser.h",
					outDir + "/" + base + "Visitor.h",
					outDir + "/" + base + "BaseVisitor.h",
				} {
					reg.Register(&GeneratedFileInfo{
						ProducerKvP:    "JV",
						OutputPath:     h,
						EmitsIncludes:  witnessIncludes,
						ProducerRef:    jvRef,
						HasProducerRef: true,
					})
				}
			}
			// PR-M3-antlr-g4-cpp: emit CP+CC for each grammar .cpp output.
			if consumerInputs != nil {
				jvInputs := []VFS{
					Source(instance.Path + "/" + g.Grammar),
					ParseVFSOrSource(stdout2stderrPath),
					ParseVFSOrSource(antlr4JarPath),
				}
				jvPrimary := outDir + "/" + base + "Lexer.cpp"
				cpccPairs := []struct{ cpp, h string }{
					{outDir + "/" + base + "Lexer.cpp", outDir + "/" + base + "Lexer.h"},
					{outDir + "/" + base + "Parser.cpp", outDir + "/" + base + "Parser.h"},
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
	if d.createBuildInfoFor != "" {
		biRef := EmitBI(instance, d.createBuildInfoFor, biFlagsForInstance(ctx.platformFor(instance)), ctx.emit)
		// F-7-B: register BI output. buildinfo_data.h is a generated header.
		// PR-M3-final-codegen-registry-expansion: the BI-script trio
		// (build_info_gen.py + xargs.py + yield_line.py) flows up into
		// CC consumers of the generated header (witnessed on
		// library/cpp/build_info/build_info_static.cpp.o in REF). Register
		// them as EmitsIncludes so the scanner closure propagates them.
		// PR-M3-L0-cascade-close-v2: ProducerRef = biRef so the CC
		// consumer of buildinfo_data.h carries the BI producer in deps[].
		if reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP: "BI",
				OutputPath:  outDir + "/" + d.createBuildInfoFor,
				EmitsIncludes: []string{
					buildInfoGenPyPath,
					xargsPyPath,
					yieldLinePyPath,
				},
				ProducerRef:    biRef,
				HasProducerRef: true,
			})
		}
	}

	// PR: PR-AUDIT-5 — RUN_PROGRAM emission moved ahead of AR so that
	// PR outputs ending in a CC-compilable extension (.cpp/.cc/.cxx/.c)
	// can be threaded into the module's AR member list as additional
	// CC sources. The dedicated entry point is `emitRunProgramsForAR`
	// (gen.go); this site is now a no-op for PR.
	_ = d.runPrograms
	return
}

// emitJVDownstreamCPCC emits one CP + one CC node for each (cpp, h) pair
// produced by a JV grammar invocation, and returns the per-CC triples
// (refs, outputPaths, memberInputs) for the caller to fold into the
// enclosing AR member accumulators.
//
// Pattern (reference sg2.json):
//
//	JV outputs CmdLexer.cpp → CP renames it to CmdLexer.g4.cpp
//	                        → CC compiles CmdLexer.g4.cpp.o
//
// CP inputs (matching reference):
//
//	[jvPrimaryOutput, (srcCpp when != primary), fsToolsPath, procCmdFiles,
//	 jvInputs..., antlr4-runtime closure...]
//
// CC inputs:
//
//	[jvPrimaryOutput, g4CppPath, srcHPath, fsToolsPath, procCmdFiles,
//	 jvInputs..., antlr4-runtime closure...]
//
// cpccPairs holds (srcCppAbsPath, srcHAbsPath) for each grammar .cpp output.
// jvPrimary is always the JV node's outputs[0] (the lexer .cpp).
// jvInputs are the JV node's Inputs (grammar .g4 files + scripts + jar).
// outputIncludes carries the repo-relative headers from the macro's
// OUTPUT_INCLUDES keyword (PR-M3-jv-antlr-system-headers); they are
// rebased to $(SOURCE_ROOT)/... and appended to the CP `.g4.cpp`
// EmitsIncludes so the CC scan walks their transitive closure.
func emitJVDownstreamCPCC(
	ctx *genCtx,
	instance ModuleInstance,
	jvRef NodeRef,
	jvPrimary string,
	jvInputs []VFS,
	cpccPairs []struct{ cpp, h string },
	outputIncludes []string,
	in ModuleCCInputs,
) (ccRefs []NodeRef, ccOutputs []VFS, memberInputsList [][]VFS) {
	reg := codegenRegForInstance(ctx, instance)

	for _, pair := range cpccPairs {
		srcCpp := pair.cpp
		srcH := pair.h

		// Derive the .g4.cpp name: replace .cpp suffix with .g4.cpp.
		base := strings.TrimSuffix(filepath.Base(srcCpp), ".cpp")
		g4CppPath := "$(BUILD_ROOT)/" + instance.Path + "/" + base + ".g4.cpp"
		g4CppRel := base + ".g4.cpp"

		// Register the .g4.cpp in the codegen registry so walkClosure
		// can resolve its transitive antlr4-runtime.h include chain.
		// PR-M3-jv-antlr-system-headers: also seed the registry entry with
		// the macro's OUTPUT_INCLUDES (rebased to $(SOURCE_ROOT)/...). The
		// scanner walks each entry transitively via the FS locator, so the
		// downstream system-header closure (e.g. util/generic/string.h →
		// glibcasm / musl / cxxsupp) lands in the CP/CC inputs naturally.
		if reg != nil {
			emits := make([]string, 0, 1+len(outputIncludes))
			emits = append(emits, antlr4RuntimeHeaderPath)
			for _, h := range outputIncludes {
				emits = append(emits, "$(SOURCE_ROOT)/"+h)
			}
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "CP",
				OutputPath:    g4CppPath,
				EmitsIncludes: emits,
			})
		}

		// Compute the include closure from the g4.cpp (through the registry).
		ccIn := in
		ccIn.IsGenerated = true
		ccIn.HasGenerator = false
		ccIn.ExtraDepRefs = nil
		closure := walkClosure(ctx, instance, ParseVFSOrSource(g4CppPath), ccIn)

		// CP node inputs: [jvPrimary, (srcCpp if != primary), fsTools, procCmd, jvInputs..., closure...]
		cpInputs := make([]VFS, 0, 2+len(jvInputs)+len(closure)+2)
		cpInputs = append(cpInputs, ParseVFSOrSource(jvPrimary))
		if srcCpp != jvPrimary {
			cpInputs = append(cpInputs, ParseVFSOrSource(srcCpp))
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
		ccIncludeInputs = append(ccIncludeInputs, ParseVFSOrSource(jvPrimary))
		ccIncludeInputs = append(ccIncludeInputs, ParseVFSOrSource(srcH))
		ccIncludeInputs = append(ccIncludeInputs, antlr4FsToolsVFS, antlr4ProcCmdVFS)
		ccIncludeInputs = append(ccIncludeInputs, jvInputs...)
		ccIncludeInputs = append(ccIncludeInputs, closure...)

		ccIn.IncludeInputs = ccIncludeInputs
		// Deps: [jvRef, cpRef] — matching reference sg2.json shape.
		ccIn.HasGenerator = true
		ccIn.Generator = jvRef
		ccIn.ExtraDepRefs = []NodeRef{cpRef}
		// PR-M3-antlr-listener-default: ANTLR4-generated .g4.cpp files declare
		// per-rule local variables the generator does not always reference;
		// upstream attaches `-Wno-unused-variable` to silence the resulting
		// `-Werror` diagnostic. The composer slots PerSourceCFlags between
		// macroPrefixMapFlags and the input path — matching the reference
		// position immediately before `<...>.g4.cpp` (sg2.json
		// devtools/ymake/lang/TConfLexer.g4.cpp.o cmd_args index 144..145).
		ccIn.PerSourceCFlags = []string{"-Wno-unused-variable"}

		ccRef, ccOut := EmitCC(instance, g4CppRel, ccIn, ctx.emit)

		// AR memberInputs: SOURCE_ROOT closure entries only (no BUILD_ROOT).
		// PR-M3-final-codegen-registry-expansion: fs_tools.py and
		// process_command_files.py are CP-step build-script helpers
		// witnessed in REF on the enclosing AR rollup (libdevtools-ymake-lang.a).
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
// PR-AUDIT-5: implements the Python→Ragel→C++ chain's terminal case
// (PR→CC). RUN_PROGRAM with `STDOUT foo.cpp` (or `OUT foo.cpp`) emits
// the .cpp under $(BUILD_ROOT)/<instance.Path>/foo.cpp; the consuming
// CC node compiles it into <foo>.cpp.o which the surrounding AR/LD
// archives alongside the module's regular SRCS.
//
// Empirical reference (sg2.json): devtools/ymake/symbols's RUN_PROGRAM
// emits dep_types.h_dumper.cpp via STDOUT, and the module's AR archives
// dep_types.h_dumper.cpp.o as the trailing member after the declared
// SRCS. Mirrors the upstream ymake behaviour: a RUN_PROGRAM whose
// STDOUT/OUT names a compilable extension auto-promotes that output to
// an implicit module source.
//
// Returns the per-CC `(refs, outputs, memberInputs)` triples for the
// caller to fold into the AR-member accumulators. `memberInputs` is
// already deduped against caller-side state via the returned per-CC
// slice; the caller's `addMemberInputs` performs the union.
func emitRunProgramsForAR(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS, memberInputs [][]VFS) {
	if len(d.runPrograms) == 0 {
		return nil, nil, nil
	}

	reg := codegenRegForInstance(ctx, instance)

	for _, rp := range d.runPrograms {
		prRef := emitRunProgram(ctx, instance, rp, d, reg, in)
		// PR-M3-unpaired-got-closure: record (output filename → PR
		// NodeRef) so ARCHIVE() in the same module can wire the AR's
		// dep set to the producing PR.
		if d.prOutputProducer == nil {
			d.prOutputProducer = map[string]NodeRef{}
		}
		for _, f := range rp.OUTFiles {
			d.prOutputProducer[f] = prRef
		}
		for _, f := range rp.OUTNoAutoFiles {
			d.prOutputProducer[f] = prRef
		}
		if rp.StdoutFile != "" {
			d.prOutputProducer[rp.StdoutFile] = prRef
		}

		// PR-AUDIT-5: classify outputs by extension. CC-compilable
		// outputs trigger a downstream CC; opaque outputs (.pyc and
		// the like) carry through as registry-only entries.
		outs := make([]string, 0, len(rp.OUTFiles)+len(rp.OUTNoAutoFiles)+1)
		outs = append(outs, rp.OUTFiles...)
		// PR-AUDIT-5: OUT_NOAUTO suppresses the auto-promote-to-source
		// behaviour upstream, so we skip rp.OUTNoAutoFiles for the CC
		// dispatch even when their extension is .cpp/.c/...
		if rp.StdoutFile != "" {
			outs = append(outs, rp.StdoutFile)
		}

		for _, out := range outs {
			if !isCCSourceExt(out) {
				continue
			}

			ccRef, ccOut, ccIns := emitPRDownstreamCC(ctx, instance, out, prRef, in)
			ccRefs = append(ccRefs, ccRef)
			ccOutputs = append(ccOutputs, ccOut)
			memberInputs = append(memberInputs, ccIns)
		}
	}

	return ccRefs, ccOutputs, memberInputs
}

// ─── ARCHIVE (PR-M3-unpaired-got-closure) ───────────────────────────────────

// archiverToolPath is the upstream host-tool that the ARCHIVE() macro
// invokes per `build/ymake.core.conf:4142-4145` (`$ARCH_TOOL`). Pinned to
// the M3 layout: tools/archiver builds a single binary named `archiver`.
const archiverToolPath = "tools/archiver"

// emitArchives emits one AR node per `ARCHIVE(NAME <out> [DONTCOMPRESS]
// files...)` declaration in `d.archives`. The node invokes the host
// archiver binary (resolved by walking tools/archiver as a host PROGRAM)
// to pack the listed files into the named output.
//
// Reference cmd_args shape (sg2.json):
//
//	$(BUILD_ROOT)/tools/archiver/archiver -q -x [-p] <file1>: [<file2>:] -o <NAME-absolute>
//
// Each archived file is rendered with a trailing colon (`${suf=\:;input}`
// in the upstream macro) and resolved to its BUILD_ROOT-absolute path when
// it names a PR-produced artifact in the same module; otherwise it is
// treated as a SOURCE_ROOT-relative path and rendered as
// `$(SOURCE_ROOT)/<modulePath>/<file>`.
//
// Inputs: the PR-produced files (as BUILD_ROOT paths), the archiver tool
// path, the producer PR's IN files (rebased to SOURCE_ROOT). Deps: the
// producer PR's NodeRef + the archiver LD's NodeRef.
func emitArchives(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.archives) == 0 {
		return
	}

	// Walk the archiver as a host program to resolve its binary path + LD
	// ref. Mirrors emitRunProgram's tool-walk shape; archiver lives at
	// tools/archiver and is hard-pinned by const archiverToolPath above.
	toolInstance := NewToolInstance(ctx.host, archiverToolPath, instance.Language)
	toolInstance.Flags = inferFlagsFromPath(archiverToolPath, true)

	var (
		toolBinPath = "$(BUILD_ROOT)/" + archiverToolPath + "/archiver"
		toolLDRef   NodeRef
	)
	if exc := Try(func() {
		res := genModule(ctx, toolInstance)
		toolLDRef = res.LDRef
		if res.LDPath != "" {
			toolBinPath = res.LDPath
		}
	}); exc != nil {
		// Tool walk failure surfaces as a fallback path; matches
		// emitRunProgram's pattern (the build will still record the
		// node, but with the conventional binary path).
	}

	// Aggregate the SOURCE_ROOT-rooted IN files contributed by every PR
	// in this module — REF includes the full set of upstream sources
	// (e.g. `__res.py` + `sitecustomize.py`) in each ARCHIVE node's
	// `inputs[]`, not just the IN list of the PR that produced the
	// specifically-archived file. Sort + dedup once so every emitted
	// AR node sees a stable view.
	var prInSources []string
	{
		seen := map[string]struct{}{}
		for _, rp := range d.runPrograms {
			for _, f := range rp.INFiles {
				p := "$(SOURCE_ROOT)/" + instance.Path + "/" + f
				if _, dup := seen[p]; dup {
					continue
				}
				seen[p] = struct{}{}
				prInSources = append(prInSources, p)
			}
		}
		sort.Strings(prInSources)
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
	toolBinPath string,
	toolLDRef NodeRef,
	prInSources []string,
	emit Emitter,
	reg *CodegenRegistry,
) {
	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + a.Name

	// Build cmd_args. Each archived file is rendered with a trailing
	// colon per upstream `${suf=\:;input:Files}`.
	cmdArgs := make([]string, 0, 4+len(a.Files)+2)
	cmdArgs = append(cmdArgs, toolBinPath, "-q", "-x")
	if a.DontCompress {
		cmdArgs = append(cmdArgs, "-p")
	}

	// Track the unique producer PRs so we can wire deps to all of them
	// and add their BUILD_ROOT outputs to the AR's input set.
	producerRefs := []NodeRef{}
	producerSet := map[NodeRef]struct{}{}
	pathPerFile := make([]string, 0, len(a.Files))

	for _, f := range a.Files {
		// When the file matches a PR output of this module, resolve to
		// the producer's BUILD_ROOT-absolute path and record the PR
		// NodeRef for dep wiring. Otherwise treat as SOURCE_ROOT-
		// relative to the module dir.
		abs := "$(BUILD_ROOT)/" + instance.Path + "/" + f
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
		if !isPRProduced {
			abs = "$(SOURCE_ROOT)/" + instance.Path + "/" + f
		}
		pathPerFile = append(pathPerFile, abs)
		cmdArgs = append(cmdArgs, abs+":")
	}
	cmdArgs = append(cmdArgs, "-o", archivePath)

	// PR-M3-unpaired-got-closure: REF's AR inputs include every output
	// of an upstream PR producer that is lexicographically ≤ the
	// archive's explicitly-referenced file. Concretely, when ARCHIVE
	// references `sitecustomize.pyc` and the producing RUN_PROGRAM also
	// emits the lexically-earlier `__res.pyc`, REF lists __res.pyc in
	// the AR's `inputs[]` alongside the archived file; the inverse
	// (ARCHIVE on `__res.pyc`, sibling `sitecustomize.pyc`) does NOT
	// include the lexically-later sibling. The lexicographic gate keeps
	// the input ordering stable across multiple archives sharing a
	// producer.
	prSiblingOutputs := make([]string, 0)
	{
		// Largest archived file path (BUILD_ROOT-rooted) becomes the
		// upper bound — siblings strictly greater than this are
		// excluded.
		maxArchived := ""
		for _, p := range pathPerFile {
			if p > maxArchived {
				maxArchived = p
			}
		}
		seen := map[string]struct{}{}
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
			if !rpProduces && rp.StdoutFile != "" {
				if _, ok := producerSet[d.prOutputProducer[rp.StdoutFile]]; ok {
					rpProduces = true
				}
			}
			if !rpProduces {
				continue
			}
			collect := func(rel string) {
				p := "$(BUILD_ROOT)/" + instance.Path + "/" + rel
				if p > maxArchived {
					return
				}
				if _, dup := seen[p]; dup {
					return
				}
				seen[p] = struct{}{}
				prSiblingOutputs = append(prSiblingOutputs, p)
			}
			for _, f := range rp.OUTFiles {
				collect(f)
			}
			for _, f := range rp.OUTNoAutoFiles {
				collect(f)
			}
			if rp.StdoutFile != "" {
				collect(rp.StdoutFile)
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
	// pathPerFile ∪ prSiblingOutputs, sorted, so REF's
	// "alphabetical merge of producer outputs" shape lines up.
	buildRootSet := map[string]struct{}{}
	for _, p := range pathPerFile {
		buildRootSet[p] = struct{}{}
	}
	for _, p := range prSiblingOutputs {
		buildRootSet[p] = struct{}{}
	}
	buildRootSorted := make([]string, 0, len(buildRootSet))
	for p := range buildRootSet {
		buildRootSorted = append(buildRootSorted, p)
	}
	sort.Strings(buildRootSorted)
	for _, p := range buildRootSorted {
		inputs = append(inputs, ParseVFSOrSource(p))
	}
	inputs = append(inputs, ParseVFSOrSource(toolBinPath))
	inSet := map[VFS]struct{}{}
	for _, p := range inputs {
		inSet[p] = struct{}{}
	}
	for _, p := range prInSources {
		v := ParseVFSOrSource(p)
		if _, dup := inSet[v]; dup {
			continue
		}
		inSet[v] = struct{}{}
		inputs = append(inputs, v)
	}

	depRefs := make([]NodeRef, 0, len(producerRefs)+1)
	depRefs = append(depRefs, producerRefs...)
	if toolLDRef != (NodeRef{}) {
		depRefs = append(depRefs, toolLDRef)
	}

	env := map[string]string{"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)"}

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
		Outputs:      []VFS{ParseVFSOrSource(archivePath)},
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

	// PR-M3-L0-cascade-close-v2: register the AR's output (the .pyc.inc
	// header for runtime_py3) in the codegen registry with the producer
	// NodeRef. Consumer CCs in the same module (e.g. __res.cpp) carry the
	// .pyc.inc path in their inputs[] via runtimePy3CCExtraInputs;
	// resolveCodegenDepRefs lifts the matching ProducerRef into the CC's
	// deps[]. EmitsIncludes is left nil — the .pyc.inc content is a
	// generator-tool output (RESOURCE-packed C array), not C-readable.
	if reg != nil {
		reg.Register(&GeneratedFileInfo{
			ProducerKvP:    "AR",
			OutputPath:     archivePath,
			ProducerRef:    arRef,
			HasProducerRef: true,
		})
	}
}

// isCCSourceExt reports whether `path` names a CC-compilable source.
// PR-AUDIT-5 dispatch helper: PR outputs with these extensions become
// implicit module sources. The extension set mirrors emitOneSource's
// .c/.cpp/.cc/.cxx branch; .S/.s/.asm are excluded — PR currently
// produces no assembly outputs in any observed closure, and the AS
// path has its own toolchain prerequisites (yasm walk) that don't
// trivially compose from a PR-driven scaffold.
func isCCSourceExt(p string) bool {
	return strings.HasSuffix(p, ".cpp") ||
		strings.HasSuffix(p, ".cc") ||
		strings.HasSuffix(p, ".cxx") ||
		strings.HasSuffix(p, ".c")
}

// emitPRDownstreamCC emits the CC node compiling a PR-generated source.
// Mirrors the R6/EV/CF downstream-CC shape (gen.go:4196..4236, PR-AUDIT-2):
// IsGenerated=true; IncludeInputs from WalkBuildRootClosure over the
// registered output (the registry entry is populated by emitRunProgram
// itself with EmitsIncludes=nil — opaque tool output — so the closure
// is empty unless a future PR-AUDIT iteration populates it).
//
// The PR-emitted source lives at $(BUILD_ROOT)/<instance.Path>/<out>;
// composeCCPaths' IsGenerated branch yields $(BUILD_ROOT)/<instance.
// Path>/<out>.o for the output (flat layout when <out> has no `/`).
func emitPRDownstreamCC(ctx *genCtx, instance ModuleInstance, out string, prRef NodeRef, in ModuleCCInputs) (NodeRef, VFS, []VFS) {
	// PR-M3-L0-cascade-close-v2: thread prRef as the downstream CC's
	// leading dep. The CC compiles the PR-emitted .cpp, but walkClosure
	// skips the root path so a registry probe over the closure alone
	// can't surface the PR producer. REF places PR as the CC's leading
	// dep (dep_types.h_dumper.cpp.o → PR dep_types.h_dumper.cpp).
	return emitCodegenDownstreamCC(ctx, instance, out, nil, []NodeRef{prRef}, in)
}

// emitCodegenDownstreamCC emits the downstream CC for a codegen producer's
// `.cpp/.cc/.cxx/.c` output. PR-M3-codegen-cc-enqueue generalises
// emitPRDownstreamCC to every codegen kind whose .cpp output lives under
// $(BUILD_ROOT)/<instance.Path>/<cppRel> AND whose owning module compiles
// that source as an implicit AR member (EN's `.h_serialized.cpp`, PR's
// STDOUT/OUT .cpp, etc.). The shape mirrors the existing PR/R6/R5/EV
// downstream-CC pattern:
//
//   - IsGenerated=true so composeCCPaths roots the input/output under
//     $(BUILD_ROOT)/<instance.Path>/<cppRel>{,.o,.pic.o} (flat layout for
//     a slash-free cppRel; `_/<cppRel>` infix when cppRel contains `/`).
//   - IncludeInputs from walkClosure() rooted at the codegen .cpp's
//     registered VFS path — the producer (EN/PR/EV/...) must have
//     registered EmitsIncludes for the .cpp in the per-scanner codegen
//     registry before this helper runs.
//   - depPrefix prepends cross-codegen dependency entries that the
//     reference graph places ahead of the primary source (EN consumers
//     prepend cross-EN deps' `_serialized.cpp` + `_serialized.h` outputs;
//     PR has no cross-deps and passes nil).
func emitCodegenDownstreamCC(ctx *genCtx, instance ModuleInstance, cppRel string, depPrefix []VFS, depRefs []NodeRef, in ModuleCCInputs) (NodeRef, VFS, []VFS) {
	cppPath := Build(instance.Path + "/" + cppRel)

	closure := walkClosure(ctx, instance, cppPath, in)

	// PR-M3-codegen-cc-enqueue: prepend depPrefix into IncludeInputs so
	// the resulting CC node's Inputs[] carries the cross-codegen dep
	// paths the reference graph places ahead of the consumer's own
	// generated .cpp (sg2.json export_json_debug.h_serialized.cpp.o
	// inputs[0..1] = the cross-EN dep `.cpp` + `.h` outputs). Dedup
	// against the scanner closure — the cross-EN `.h` is already in the
	// closure via the codegen registry's `_serialized.h` EmitsIncludes
	// chain; only the cross-EN `.cpp` reliably needs the explicit prepend.
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

	// PR-M3-L0-codegen-deps-EV-PB: append codegen-producer refs reached via
	// the .cpp's transitive include closure (PB/EV peers an EN-downstream CC
	// pulls in by including .pb.h / .ev.pb.h). Filter out anything already in
	// depRefs (cross-EN dep set) so we don't duplicate.
	extra := resolveCodegenDepRefs(ctx, instance, includeInputs, depRefs...)
	if len(extra) > 0 {
		ccIn.ExtraDepRefs = append(append([]NodeRef(nil), depRefs...), extra...)
	}

	ref, outPath := EmitCC(instance, cppRel, ccIn, ctx.emit)

	// AR member-inputs: only the SOURCE_ROOT-rooted closure entries.
	// PR-35y R7 / PR-M3-codegen-cc-enqueue: the AR aggregator excludes
	// BUILD_ROOT-staged generated `.cpp` and the cross-codegen dep
	// prefix from its `inputs[]` slot (the reference graph for
	// devtools/ymake's `libdevtools-ymake.a` carries header `.h` entries
	// from the EN-downstream CC's include closure but never the BUILD_
	// ROOT-rooted `.h_serialized.cpp` / `.h_serialized.h` source
	// outputs themselves — those are wired implicitly via the .o
	// archive members). Filter BUILD_ROOT entries here so the AR
	// inputs[] aggregate aligns with REF's multiset shape.
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
func emitRunProgram(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, d *moduleData, reg *CodegenRegistry, moduleInputs ModuleCCInputs) NodeRef {
	// Walk the tool as a host program.
	toolPath := filepath.Clean(stmt.ToolPath)
	toolInstance := NewToolInstance(ctx.host, toolPath, instance.Language)
	toolInstance.Flags = inferFlagsFromPath(toolPath, true)

	var toolBinPath string
	var toolLDRef NodeRef
	var toolInducedDeps []string

	if exc := Try(func() {
		res := genModule(ctx, toolInstance)
		toolLDRef = res.LDRef
		toolBinPath = res.LDPath
		toolInducedDeps = res.InducedDeps
	}); exc != nil {
		// Swallow parse errors (tool may not fully parse); use fallback path.
		toolBinPath = "$(BUILD_ROOT)/" + toolPath + "/" + filepath.Base(toolPath)
	}

	// F-7-B: register PR outputs FIRST so the closure walk below can resolve
	// each output's $(BUILD_ROOT) path through the codegen registry. PR outputs
	// are generated files (possibly .h headers) but their include content is
	// tool-specific and opaque at gen time.
	//
	// PR-AUDIT-5: for CC-compilable outputs (.cpp/.cc/.cxx/.c) we know the
	// generator-tool convention: the emitted source textually `#include`s
	// its IN files (which are the headers the tool was asked to read).
	// Populate EmitsIncludes with the SOURCE_ROOT-rooted IN paths so the
	// downstream CC's WalkBuildRootClosure picks up the transitive header
	// closure. The OUTPUT_INCLUDES directive declares additional textual
	// `#include`s the generator produces; thread those in too. For non-
	// compilable outputs (.h, .pyc, ...) EmitsIncludes stays nil — those
	// either lead to opaque binary content or to header content the
	// scanner cannot derive at gen time without invoking the tool.
	//
	// PR-M3-runprogram-closure: the tool's module-level INDUCED_DEPS(<ext>
	// headers...) names headers the tool injects into every generated
	// output of the listed extensions. Treat the listed headers as
	// additional EmitsIncludes for the PR output so the scanner reaches
	// the tool-injected header closure (e.g. struct2fieldcalc declares
	// INDUCED_DEPS(h+cpp .../field_calc_int.h), the scanner then walks
	// field_calc_int.h → field_calc.h → autoarray.h).
	if reg != nil {
		for _, f := range stmt.OUTFiles {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PR",
				OutputPath:    "$(BUILD_ROOT)/" + instance.Path + "/" + f,
				EmitsIncludes: prEmitsIncludes(instance, f, stmt, toolInducedDeps),
			})
		}
		for _, f := range stmt.OUTNoAutoFiles {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PR",
				OutputPath:    "$(BUILD_ROOT)/" + instance.Path + "/" + f,
				EmitsIncludes: prEmitsIncludes(instance, f, stmt, toolInducedDeps),
			})
		}
		if stmt.StdoutFile != "" {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PR",
				OutputPath:    "$(BUILD_ROOT)/" + instance.Path + "/" + stmt.StdoutFile,
				EmitsIncludes: prEmitsIncludes(instance, stmt.StdoutFile, stmt, toolInducedDeps),
			})
		}
	}

	// PR-M3-G-3: fold the transitive include closure of each CC-compilable
	// output into THIS PR node's `inputs`. REF's PR node `inputs` carry the
	// full closure (e.g. devtools/ymake/symbols's `dep_types.h_dumper.cpp`
	// PR node holds 1500 entries — every transitively-reachable header from
	// the generator output via the registered EmitsIncludes set). The
	// closure walk is driven by the registry: each output's EmitsIncludes
	// is the SOURCE_ROOT-rooted IN/OUTPUT_INCLUDES headers; the scanner
	// then follows real `#include` directives in those headers via
	// parseIncludes. Non-CC outputs (.h/.pyc) have nil EmitsIncludes and
	// would walk to nothing — skip them.
	inputClosure := prInputClosure(ctx, instance, stmt, moduleInputs)

	// PR-M3-L0-cascade-close-v2: resolve codegen-producer refs reached
	// through the PR's inputClosure. The PR node's deps[] must include any
	// cross-module EN/PB/EV/... producer whose generated header appears in
	// the PR's transitive input set (e.g. devtools/ymake/symbols's
	// dep_types.h_dumper.cpp PR depends on devtools/ymake/diag's EN
	// stats_enums.h_serialized.cpp via dep_types.h → stats_enums.h closure).
	prExtraDepRefs := resolveCodegenDepRefs(ctx, instance, inputClosure, toolLDRef)

	prRef := EmitPR(instance, stmt, toolBinPath, toolLDRef, inputClosure, prExtraDepRefs, ctx.emit)

	// PR-M3-L0-cascade-close-v2: backfill the PR ProducerRef so the
	// downstream CC's resolveCodegenDepRefs threads PR into its deps[].
	// The registry entries were created above with HasProducerRef=false
	// (NodeRef not yet known); SetProducerRef fills it in atomically.
	if reg != nil {
		for _, f := range stmt.OUTFiles {
			reg.SetProducerRef("$(BUILD_ROOT)/"+instance.Path+"/"+f, prRef)
		}
		for _, f := range stmt.OUTNoAutoFiles {
			reg.SetProducerRef("$(BUILD_ROOT)/"+instance.Path+"/"+f, prRef)
		}
		if stmt.StdoutFile != "" {
			reg.SetProducerRef("$(BUILD_ROOT)/"+instance.Path+"/"+stmt.StdoutFile, prRef)
		}
	}

	return prRef
}

// prInputClosure returns the union of transitive include closures of every
// CC-compilable PR output (OUT / OUT_NOAUTO / STDOUT). For each such output
// the scanner walks the registered EmitsIncludes (SOURCE_ROOT-rooted IN +
// OUTPUT_INCLUDES headers) and follows real `#include` directives from
// there. PR outputs whose extension is not CC-compilable (.h / .pyc / ...)
// have nil EmitsIncludes and contribute nothing.
//
// PR-M3-G-3 helper.
func prInputClosure(ctx *genCtx, instance ModuleInstance, stmt *RunProgramStmt, moduleInputs ModuleCCInputs) []VFS {
	// Use the consuming module's full scan-input bag (AddIncl +
	// PeerAddInclGlobal) so peer headers reachable from the PR-output's
	// EmitsIncludes chain resolve correctly. Mirrors the EN-node scanner
	// configuration in emitEnumSrcs (gen.go).
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
	if stmt.StdoutFile != "" && isCCSourceExt(stmt.StdoutFile) {
		walkOne(stmt.StdoutFile)
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
func prEmitsIncludes(instance ModuleInstance, outFile string, stmt *RunProgramStmt, toolInducedDeps []string) []string {
	if !isCCSourceExt(outFile) {
		return nil
	}

	includes := make([]string, 0, len(stmt.INFiles)+len(stmt.OutputIncludes)+len(toolInducedDeps))

	// IN files are module-relative; rebase to SOURCE_ROOT.
	for _, f := range stmt.INFiles {
		includes = append(includes, "$(SOURCE_ROOT)/"+instance.Path+"/"+f)
	}

	// OUTPUT_INCLUDES entries are repo-relative (e.g.
	// `devtools/ymake/symbols/file_store.h`); rebase to SOURCE_ROOT.
	for _, f := range stmt.OutputIncludes {
		includes = append(includes, "$(SOURCE_ROOT)/"+f)
	}

	// Tool-declared INDUCED_DEPS (repo-relative); rebase to SOURCE_ROOT.
	for _, f := range toolInducedDeps {
		includes = append(includes, "$(SOURCE_ROOT)/"+f)
	}

	return includes
}
