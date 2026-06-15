package main

import (
	"testing"
)

func TestCompilerFlagsFromConfig_NonTestMergesInternalYaConf(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.conf": `
[flags]
CFLAGS = "-DROOT=1"

[host_platform_flags]
CFLAGS = "-DHOST_ROOT=1"
`,
		"build/internal/ya.conf": `
[flags]
CFLAGS = "-DINTERNAL=1"

[host_platform_flags]
CFLAGS = "-DHOST_INTERNAL=1"
`,
	})

	targetFlags := readYaConfSection(fs, "ya.conf", "flags")
	hostFlags := readYaConfSection(fs, "ya.conf", "host_platform_flags")
	targetInternal := readOptionalYaConfSection(fs, "build/internal/ya.conf", "flags")
	hostInternal := readOptionalYaConfSection(fs, "build/internal/ya.conf", "host_platform_flags")

	if got, want := compilerFlagsFromConfig(targetFlags, targetInternal, "CFLAGS", "-DENV=1"), "-DROOT=1 -DINTERNAL=1 -DENV=1"; got != want {
		t.Fatalf("target compiler flags = %q, want %q", got, want)
	}

	if got, want := compilerFlagsFromConfig(hostFlags, hostInternal, "CFLAGS", ""), "-DHOST_ROOT=1 -DHOST_INTERNAL=1"; got != want {
		t.Fatalf("host compiler flags = %q, want %q", got, want)
	}
}

func TestCompilerFlagsFromConfig_TestModeSkipsInternalYaConf(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.conf": `
[flags]
CFLAGS = "-DROOT=1"
`,
		"build/internal/ya.conf": `
[flags]
CFLAGS = "-DINTERNAL=1"
`,
	})

	targetFlags := readYaConfSection(fs, "ya.conf", "flags")

	if got, want := compilerFlagsFromConfig(targetFlags, nil, "CFLAGS", ""), "-DROOT=1"; got != want {
		t.Fatalf("target compiler flags = %q, want %q", got, want)
	}
}
