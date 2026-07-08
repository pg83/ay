package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestPrebuiltToolchainFlags_CarryConfigNotToolPaths(t *testing.T) {
	flags := prebuiltToolchainFlags()

	if got, want := flags["CLANG_VER"], "20"; got != want {
		t.Fatalf("CLANG_VER = %q, want %q", got, want)
	}

	for _, k := range []string{
		"CLANG_TOOL", "CLANG_pl_pl_TOOL", "AR_TOOL", "OBJCOPY_TOOL", "STRIP_TOOL",
		"LLD_TOOL", "BUILD_PYTHON_BIN", "BUILD_PYTHON3_BIN",
		"CLANG16_RESOURCE_GLOBAL", "LLD_ROOT_RESOURCE_GLOBAL",
	} {
		if got, ok := flags[k]; ok {
			t.Fatalf("%s unexpectedly present in prebuiltToolchainFlags = %q (must come from peerdir)", k, got)
		}
	}
}

func TestReadYaConfSections_MergesLaterFilesAndSkipsMissing(t *testing.T) {
	fs := newMemFS(map[string]string{
		"ya.conf": `[flags]
ROOT_ONLY = "root"
SHARED = "root"
`,
		"build/internal/ya.conf": `[flags]
INTERNAL_ONLY = "internal"
SHARED = "internal"
`,
	})

	got := readYaConfSections(fs, "flags", "ya.conf", "missing/ya.conf", "build/internal/ya.conf")
	want := map[string]string{
		"ROOT_ONLY":     "root",
		"INTERNAL_ONLY": "internal",
		"SHARED":        "internal",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readYaConfSections() = %#v, want %#v", got, want)
	}
}

func readYaConfSections(fs FS, wantSection string, rels ...string) map[string]string {
	out := map[string]string{}

	for _, rel := range rels {
		if !fs.isFile(srcRootRel, rel) {
			continue
		}

		raw := fs.read(rel)

		section := ""

		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)

			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))

				continue
			}

			if section != wantSection {
				continue
			}

			key, val, ok := strings.Cut(line, "=")

			if !ok {
				continue
			}

			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			val = strings.Trim(val, `"`)

			if key != "" {
				out[key] = val
			}
		}
	}

	return out
}
