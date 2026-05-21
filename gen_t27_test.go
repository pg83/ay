package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestGen_YaBinLinkTailMatchesReference(t *testing.T) {
	const targetDir = "devtools/ya/bin"

	if _, err := os.Stat(filepath.Join(sourceRoot, targetDir, "ya.make")); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("reference ya.make not present at %s/%s/ya.make", sourceRoot, targetDir)
		}

		t.Fatalf("stat ya.make: %v", err)
	}

	host := newT20ResourcePlatform(OSLinux, ISAX8664, "yes", []string{"tool"}, true)
	target := newT20ResourcePlatform(OSLinux, ISAAArch64, "no", nil, true)
	our := GenWithMode(sourceRoot, targetDir, host, target, defaultScanCtxMode, func(Warn) {})
	ourNode := findGraphNodeByOutputs(t, our, "$(B)/devtools/ya/bin/ya-bin", "$(B)/devtools/ya/bin/ya-bin.debug")
	refNode := loadT20RefNode(t, "$(BUILD_ROOT)/devtools/ya/bin/ya-bin", "$(BUILD_ROOT)/devtools/ya/bin/ya-bin.debug")

	if len(ourNode.Cmds) < 3 || len(refNode.Cmds) < 3 {
		t.Fatalf("expected both nodes to have at least 3 cmds")
	}

	gotTail := cmdArgsFrom(t, ourNode.Cmds[2].CmdArgs, "-Wl,--start-group")
	wantTail := normalizeT20Strings(cmdArgsFrom(t, refNode.Cmds[2].CmdArgs, "-Wl,--start-group"))

	if !reflect.DeepEqual(gotTail, wantTail) {
		t.Fatalf("ya-bin link tail mismatch:\n  got:  %#v\n  want: %#v", gotTail, wantTail)
	}

	anchor := "build/cow/on/libbuild-cow-on.a"
	wantAfterAnchor := []string{
		"library/cpp/malloc/api/libcpp-malloc-api.a",
		"contrib/libs/jemalloc/libcontrib-libs-jemalloc.a",
		"library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a",
	}
	anchorIdx := slices.Index(gotTail, anchor)
	if anchorIdx < 0 {
		t.Fatalf("expected %q in ya-bin link tail: %v", anchor, gotTail)
	}
	if anchorIdx+1+len(wantAfterAnchor) > len(gotTail) {
		t.Fatalf("expected %q to be followed by %v in ya-bin link tail: %v", anchor, wantAfterAnchor, gotTail)
	}
	if !slices.Equal(gotTail[anchorIdx+1:anchorIdx+1+len(wantAfterAnchor)], wantAfterAnchor) {
		t.Fatalf("expected %q to be followed by %v in ya-bin link tail: %v", anchor, wantAfterAnchor, gotTail)
	}

	enumRuntime := "tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a"
	jsonCommon := "library/cpp/json/common/libcpp-json-common.a"
	enumIdx := slices.Index(gotTail, enumRuntime)
	jsonIdx := slices.Index(gotTail, jsonCommon)
	if enumIdx < 0 || jsonIdx < 0 {
		t.Fatalf("expected both %q and %q in ya-bin link tail: %v", enumRuntime, jsonCommon, gotTail)
	}
	if enumIdx+1 != jsonIdx {
		t.Fatalf("expected %q immediately before %q in ya-bin link tail: %v", enumRuntime, jsonCommon, gotTail)
	}

	if len(ourNode.Cmds) < 7 || len(refNode.Cmds) < 7 {
		t.Fatalf("expected both nodes to have at least 7 cmds")
	}

	for _, cmdIdx := range []int{4, 5, 6} {
		gotArgs := normalizeT20Strings(ourNode.Cmds[cmdIdx].CmdArgs)
		wantArgs := normalizeT20Strings(refNode.Cmds[cmdIdx].CmdArgs)
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			t.Fatalf("ya-bin cmd[%d] args mismatch:\n  got:  %#v\n  want: %#v", cmdIdx, gotArgs, wantArgs)
		}

		gotEnv := normalizeT20Env(ourNode.Cmds[cmdIdx].Env)
		wantEnv := normalizeT20Env(refNode.Cmds[cmdIdx].Env)
		if !reflect.DeepEqual(gotEnv, wantEnv) {
			t.Fatalf("ya-bin cmd[%d] env mismatch:\n  got:  %#v\n  want: %#v", cmdIdx, gotEnv, wantEnv)
		}
	}
}
