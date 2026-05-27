package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompilerFlagsFromConfig_NonTestMergesInternalYaConf(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "ya.conf"), `
[flags]
CFLAGS = "-DROOT=1"

[host_platform_flags]
CFLAGS = "-DHOST_ROOT=1"
`)
	writeTestFile(t, filepath.Join(root, "build", "internal", "ya.conf"), `
[flags]
CFLAGS = "-DINTERNAL=1"

[host_platform_flags]
CFLAGS = "-DHOST_INTERNAL=1"
`)

	fs := NewFS(root)
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
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "ya.conf"), `
[flags]
CFLAGS = "-DROOT=1"
`)
	writeTestFile(t, filepath.Join(root, "build", "internal", "ya.conf"), `
[flags]
CFLAGS = "-DINTERNAL=1"
`)

	fs := NewFS(root)
	targetFlags := readYaConfSection(fs, "ya.conf", "flags")

	if got, want := compilerFlagsFromConfig(targetFlags, nil, "CFLAGS", ""), "-DROOT=1"; got != want {
		t.Fatalf("target compiler flags = %q, want %q", got, want)
	}
}

func TestShouldExposeSandboxingTargetTags(t *testing.T) {
	cases := []struct {
		name string
		mf   *makeFlags
		want bool
	}{
		{
			name: "nil",
			mf:   nil,
			want: false,
		},
		{
			name: "plain sandboxing build",
			mf: &makeFlags{
				sandboxing: true,
			},
			want: false,
		},
		{
			name: "test sandboxing build",
			mf: &makeFlags{
				sandboxing: true,
				testLevel:  3,
			},
			want: true,
		},
	}

	for _, tc := range cases {
		if got := shouldExposeSandboxingTargetTags(tc.mf); got != tc.want {
			t.Fatalf("%s: shouldExposeSandboxingTargetTags = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
