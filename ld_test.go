package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ld_test.go — byte-exact regression test for EmitLD against the
// reference graph for `tools/archiver/archiver` (the M2 PROGRAM
// target), plus a synthetic structural test.
//
// Strategy mirrors cc_test.go / as_test.go: the test does its own
// os.ReadFile + json.Unmarshal of the reference graph, locates the LD
// node by output path, and compares EmitLD's output field-by-field
// (UID/SelfUID/StatsUID excluded, since they are Finalize-computed).
//
// Inputs to EmitLD are reconstructed from the reference: peer
// archive paths in PEERDIR walk order extracted from cmd[2]'s
// `--start-group ... --end-group` block; pluginPaths and globalPaths
// extracted from the same cmd. Stub NodeRefs are wired through a
// fresh BufferedEmitter so that the LD node can refer to them as
// DepRefs without a real PEERDIR walk (which is PR-25's job — PR-24
// just lands the rule).

// referenceLDOutput is the output path used to locate the reference
// LD node for `tools/archiver`.
const referenceLDOutput = "$(BUILD_ROOT)/tools/archiver/archiver"

// archiverPeerLibPaths are the 32 peer LIBRARY archive paths in the
// exact PEERDIR walk order observed in the reference graph's cmd[2].
// The order is NON-ALPHABETICAL (R14): it mirrors the recursive
// PEERDIR-declaration walk that PR-25 will reproduce. PR-24 uses this
// list verbatim to bypass the walker and pin EmitLD's output byte-
// exact.
var archiverPeerLibPaths = []string{
	"contrib/libs/cxxsupp/libcxxabi-parts/liblibs-cxxsupp-libcxxabi-parts.a",
	"contrib/libs/libunwind/libcontrib-libs-libunwind.a",
	"contrib/libs/cxxsupp/libcxxrt/liblibs-cxxsupp-libcxxrt.a",
	"contrib/libs/cxxsupp/builtins/liblibs-cxxsupp-builtins.a",
	"contrib/libs/cxxsupp/libcxx/liblibs-cxxsupp-libcxx.a",
	"util/charset/libutil-charset.a",
	"contrib/libs/zlib/libcontrib-libs-zlib.a",
	"contrib/libs/double-conversion/libcontrib-libs-double-conversion.a",
	"contrib/libs/libc_compat/libcontrib-libs-libc_compat.a",
	"contrib/libs/linuxvdso/original/liblibs-linuxvdso-original.a",
	"contrib/libs/linuxvdso/libcontrib-libs-linuxvdso.a",
	"util/libyutil.a",
	"build/cow/on/libbuild-cow-on.a",
	"library/cpp/malloc/api/libcpp-malloc-api.a",
	"contrib/restricted/abseil-cpp/libcontrib-restricted-abseil-cpp.a",
	"contrib/libs/tcmalloc/malloc_extension/liblibs-tcmalloc-malloc_extension.a",
	"library/cpp/malloc/tcmalloc/libcpp-malloc-tcmalloc.a",
	"contrib/libs/tcmalloc/no_percpu_cache/liblibs-tcmalloc-no_percpu_cache.a",
	"contrib/libs/musl/libcontrib-libs-musl.a",
	"contrib/libs/musl/full/liblibs-musl-full.a",
	"library/cpp/archive/liblibrary-cpp-archive.a",
	"contrib/libs/nayuki_md5/libcontrib-libs-nayuki_md5.a",
	"contrib/libs/base64/avx2/liblibs-base64-avx2.a",
	"contrib/libs/base64/ssse3/liblibs-base64-ssse3.a",
	"contrib/libs/base64/neon32/liblibs-base64-neon32.a",
	"contrib/libs/base64/neon64/liblibs-base64-neon64.a",
	"contrib/libs/base64/plain32/liblibs-base64-plain32.a",
	"contrib/libs/base64/plain64/liblibs-base64-plain64.a",
	"library/cpp/string_utils/base64/libcpp-string_utils-base64.a",
	"library/cpp/digest/md5/libcpp-digest-md5.a",
	"library/cpp/colorizer/liblibrary-cpp-colorizer.a",
	"library/cpp/getopt/small/libcpp-getopt-small.a",
}

// archiverPluginPaths is the single plugin (musl pyplugin) referenced
// by the archiver LD's `--start-plugins ... --end-plugins` block.
var archiverPluginPaths = []string{
	"$(BUILD_ROOT)/contrib/libs/musl/include/musl.py.pyplugin",
}

// archiverGlobalPaths is the single global archive (tcmalloc no-percpu
// global) referenced by the archiver LD's `-Wl,--whole-archive`
// section. BUILD_ROOT-relative (no $(BUILD_ROOT)/ prefix — link_exe.py
// resolves these against `cwd: $(BUILD_ROOT)`).
var archiverGlobalPaths = []string{
	"contrib/libs/tcmalloc/no_percpu_cache/liblibs-tcmalloc-no_percpu_cache.global.a",
}

// loadReferenceLDNode reads the on-disk reference graph and returns the
// LD node whose first output is referenceLDOutput. Returns nil and a
// reason string when the file is absent (so the caller can t.Skip) or
// when the node is missing.
func loadReferenceLDNode(t *testing.T) (*Node, string) {
	t.Helper()

	raw, err := os.ReadFile(referenceGraphPath)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, "reference graph " + referenceGraphPath + " not present on this host"
		}

		t.Fatalf("read %s: %v", referenceGraphPath, err)
	}

	var g Graph
	Throw(json.Unmarshal(raw, &g))

	for _, n := range g.Graph {
		if len(n.Outputs) > 0 && n.Outputs[0] == referenceLDOutput && n.KV["p"] == "LD" {
			return n, ""
		}
	}

	return nil, "reference graph contains no LD node with output " + referenceLDOutput
}

// TestEmitLD_ToolsArchiver_ByteExact pins the 4-cmd LD node against
// the reference graph entry for `tools/archiver/archiver`. The test
// supplies the peer-lib / plugin / global / cc paths directly (not
// via Gen's recursion — that lands in PR-25). Stub NodeRefs are
// minted via fresh BufferedEmitter Emit calls so the DepRef wiring
// is exercised end-to-end.
func TestEmitLD_ToolsArchiver_ByteExact(t *testing.T) {
	ref, skipReason := loadReferenceLDNode(t)

	if ref == nil {
		t.Skip(skipReason)
	}

	emit := NewBufferedEmitter()

	// Stub the dep-tree nodes the LD references. The actual node
	// content is irrelevant — we only need stable NodeRefs whose
	// existence makes Finalize's checkRef pass. These stubs are
	// distinct (each Emit increments the id counter) so DepRef
	// length and ordering are exercised.
	stub := func() NodeRef {
		return emit.Emit(&Node{
			KV:      map[string]string{"p": "STUB"},
			Tags:    []string{},
			Outputs: []string{},
			Inputs:  []string{},
		})
	}

	// One CC ref for main.cpp.o.
	mainCCRef := stub()
	mainCCPath := "$(BUILD_ROOT)/tools/archiver/main.cpp.o"

	// 32 peer LD/AR refs in PEERDIR walk order.
	peerLDRefs := make([]NodeRef, len(archiverPeerLibPaths))
	for i := range archiverPeerLibPaths {
		peerLDRefs[i] = stub()
	}

	// 1 plugin ref (musl pyplugin CP node).
	pluginRefs := []NodeRef{stub()}

	// 1 global archive ref (tcmalloc global AR node).
	globalRefs := []NodeRef{stub()}

	instance := targetInstance("tools/archiver")

	// PR-31 D11: derive memberInputs from the reference inputs.
	// The reference's input order is archives + .o + 7 scripts +
	// member CC inputs (source + headers). The script bundle
	// signature is the trailing bundle's last entry `fs_tools.py`;
	// everything after it is member inputs.
	//
	// PR-35v: svnversion.h is the last entry in the reference inputs
	// (index 1051). EmitLD now injects it unconditionally as a static
	// input after the member-CC union; the caller (gen.go) does NOT
	// include it in memberInputs. Strip it from the reference-derived
	// slice so the test exercises the static injection path rather
	// than the dedup-passthrough path.
	const fsToolsScript = "$(SOURCE_ROOT)/build/scripts/fs_tools.py"
	var refMemberInputs []string

	for i, p := range ref.Inputs {
		if p == fsToolsScript {
			raw := ref.Inputs[i+1:]
			refMemberInputs = make([]string, 0, len(raw))

			for _, mp := range raw {
				if mp == ldSvnversionHInput {
					continue
				}

				refMemberInputs = append(refMemberInputs, mp)
			}

			break
		}
	}

	ldRef := EmitLD(
		instance,
		"archiver",
		[]NodeRef{mainCCRef}, []string{mainCCPath},
		peerLDRefs, archiverPeerLibPaths,
		pluginRefs, archiverPluginPaths,
		globalRefs, archiverGlobalPaths,
		nil, nil, // PR-M3-py3cc-objcopy-shape: archiver has no SRCS_GLOBAL objcopy
		refMemberInputs,
		true,  // PR-32 D10: archiver pin runs MUSL=yes (M2 default)
		nil,   // PR-38: moduleCFlags — nil for target PROGRAM (archiver has no own CFLAGS)
		nil,   // PR-M3-final-LD-trailer-and-cflags: peerCFlagsGlobal — nil for archiver (no peer GLOBAL CFLAGS)
		false, // PR-M3-final-LD-trailer-and-cflags: usePython3 — false for archiver
		emit,
	)

	if ldRef.id < 0 {
		t.Fatalf("EmitLD returned invalid NodeRef %v", ldRef)
	}

	got := emit.nodes[ldRef.id]

	// Headline acceptance: 4 cmds, exact arg lengths.
	if len(got.Cmds) != 4 {
		t.Fatalf("got %d Cmds, want 4", len(got.Cmds))
	}

	wantArgLens := []int{5, 94, 73, 5}
	for i, want := range wantArgLens {
		if len(got.Cmds[i].CmdArgs) != want {
			t.Errorf("cmd[%d].cmd_args length = %d, want %d", i, len(got.Cmds[i].CmdArgs), want)
		}
	}

	if t.Failed() {
		t.FailNow()
	}

	// Walk every cmd's cmd_args entry-by-entry so a mismatch
	// reports the offending (cmd, index) pair.
	for ci := 0; ci < 4; ci++ {
		wantArgs := ref.Cmds[ci].CmdArgs
		gotArgs := got.Cmds[ci].CmdArgs

		for i := range wantArgs {
			if i >= len(gotArgs) {
				t.Errorf("cmd[%d].cmd_args[%d]: got (missing), want %q", ci, i, wantArgs[i])

				continue
			}

			if gotArgs[i] != wantArgs[i] {
				t.Errorf("cmd[%d].cmd_args[%d]:\n  got:  %q\n  want: %q", ci, i, gotArgs[i], wantArgs[i])
			}
		}

		fieldEqual(t, ldFieldName(ci, "env"), got.Cmds[ci].Env, ref.Cmds[ci].Env)

		if got.Cmds[ci].Cwd != ref.Cmds[ci].Cwd {
			t.Errorf("cmd[%d].cwd: got %q, want %q", ci, got.Cmds[ci].Cwd, ref.Cmds[ci].Cwd)
		}
	}

	// Hard-pinned to the reference inputs count. PR-35b closed
	// PR-31-D09: composeLDInputs now interleaves peer-archive paths
	// with plugins, global archives, and own .o (35-entry alphabetical
	// BUILD_ROOT block: 32 peers + 1 plugin + 1 global + 1 own.o) +
	// 7 scripts + 1010 member-CC inputs = 1052 = reference exactly.
	const wantInputCount = 1052

	if len(got.Inputs) != wantInputCount {
		t.Errorf("inputs count = %d, want %d", len(got.Inputs), wantInputCount)
	}
	fieldEqual(t, "outputs", got.Outputs, ref.Outputs)
	fieldEqual(t, "kv", got.KV, ref.KV)
	fieldEqual(t, "tags", got.Tags, ref.Tags)
	fieldEqual(t, "target_properties", got.TargetProperties, ref.TargetProperties)
	fieldEqual(t, "platform", got.Platform, ref.Platform)
	fieldEqual(t, "requirements", got.Requirements, ref.Requirements)
	fieldEqual(t, "env (top-level)", got.Env, ref.Env)

	if got.HostPlatform {
		t.Errorf("host_platform: got true, want false")
	}

	if ref.HostPlatform {
		t.Errorf("reference host_platform: got true, want false (sanity check)")
	}

	if got.ForeignDeps != nil {
		t.Errorf("foreign_deps: got %#v, want nil", got.ForeignDeps)
	}

	if ref.ForeignDeps != nil {
		t.Errorf("reference foreign_deps: got %#v, want nil (sanity check)", ref.ForeignDeps)
	}

	// DepRefs: own CC + 32 peers + 1 plugin + 1 global = 35.
	wantDepCount := 1 + len(archiverPeerLibPaths) + len(archiverPluginPaths) + len(archiverGlobalPaths)
	if len(got.DepRefs) != wantDepCount {
		t.Errorf("DepRefs: got %d, want %d", len(got.DepRefs), wantDepCount)
	}

	if len(ref.Deps) != wantDepCount {
		t.Errorf("reference deps: got %d, want %d (sanity check)", len(ref.Deps), wantDepCount)
	}
}

// ldFieldName builds a stable diagnostic name for a per-cmd field so
// fieldEqual's mismatch report identifies which cmd index is at fault
// without a freeform format string.
func ldFieldName(ci int, suffix string) string {
	switch ci {
	case 0:
		return "cmds[0]." + suffix
	case 1:
		return "cmds[1]." + suffix
	case 2:
		return "cmds[2]." + suffix
	case 3:
		return "cmds[3]." + suffix
	default:
		return "cmds[?]." + suffix
	}
}

// TestEmitLD_SyntheticPROGRAM verifies the structural shape of the LD
// node for a 1-source PROGRAM with zero PEERDIR. The synthetic case
// does not match any reference (no archiver-style peer closure or
// global archives), so the test asserts only that the four cmds are
// present and carry the expected scaffolding (vcs_info / clang
// compile / link_exe / fs_tools), the output path is correct, and the
// node carries the LD kv markers.
func TestEmitLD_SyntheticPROGRAM(t *testing.T) {
	emit := NewBufferedEmitter()
	mainRef := emit.Emit(&Node{
		KV: map[string]string{"p": "STUB"},
	})
	mainPath := "$(BUILD_ROOT)/some/prog/main.cpp.o"

	instance := targetInstance("some/prog")

	ldRef := EmitLD(
		instance,
		"", // empty falls back to lastPathComponent → "prog"
		[]NodeRef{mainRef}, []string{mainPath},
		nil, nil,
		nil, nil,
		nil, nil,
		nil, nil, // PR-M3-py3cc-objcopy-shape: objcopy slot
		nil,
		true,  // PR-32 D10: synthetic test pin runs MUSL=yes
		nil,   // PR-38: moduleCFlags nil for synthetic target test
		nil,   // PR-M3-final-LD-trailer-and-cflags: peerCFlagsGlobal nil
		false, // PR-M3-final-LD-trailer-and-cflags: usePython3 false
		emit,
	)

	got := emit.nodes[ldRef.id]

	if len(got.Cmds) != 4 {
		t.Fatalf("Cmds = %d, want 4", len(got.Cmds))
	}

	// cmd[0]: vcs_info.py
	if got.Cmds[0].CmdArgs[1] != "$(SOURCE_ROOT)/build/scripts/vcs_info.py" {
		t.Errorf("cmd[0] does not invoke vcs_info.py: %q", got.Cmds[0].CmdArgs[1])
	}

	// cmd[1]: clang. With -c -o.
	if got.Cmds[1].CmdArgs[0] != ccCompilerPath {
		t.Errorf("cmd[1][0] = %q, want %q", got.Cmds[1].CmdArgs[0], ccCompilerPath)
	}

	// cmd[2]: link_exe.py.
	if got.Cmds[2].CmdArgs[1] != "$(SOURCE_ROOT)/build/scripts/link_exe.py" {
		t.Errorf("cmd[2] does not invoke link_exe.py: %q", got.Cmds[2].CmdArgs[1])
	}

	if got.Cmds[2].Cwd != "$(BUILD_ROOT)" {
		t.Errorf("cmd[2].cwd = %q, want $(BUILD_ROOT)", got.Cmds[2].Cwd)
	}

	// cmd[3]: fs_tools.py
	if got.Cmds[3].CmdArgs[1] != "$(SOURCE_ROOT)/build/scripts/fs_tools.py" {
		t.Errorf("cmd[3] does not invoke fs_tools.py: %q", got.Cmds[3].CmdArgs[1])
	}

	// Output path: $(BUILD_ROOT)/some/prog/prog.
	wantOut := "$(BUILD_ROOT)/some/prog/prog"
	if len(got.Outputs) != 1 || got.Outputs[0] != wantOut {
		t.Errorf("outputs = %#v, want [%q]", got.Outputs, wantOut)
	}

	// Synthetic case has no plugins / no globals; cmd[2] should
	// not contain --start-plugins.
	for _, a := range got.Cmds[2].CmdArgs {
		if a == "--start-plugins" {
			t.Errorf("synthetic LD cmd[2] unexpectedly contains --start-plugins (no plugins supplied)")

			break
		}
	}

	// kv: p=LD, pc=light-blue, show_out=yes.
	wantKV := map[string]string{"p": "LD", "pc": "light-blue", "show_out": "yes"}
	for k, v := range wantKV {
		if got.KV[k] != v {
			t.Errorf("kv[%q] = %q, want %q", k, got.KV[k], v)
		}
	}

	// target_properties: module_dir + module_lang + module_type=bin.
	if got.TargetProperties["module_type"] != "bin" {
		t.Errorf("target_properties.module_type = %q, want bin", got.TargetProperties["module_type"])
	}

	// DepRefs: 1 (the single own .cpp.o).
	if len(got.DepRefs) != 1 {
		t.Errorf("DepRefs = %d, want 1", len(got.DepRefs))
	}
}

// TestEmitLD_AcceptsHostPIC verifies PR-25's lift of PR-24's
// host-PIC guard. Cross-platform recursion (D31) requires building
// host PROGRAM modules (ragel6/yasm), so EmitLD now accepts
// `Flags.PIC=true`. The cmd_args bundle is still the target shape;
// byte-exact host LD pinning is PR-26+ scope. This test only
// asserts the call no longer throws and the resulting node carries
// `host_platform=true`.
func TestEmitLD_AcceptsHostPIC(t *testing.T) {
	emit := NewBufferedEmitter()
	stub := emit.Emit(&Node{KV: map[string]string{"p": "STUB"}})

	ref := EmitLD(
		hostInstance("some/prog"),
		"", // empty falls back to lastPathComponent → "prog"
		[]NodeRef{stub}, []string{"$(BUILD_ROOT)/some/prog/main.cpp.o"},
		nil, nil,
		nil, nil,
		nil, nil,
		nil, nil, // PR-M3-py3cc-objcopy-shape: objcopy slot
		nil,
		true,  // PR-32 D10: host pin runs MUSL=yes (M2 default)
		nil,   // PR-38: moduleCFlags nil for synthetic host test
		nil,   // PR-M3-final-LD-trailer-and-cflags: peerCFlagsGlobal nil
		false, // PR-M3-final-LD-trailer-and-cflags: usePython3 false
		emit,
	)

	got := emit.nodes[ref.id]

	if got.Platform != string(PlatformDefaultLinuxX8664) {
		t.Errorf("platform = %q, want %q", got.Platform, PlatformDefaultLinuxX8664)
	}

	if !got.HostPlatform {
		t.Errorf("host_platform = false, want true")
	}

	if len(got.Tags) != 1 || got.Tags[0] != "tool" {
		t.Errorf("tags = %v, want [\"tool\"]", got.Tags)
	}
}

// TestEmitLD_LengthMismatchPanics verifies the precondition checks on all
// four ref/path slice pairs (cc, peerLD, plugin, global).
func TestEmitLD_LengthMismatchPanics(t *testing.T) {
	tests := []struct {
		name                                         string
		ccRefs, peerRefs, pluginRefs, globalRefs     []NodeRef
		ccPaths, peerPaths, pluginPaths, globalPaths []string
		wantSubstr                                   string
	}{
		{"ccRefs vs ccPaths", []NodeRef{{}}, nil, nil, nil, nil, nil, nil, nil, "ccRefs"},
		{"peerLDRefs vs peerLibPaths", nil, []NodeRef{{}}, nil, nil, nil, nil, nil, nil, "peerLD"},
		{"pluginRefs vs pluginPaths", nil, nil, []NodeRef{{}}, nil, nil, nil, nil, nil, "plugin"},
		{"globalRefs vs globalPaths", nil, nil, nil, []NodeRef{{}}, nil, nil, nil, nil, "global"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := NewBufferedEmitter()
			instance := targetInstance("test/prog")

			exc := Try(func() {
				EmitLD(instance, "prog", tc.ccRefs, tc.ccPaths, tc.peerRefs, tc.peerPaths, tc.pluginRefs, tc.pluginPaths, tc.globalRefs, tc.globalPaths, nil, nil, nil, true, nil, nil, false, e)
			})

			if exc == nil {
				t.Fatal("expected exception")
			}

			if !strings.Contains(exc.Error(), tc.wantSubstr) {
				t.Errorf("unexpected error: %v", exc)
			}
		})
	}
}
