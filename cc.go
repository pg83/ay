package main

// cc.go — emitter for CC compilation nodes.
//
// PR-23 retrofitted the signature: `EmitCC` now takes a
// `ModuleInstance` instead of a (PlatformConfig, moduleDir) pair.
// The instance carries Path / Language / Target / Flags; rule
// composition keys off `Flags.PIC` (host vs target) and a path-prefix
// match for musl modules. PR-25 will replace the path prefix with a
// real flavour bit on `FlagSet`.
//
// Output path convention is unchanged from PR-12:
//
//   - Flat source: `$(BUILD_ROOT)/<path>/<srcRel><.o|.pic.o>`
//   - Nested source (contains "/"): `$(BUILD_ROOT)/<path>/_/<srcRel><.o|.pic.o>`
//
// Suffix is `.o` for target builds, `.pic.o` for host (Flags.PIC=true).
//
// Three flavours of cmd_args composition:
//
//   - target-default (`commonCFlags` + `noLibcUndebugBlock` × 2 +
//     `catboostOpenSourceDefine` between): 101 args. Pinned byte-exact
//     against `build/cow/on/lib.c.o`.
//   - host-PIC (`hostCFlags` + `ndebugPicBlock` × 2 +
//     `catboostOpenSourceDefine` + `hostSseFeatures` between): 105
//     args. Pinned byte-exact against `build/cow/on/lib.c.pic.o`.
//   - musl (`muslCcIncludes` + `muslWarningFlags` + `muslExtraDefines`
//     + same no-libc tail): 111 args. Pinned byte-exact against
//     `contrib/libs/musl/...` via PR-14's bundle additions; PR-23 only
//     reaffirms the route, no new test until PR-25 wires the walker
//     into musl.

import (
	"strings"
)

// EmitCC emits a CC node for compiling `srcRel` (a path relative to
// `instance.Path`, e.g. "lib.c" or "src/algorithm.cpp") into an
// object file. Returns the NodeRef so callers (typically the AR step)
// can wire it as a dependency, plus the output path so callers do
// not have to re-derive it (PR-10-D03).
//
// The composed cmd_args length is 101 / 105 / 111 depending on the
// flavour; reviewer-tracked tests pin each variant against the
// reference graph.
func EmitCC(instance ModuleInstance, srcRel string, emit Emitter) (NodeRef, string) {
	suffix := ".o"
	if instance.Flags.PIC {
		suffix = ".pic.o"
	}

	var outputPath string
	if strings.Contains(srcRel, "/") {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/_/" + srcRel + suffix
	} else {
		outputPath = "$(BUILD_ROOT)/" + instance.Path + "/" + srcRel + suffix
	}

	inputPath := "$(SOURCE_ROOT)/" + instance.Path + "/" + srcRel

	isMusl := instance.Path == "contrib/libs/musl" || strings.HasPrefix(instance.Path, "contrib/libs/musl/")

	var cmdArgs []string

	switch {
	case isMusl:
		cmdArgs = composeMuslCC(srcRel, outputPath, inputPath, instance.Path)
	case instance.Flags.PIC:
		cmdArgs = composeHostCC(outputPath, inputPath)
	default:
		cmdArgs = composeTargetCC(outputPath, inputPath)
	}

	// The reference graph carries the same env map at both the cmd
	// level and the top level of the Node. Build it once and reuse;
	// EmitCC is single-shot so the alias is safe today. Future PRs
	// that mutate emitted nodes post-emit MUST clone before mutating.
	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
		"DYLD_LIBRARY_PATH":      "$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu",
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  []string{inputPath},
		Outputs: []string{outputPath},
		KV: map[string]string{
			"p":  "CC",
			"pc": "green",
		},
		Tags: []string{},
		TargetProperties: map[string]string{
			"module_dir": instance.Path,
		},
		Platform: string(instance.Target),
		// Numeric values are stored as float64 to match what
		// encoding/json produces when unmarshalling the reference
		// graph into `map[string]interface{}` (Go's default JSON-
		// number type for `interface{}` targets). Constructing with
		// int literals would make a comparator using
		// reflect.DeepEqual against the reference fail spuriously
		// even though the on-disk JSON is identical.
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
	}

	if instance.Flags.PIC {
		// Host build: reference nodes carry `host_platform=true`
		// and `tags=["tool"]`. The "tool" tag distinguishes host
		// nodes that are built specifically to be invoked at
		// build-time (per the reference graph's classification).
		node.HostPlatform = true
		node.Tags = []string{"tool"}
	}

	return emit.Emit(node), outputPath
}

// composeTargetCC composes the 101-arg cmd_args bundle for a TARGET-
// flavoured no-libc CC compilation. Pinned byte-exact against
// build/cow/on/lib.c.o in /home/pg/monorepo/yatool_orig/g.json.
func composeTargetCC(outputPath, inputPath string) []string {
	cmdArgs := make([]string, 0, 101)
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludes...)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, warningFlags...)
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}

// composeHostCC composes the 105-arg cmd_args bundle for a HOST-
// flavoured PIC CC compilation. Pinned byte-exact against
// build/cow/on/lib.c.pic.o in /home/pg/monorepo/yatool_orig/g.json.
//
// Differs from target in:
//   - No `-march=` (host is generic x86_64; the architecture is
//     captured by `-m64` inside hostCFlags instead).
//   - Release-flavoured: `-O3` in hostCFlags (vs target's `-g`).
//   - `-fPIC` and `-DNDEBUG` (vs target's `-UNDEBUG`).
//   - Adds `-D_YNDX_LIBUNWIND_ENABLE_EXCEPTION_BACKTRACE` to the
//     define block (host libunwind shim).
//   - Inserts `hostSseFeatures` (7 args) between the two ndebugPicBlock
//     copies, in addition to `catboostOpenSourceDefine`.
func composeHostCC(outputPath, inputPath string) []string {
	cmdArgs := make([]string, 0, 105)
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+hostTriple,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, ccIncludes...)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, hostCFlags...)
	cmdArgs = append(cmdArgs, warningFlags...)
	cmdArgs = append(cmdArgs, hostDefines...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, hostSseFeatures...)
	cmdArgs = append(cmdArgs, ndebugPicBlock...)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}

// composeMuslCC composes the 111-arg cmd_args bundle for a
// `contrib/libs/musl/...` CC compilation. Differs from target in:
//   - `muslCcIncludes` (10 args) replaces `ccIncludes` (4 args)
//   - `muslWarningFlags` (1 arg) replaces `warningFlags` (6 args)
//   - `muslExtraDefines` (9 args) inserted after `commonDefines`,
//     before the noLibc block
//
// Net delta: +6 +(-5) +9 = +10 args. 101 + 10 = 111.
//
// Note: PR-23 declares the function but does not have a byte-exact
// test for it (musl modules are not in PR-23's acceptance scope —
// PR-25 wires the walker into musl, where this composition gets
// regression-pinned).
func composeMuslCC(srcRel, outputPath, inputPath, modulePath string) []string {
	_ = srcRel
	_ = modulePath

	cmdArgs := make([]string, 0, 111)
	cmdArgs = append(cmdArgs,
		ccCompilerPath,
		"--target="+targetTriple,
		"-march="+archFlag,
		"-B"+binPath,
		"-c",
		"-o",
		outputPath,
	)
	cmdArgs = append(cmdArgs, muslCcIncludes...)
	cmdArgs = append(cmdArgs, debugPrefixMapFlags...)
	cmdArgs = append(cmdArgs, xclangDebugCompilationDir...)
	cmdArgs = append(cmdArgs, commonCFlags...)
	cmdArgs = append(cmdArgs, muslWarningFlags...)
	cmdArgs = append(cmdArgs, commonDefines...)
	cmdArgs = append(cmdArgs, muslExtraDefines...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, catboostOpenSourceDefine...)
	cmdArgs = append(cmdArgs, noLibcUndebugBlock...)
	cmdArgs = append(cmdArgs, builtinMacroDateTime...)
	cmdArgs = append(cmdArgs, macroPrefixMapFlags...)
	cmdArgs = append(cmdArgs, inputPath)

	return cmdArgs
}
