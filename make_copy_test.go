package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyOne_SkipsExistingDstWithoutTouchingSrc verifies the idempotency invariant:
// when dst already exists, copyOne returns nil and performs no operation on src — so a
// missing/unreadable src is irrelevant and the existing dst is left untouched.
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

// TestCopyFileMode_IgnoresSrcEPERM verifies that a permission-denied read of src is
// swallowed (return nil) rather than failing the slice, and that no dst is written.
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

// TestCopySliceConcurrent_SkipExistingCopyFresh drives the full producer/worker/printer
// pipeline over a dir whose dst is partially populated: the file already at dst is
// skipped (its content preserved, src not re-read), the file absent from dst is copied,
// and the two are tallied separately.
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

	// "old" is already present at dst (a prior pass placed it) — must be skipped.
	if err := os.MkdirAll(filepath.Join(dst, "d"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dst, "d", "old"), []byte("DST-OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	copied, skipped, err := copySliceConcurrent(srcRoot, dst, []string{"d"}, func(Warn) {})

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

// TestCopyLooseFiles_DstCheckedBeforeSrc verifies copyLooseFiles skips a rel whose
// target already exists without touching src (here src is absent), and still copies a
// fresh one.
func TestCopyLooseFiles_DstCheckedBeforeSrc(t *testing.T) {
	srcRoot := t.TempDir()
	dst := t.TempDir()

	// "existing" already present at dst; its src does not exist — must be skipped.
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
