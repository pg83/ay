package main

import (
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

// ─── JV ──────────────────────────────────────────────────────────────────────

// ─── CF ──────────────────────────────────────────────────────────────────────

// ─── BI ──────────────────────────────────────────────────────────────────────

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
