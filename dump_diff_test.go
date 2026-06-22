package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two identical archive variants (non-PIC + PIC) at one output path: the diff
// must pair like-with-like and report no difference. Without the PIC-aware match
// key, findMatchingNodePair mispairs and emits a spurious member diff.
func TestDumpDiffPair_PicAndNonPicVariantsDoNotMispair(t *testing.T) {
	nonPic := `{"self_uid":"%s","kv":{"p":"AR"},"platform":"plat","outputs":["$(B)/x/lib.a"],"cmds":[{"cmd_args":["ar","$(B)/x/foo.cpp.o"]}],"inputs":["$(B)/x/foo.cpp.o"],"tags":[]}`
	pic := `{"self_uid":"%s","kv":{"p":"AR"},"platform":"plat","outputs":["$(B)/x/lib.a"],"cmds":[{"cmd_args":["ar","$(B)/x/foo.cpp.pic.o"]}],"inputs":["$(B)/x/foo.cpp.pic.o"],"tags":[]}`

	dir := t.TempDir()
	left := filepath.Join(dir, "L.jsonl")
	right := filepath.Join(dir, "R.jsonl")

	writeLines := func(path, a, b string) {
		if err := os.WriteFile(path, []byte(a+"\n"+b+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeLines(left,
		strings.Replace(nonPic, "%s", "l1", 1),
		strings.Replace(pic, "%s", "l2", 1))
	writeLines(right,
		strings.Replace(nonPic, "%s", "r1", 1),
		strings.Replace(pic, "%s", "r2", 1))

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	diffPair(left, right, "$(B)/x/lib.a", bw)
	throw(bw.Flush())

	out := buf.String()
	if strings.Contains(out, "differs") {
		t.Fatalf("identical PIC/non-PIC variant sets reported a diff:\n%s", out)
	}
}

// The PIC-aware key must not mask a genuine member divergence: an extra member
// in ref's PIC variant must still be reported.
func TestDumpDiffPair_PicVariantMemberDivergenceStillReported(t *testing.T) {
	nonPic := `{"self_uid":"%s","kv":{"p":"AR"},"platform":"plat","outputs":["$(B)/x/lib.a"],"cmds":[{"cmd_args":["ar","$(B)/x/foo.cpp.o"]}],"inputs":["$(B)/x/foo.cpp.o"],"tags":[]}`
	picOurs := `{"self_uid":"l2","kv":{"p":"AR"},"platform":"plat","outputs":["$(B)/x/lib.a"],"cmds":[{"cmd_args":["ar","$(B)/x/foo.cpp.pic.o"]}],"inputs":["$(B)/x/foo.cpp.pic.o"],"tags":[]}`
	picRef := `{"self_uid":"r2","kv":{"p":"AR"},"platform":"plat","outputs":["$(B)/x/lib.a"],"cmds":[{"cmd_args":["ar","$(B)/x/foo.cpp.pic.o","$(B)/x/bar.cpp.pic.o"]}],"inputs":["$(B)/x/foo.cpp.pic.o","$(B)/x/bar.cpp.pic.o"],"tags":[]}`

	dir := t.TempDir()
	left := filepath.Join(dir, "L.jsonl")
	right := filepath.Join(dir, "R.jsonl")

	if err := os.WriteFile(left, []byte(strings.Replace(nonPic, "%s", "l1", 1)+"\n"+picOurs+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(right, []byte(strings.Replace(nonPic, "%s", "r1", 1)+"\n"+picRef+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	diffPair(left, right, "$(B)/x/lib.a", bw)
	throw(bw.Flush())

	out := buf.String()
	if !strings.Contains(out, "bar.cpp.pic.o") {
		t.Fatalf("genuine PIC member divergence (extra bar.cpp.pic.o) not reported:\n%s", out)
	}
}
