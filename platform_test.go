package main

import (
	"reflect"
	"testing"
)

func TestParseCompilerFlags(t *testing.T) {
	got := parseCompilerFlags(`-O2 -DNAME="hello world" '-DOTHER=two words' -DQUOTE=\"x\" trailing\ slash`)
	want := []string{
		"-O2",
		"-DNAME=hello world",
		"-DOTHER=two words",
		`-DQUOTE="x"`,
		"trailing slash",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCompilerFlags mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNewPlatform_ParsesCompilerFlags(t *testing.T) {
	flags := map[string]string{
		"PIC": "no",
	}

	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, `-O2 -DNAME="hello world"`, `-stdlib=libc++ -DCPP=1`)

	if !reflect.DeepEqual(p.CFlags, []string{"-O2", "-DNAME=hello world"}) {
		t.Fatalf("CFlags = %#v", p.CFlags)
	}

	if !reflect.DeepEqual(p.CXXFlags, []string{"-stdlib=libc++", "-DCPP=1"}) {
		t.Fatalf("CXXFlags = %#v", p.CXXFlags)
	}
}

func TestStatsTagsForPlatform_TargetSandboxing(t *testing.T) {
	flags := map[string]string{
		"GG_BUILD_TYPE": "debug",
		"MUSL":          "yes",
		"PIC":           "no",
		"SANDBOXING":    "yes",
	}
	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "")
	p.StatsFlags = buildTargetStatsFlags(flags, map[string]string{"MUSL": "yes"})

	want := []string{
		"default-linux-aarch64",
		"debug",
		"FAKEID=sandboxing",
		"SANDBOXING=yes",
		"musl",
	}

	if got := statsTagsForPlatform(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("statsTagsForPlatform(target) mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestStatsTagsForPlatform_TargetBaseFlags(t *testing.T) {
	flags := map[string]string{
		"GG_BUILD_TYPE": "debug",
		"PIC":           "no",
		"USE_LTO":       "yes",
	}
	p := NewPlatform(OSLinux, ISAAArch64, flags, nil, "", "")
	p.StatsFlags = buildTargetStatsFlags(flags, map[string]string{"UNRELATED": "yes"})

	want := []string{
		"default-linux-aarch64",
		"debug",
		"lto",
	}

	if got := statsTagsForPlatform(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("statsTagsForPlatform(target base flags) mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestStatsTagsForPlatform_HostTool(t *testing.T) {
	p := NewPlatform(OSLinux, ISAX8664, map[string]string{"PIC": "yes", "GG_BUILD_TYPE": "release"}, []string{"tool"}, "", "")
	p.StatsFlags = buildHostStatsFlags(map[string]string{"MUSL": "yes"}, false)

	want := []string{
		"default-linux-x86_64",
		"release",
		"CLANG_COVERAGE=no",
		"CONSISTENT_DEBUG=yes",
		"NO_DEBUGINFO=yes",
		"TIDY=no",
		"TOOL_BUILD_MODE=yes",
		"TRAVERSE_RECURSE=no",
		"musl",
		"pic",
	}

	if got := statsTagsForPlatform(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("statsTagsForPlatform(host) mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestStatsTagsForPlatform_HostSandboxing(t *testing.T) {
	p := NewPlatform(OSLinux, ISAX8664, map[string]string{"PIC": "yes", "GG_BUILD_TYPE": "release"}, []string{"tool"}, "", "")
	p.StatsFlags = buildHostStatsFlags(map[string]string{"MUSL": "yes"}, true)

	want := []string{
		"default-linux-x86_64",
		"release",
		"CLANG_COVERAGE=no",
		"CONSISTENT_DEBUG=yes",
		"FAKEID=sandboxing",
		"NO_DEBUGINFO=yes",
		"SANDBOXING=yes",
		"TIDY=no",
		"TOOL_BUILD_MODE=yes",
		"TRAVERSE_RECURSE=no",
		"musl",
		"pic",
	}

	if got := statsTagsForPlatform(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("statsTagsForPlatform(host sandboxing) mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestPlatformMultiarchLibPath_UsesCompilerRoot(t *testing.T) {
	p := NewPlatform(OSLinux, ISAX8664, map[string]string{
		"PIC":              "yes",
		"BUILD_PYTHON_BIN": "$(YMAKE_PYTHON3-1002064631)/bin/python3",
		"CLANG_TOOL":       "$(CLANG-1274503668)/bin/clang",
		"CLANG_pl_pl_TOOL": "$(CLANG-1274503668)/bin/clang++",
		"AR_TOOL":          "$(CLANG-1274503668)/bin/llvm-ar",
		"LLD_TOOL":         "$(LLD_ROOT-3107549726)/bin/ld.lld",
	}, []string{"tool"}, "", "")

	if got, want := p.MultiarchLibPath(), "$(CLANG-1274503668)/lib:$OS_SDK_ROOT_RESOURCE_GLOBAL/usr/lib/x86_64-linux-gnu"; got != want {
		t.Fatalf("MultiarchLibPath = %q, want %q", got, want)
	}
}

func TestPlatformLinkerSelectionTailFlags_UsesConfiguredLLDPath(t *testing.T) {
	p := NewPlatform(OSLinux, ISAX8664, map[string]string{
		"PIC":              "no",
		"CLANG_TOOL":       "$(CLANG-1274503668)/bin/clang",
		"CLANG_pl_pl_TOOL": "$(CLANG-1274503668)/bin/clang++",
		"AR_TOOL":          "$(CLANG-1274503668)/bin/llvm-ar",
		"LLD_TOOL":         "$(LLD_ROOT-3107549726)/bin/ld.lld",
	}, nil, "", "")

	want := []string{
		"-fuse-ld=lld",
		"--ld-path=$(LLD_ROOT-3107549726)/bin/ld.lld",
		"-Wl,--no-rosegment",
		"-Wl,--build-id=sha1",
	}
	if got := p.LinkerSelectionTailFlags(); !reflect.DeepEqual(got, want) {
		t.Fatalf("LinkerSelectionTailFlags = %#v, want %#v", got, want)
	}
}
