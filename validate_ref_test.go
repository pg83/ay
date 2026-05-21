package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeValidateRefFixture(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runValidateRefCLI(t *testing.T, args ...string) error {
	t.Helper()

	cmd := exec.Command("python3", append([]string{"dev/validate_ref.py"}, args...)...)
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("validate_ref.py %v\n%s", args, out)
	}

	return err
}

func TestValidateRefPackAndCompare(t *testing.T) {
	dir := t.TempDir()
	refRaw := filepath.Join(dir, "ref.json")
	ourRaw := filepath.Join(dir, "our.json")
	refXZ := filepath.Join(dir, "ref.json.xz")
	ourCanon := filepath.Join(dir, "our.canon.json")
	refCanon := filepath.Join(dir, "ref.canon.json")

	writeValidateRefFixture(t, refRaw, `{
    "conf": {},
    "graph": [
        {
            "cmds": [],
            "deps": [],
            "env": {},
            "inputs": [
                "$(SOURCE_ROOT)/pkg/app/a.h",
                "$(SOURCE_ROOT)/pkg/app/b.h",
                "$(SOURCE_ROOT)/pkg/app/main.c"
            ],
            "kv": {
                "p": "CC"
            },
            "outputs": [
                "$(BUILD_ROOT)/pkg/app/main.o"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "cc-ref",
            "tags": [],
            "target_properties": {},
            "uid": "cc-ref"
        },
        {
            "cmds": [],
            "deps": [
                "cc-ref"
            ],
            "env": {},
            "inputs": [
                "$(BUILD_ROOT)/pkg/app/main.o",
                "$(SOURCE_ROOT)/build/scripts/link_exe.py"
            ],
            "kv": {
                "p": "LD"
            },
            "outputs": [
                "$(BUILD_ROOT)/pkg/app/app"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "ld-ref",
            "tags": [],
            "target_properties": {},
            "uid": "ld-ref"
        }
    ],
    "inputs": {},
    "result": [
        "ld-ref"
    ]
}`)

	writeValidateRefFixture(t, ourRaw, `{
    "conf": {},
    "graph": [
        {
            "cmds": [],
            "deps": [],
            "env": {},
            "inputs": [],
            "kv": {
                "p": "FETCH"
            },
            "outputs": [
                "$(B)/resources/YMAKE_PYTHON3"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "fetch-our",
            "tags": [],
            "target_properties": {},
            "uid": "fetch-our"
        },
        {
            "cmds": [],
            "deps": [],
            "env": {},
            "inputs": [
                "$(S)/pkg/app/main.c",
                "$(S)/pkg/app/b.h",
                "$(S)/pkg/app/a.h"
            ],
            "kv": {
                "p": "CC"
            },
            "outputs": [
                "$(B)/pkg/app/main.o"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": false,
            "self_uid": "cc-our",
            "tags": [],
            "target_properties": {},
            "uid": "cc-our"
        },
        {
            "cmds": [],
            "deps": [
                "fetch-our",
                "cc-our"
            ],
            "env": {},
            "inputs": [
                "$(S)/build/scripts/link_exe.py",
                "$(B)/pkg/app/main.o"
            ],
            "kv": {
                "p": "LD"
            },
            "outputs": [
                "$(B)/pkg/app/app"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": false,
            "self_uid": "ld-our",
            "tags": [],
            "target_properties": {},
            "uid": "ld-our"
        }
    ],
    "inputs": {},
    "result": [
        "ld-our"
    ]
}`)

	if err := runValidateRefCLI(t, "pack", "--raw", refRaw, "--target", "pkg/app", "--out", refXZ); err != nil {
		t.Fatalf("pack failed: %v", err)
	}

	if err := runValidateRefCLI(t,
		"compare",
		"--our", ourRaw,
		"--ref", refXZ,
		"--target", "pkg/app",
		"--our-out", ourCanon,
		"--ref-out", refCanon,
	); err != nil {
		t.Fatalf("compare failed: %v", err)
	}

	ourBytes, err := os.ReadFile(ourCanon)
	if err != nil {
		t.Fatalf("read our canon: %v", err)
	}
	refBytes, err := os.ReadFile(refCanon)
	if err != nil {
		t.Fatalf("read ref canon: %v", err)
	}
	if string(ourBytes) != string(refBytes) {
		t.Fatalf("canonical outputs differ\nour:\n%s\nref:\n%s", ourBytes, refBytes)
	}
}

func TestValidateRefCompareFailsOnSemanticDrift(t *testing.T) {
	dir := t.TempDir()
	refRaw := filepath.Join(dir, "ref.json")
	ourRaw := filepath.Join(dir, "our.json")
	refXZ := filepath.Join(dir, "ref.json.xz")

	writeValidateRefFixture(t, refRaw, `{
    "conf": {},
    "graph": [
        {
            "cmds": [],
            "deps": [],
            "env": {},
            "inputs": [
                "$(SOURCE_ROOT)/pkg/app/main.c"
            ],
            "kv": {
                "p": "CC"
            },
            "outputs": [
                "$(BUILD_ROOT)/pkg/app/main.o"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "cc-ref",
            "tags": [],
            "target_properties": {},
            "uid": "cc-ref"
        },
        {
            "cmds": [],
            "deps": [
                "cc-ref"
            ],
            "env": {},
            "inputs": [
                "$(BUILD_ROOT)/pkg/app/main.o"
            ],
            "kv": {
                "p": "LD"
            },
            "outputs": [
                "$(BUILD_ROOT)/pkg/app/app"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "ld-ref",
            "tags": [],
            "target_properties": {},
            "uid": "ld-ref"
        }
    ],
    "inputs": {},
    "result": [
        "ld-ref"
    ]
}`)

	writeValidateRefFixture(t, ourRaw, `{
    "conf": {},
    "graph": [
        {
            "cmds": [],
            "deps": [],
            "env": {},
            "inputs": [
                "$(S)/pkg/app/other.c"
            ],
            "kv": {
                "p": "CC"
            },
            "outputs": [
                "$(B)/pkg/app/main.o"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "cc-our",
            "tags": [],
            "target_properties": {},
            "uid": "cc-our"
        },
        {
            "cmds": [],
            "deps": [
                "cc-our"
            ],
            "env": {},
            "inputs": [
                "$(B)/pkg/app/main.o"
            ],
            "kv": {
                "p": "LD"
            },
            "outputs": [
                "$(B)/pkg/app/app"
            ],
            "platform": "default-linux-x86_64",
            "requirements": {},
            "sandboxing": true,
            "self_uid": "ld-our",
            "tags": [],
            "target_properties": {},
            "uid": "ld-our"
        }
    ],
    "inputs": {},
    "result": [
        "ld-our"
    ]
}`)

	if err := runValidateRefCLI(t, "pack", "--raw", refRaw, "--target", "pkg/app", "--out", refXZ); err != nil {
		t.Fatalf("pack failed: %v", err)
	}

	err := runValidateRefCLI(t, "compare", "--our", ourRaw, "--ref", refXZ, "--target", "pkg/app")
	if err == nil {
		t.Fatalf("compare unexpectedly succeeded")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("compare returned %T, want *exec.ExitError", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("compare exit code = %d, want 1", exitErr.ExitCode())
	}
}
