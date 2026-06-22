package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyOne_SkipsExistingDstWithoutTouchingSrc verifies the idempotency invariant: when
// dst already exists, copyOne returns nil and touches src not at all — so a missing src is
// irrelevant and the existing dst is left untouched.
func TestCopyOne_SkipsExistingDstWithoutTouchingSrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src") // deliberately never created
	dst := filepath.Join(dir, "dst")

	if err := os.WriteFile(dst, []byte("DST"), 0o644); err != nil {
		t.Fatal(err)
	}

	skipped, err := copyOne(src, dst, copyJob{rel: "dst"})

	if err != nil {
		t.Fatalf("copyOne over existing dst (absent src) = %v, want nil", err)
	}

	if !skipped {
		t.Fatalf("copyOne over existing dst: skipped = false, want true (dst present, src untouched)")
	}

	got, err := os.ReadFile(dst)

	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "DST" {
		t.Fatalf("dst overwritten: got %q, want %q", got, "DST")
	}
}

// TestCopyFileMode_IgnoresSrcEPERM verifies a permission-denied read of src is swallowed
// (return nil) rather than failing the slice, and that no dst is written.
func TestCopyFileMode_IgnoresSrcEPERM(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	if err := os.WriteFile(src, []byte("SRC"), 0o000); err != nil {
		t.Fatal(err)
	}

	if err := copyFileMode(src, dst); err != nil {
		t.Fatalf("copyFileMode on unreadable src = %v, want nil (EPERM ignored)", err)
	}

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("dst created despite EPERM on src: stat err = %v", err)
	}
}

// TestCopySliceConcurrent_SkipExistingCopyFresh drives the full pipeline over a dir whose
// dst is partially populated: the file already at dst is skipped (content preserved), the
// absent file is copied, and the two are tallied separately.
func TestCopySliceConcurrent_SkipExistingCopyFresh(t *testing.T) {
	srcRoot := t.TempDir()
	dst := t.TempDir()

	if err := os.MkdirAll(filepath.Join(srcRoot, "d"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(srcRoot, "d", "old"), []byte("SRC-OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(srcRoot, "d", "new"), []byte("SRC-NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	// "old" is already at dst (a prior pass) — must be skipped.
	if err := os.MkdirAll(filepath.Join(dst, "d"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dst, "d", "old"), []byte("DST-OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	copied, skipped, err := copySliceConcurrent(srcRoot, dst, nil, []string{"d"}, func(Warn) {})

	if err != nil {
		t.Fatalf("copySliceConcurrent = %v, want nil", err)
	}

	if copied != 1 {
		t.Fatalf("copied = %d, want 1 (only d/new)", copied)
	}

	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (d/old already at dst)", skipped)
	}

	if got, _ := os.ReadFile(filepath.Join(dst, "d", "old")); string(got) != "DST-OLD" {
		t.Fatalf("existing dst overwritten: got %q, want %q", got, "DST-OLD")
	}

	if got, _ := os.ReadFile(filepath.Join(dst, "d", "new")); string(got) != "SRC-NEW" {
		t.Fatalf("fresh not copied: got %q, want %q", got, "SRC-NEW")
	}
}

// TestCopySliceConcurrent_ShallowSkipsSubdirs pins the read-dir granularity: a shallow
// dir copies its own file entries but never descends, so a read-less subtree under it is
// left out of the slice.
func TestCopySliceConcurrent_ShallowSkipsSubdirs(t *testing.T) {
	srcRoot := t.TempDir()
	dst := t.TempDir()

	if err := os.MkdirAll(filepath.Join(srcRoot, "d", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(srcRoot, "d", "top.txt"), []byte("TOP"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(srcRoot, "d", "sub", "deep.txt"), []byte("DEEP"), 0o644); err != nil {
		t.Fatal(err)
	}

	copied, skipped, err := copySliceConcurrent(srcRoot, dst, nil, []string{"d"}, func(Warn) {})

	if err != nil {
		t.Fatalf("copySliceConcurrent = %v, want nil", err)
	}

	if copied != 1 {
		t.Fatalf("copied = %d, want 1 (only d/top.txt; d/sub not descended)", copied)
	}

	if skipped != 0 {
		t.Fatalf("skipped = %d, want 0", skipped)
	}

	if _, err := os.Stat(filepath.Join(dst, "d", "top.txt")); err != nil {
		t.Fatalf("d/top.txt not copied: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "d", "sub", "deep.txt")); !os.IsNotExist(err) {
		t.Fatalf("read-less subdir copied (d/sub/deep.txt): stat err = %v, want not-exist", err)
	}
}

// TestCopySliceConcurrent_RecursiveCopiesSubtree verifies a recursive dir copies its whole
// subtree (alwaysCopyDirs semantics).
func TestCopySliceConcurrent_RecursiveCopiesSubtree(t *testing.T) {
	srcRoot := t.TempDir()
	dst := t.TempDir()

	if err := os.MkdirAll(filepath.Join(srcRoot, "r", "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(srcRoot, "r", "top.txt"), []byte("T"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(srcRoot, "r", "a", "b", "deep.txt"), []byte("D"), 0o644); err != nil {
		t.Fatal(err)
	}

	copied, _, err := copySliceConcurrent(srcRoot, dst, []string{"r"}, nil, func(Warn) {})

	if err != nil {
		t.Fatalf("copySliceConcurrent = %v, want nil", err)
	}

	if copied != 2 {
		t.Fatalf("copied = %d, want 2 (whole subtree)", copied)
	}

	if _, err := os.Stat(filepath.Join(dst, "r", "a", "b", "deep.txt")); err != nil {
		t.Fatalf("recursive deep file not copied: %v", err)
	}
}

// TestDropUnderRecursive verifies a dir covered by a recursive dir (itself or an ancestor)
// is dropped, while an unrelated dir and a strict ancestor are kept.
func TestDropUnderRecursive(t *testing.T) {
	got := dropUnderRecursive(
		[]string{"a/b", "x/y", "build/scripts/sub", "build"},
		[]string{"build/scripts", "x"},
	)

	want := []string{"a/b", "build"}

	if len(got) != len(want) {
		t.Fatalf("dropUnderRecursive = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dropUnderRecursive = %v, want %v", got, want)
		}
	}
}

// TestCopyLooseFiles_DstCheckedBeforeSrc verifies copyLooseFiles skips a rel whose target
// already exists without touching src (here absent), and still copies a fresh one.
func TestCopyLooseFiles_DstCheckedBeforeSrc(t *testing.T) {
	srcRoot := t.TempDir()
	dst := t.TempDir()

	// "existing" at dst, src absent — must be skipped.
	if err := os.WriteFile(filepath.Join(dst, "existing"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	// "fresh" present only at src — must be copied.
	if err := os.WriteFile(filepath.Join(srcRoot, "fresh"), []byte("NEW"), 0o644); err != nil {
		t.Fatal(err)
	}

	n := copyLooseFiles(srcRoot, dst, []string{"existing", "fresh"})

	if n != 1 {
		t.Fatalf("copied count = %d, want 1 (only fresh)", n)
	}

	if got, _ := os.ReadFile(filepath.Join(dst, "existing")); string(got) != "OLD" {
		t.Fatalf("existing dst overwritten: got %q, want %q", got, "OLD")
	}

	if got, _ := os.ReadFile(filepath.Join(dst, "fresh")); string(got) != "NEW" {
		t.Fatalf("fresh not copied: got %q, want %q", got, "NEW")
	}
}
