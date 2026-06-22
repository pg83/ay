package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// alwaysCopyDirs mirrors devtools/ya/tests/copy_inputs.py's ALWAYS_INCLUDE: dirs
// pulled in at configure time via PEERDIR/ADDINCL from the ya.make files of modules
// we copy, but not necessarily read while building the graph — so `ya make` in the
// slice would bail with "PEERDIR to missing directory" / "ADDINCL to non existent
// source directory" without them.
var alwaysCopyDirs = []string{
	"library/cpp/sanitizer",
	"contrib/libs/glibcasm",
	"build/platform",
	"build/conf",
	"build/scripts",
}

// copySourceSlice writes the minimal repo slice the graph build actually touched into
// dst: every source file the FS read (readSourceRels), coarsened to its directory and
// copied recursively, plus the ancestor ya.make of every copied directory (configure
// needs them) and the always-include dirs. It is the in-process replacement for
// devtools/ya/tests/copy_inputs.py — driven by real FS reads rather than the graph,
// which omits scanned headers and other opened files. Directories are copied
// concurrently. The graph must have been built first so contentHashes is populated.
func copySourceSlice(fs *OsFS, srcRoot, dst string, onWarn func(Warn)) error {
	absSrc, err := filepath.Abs(srcRoot)

	if err != nil {
		return err
	}

	absDst, err := filepath.Abs(dst)

	if err != nil {
		return err
	}

	if absSrc == absDst {
		return fmt.Errorf("--copy-sources dst must differ from the source root")
	}

	dirSet := make(map[string]struct{})

	var rootFiles []string

	for _, rel := range fs.readSourceRels() {
		// A read at the repo root (e.g. ya.conf) has dir "." — the repo root is never a
		// copy unit, so copy the file itself. Everything else copies via its directory.
		if d := filepath.ToSlash(filepath.Dir(rel)); d != "." && d != "" {
			dirSet[d] = struct{}{}
		} else {
			rootFiles = append(rootFiles, rel)
		}
	}

	for _, d := range alwaysCopyDirs {
		dirSet[d] = struct{}{}
	}

	dirs := dropDescendantDirs(dirSet)

	// The repo root is never a copy unit: a recursive copy of it would clone the whole
	// tree (and would never have been the intended slice). Drop any entry that resolves
	// to it — "", ".", "/", or a path that joins back to the source root.
	dirs = dropRepoRoot(absSrc, dirs)

	// Individually-copied files: the ancestor ya.make of every kept dir (configure walks
	// them top-down) plus the root-level files the build read (ya.conf, …).
	loose := append(ancestorYamakes(dirs), rootFiles...)

	fmt.Fprintf(os.Stderr, "copy-sources: %d directories (from %d read dirs) + %d loose files -> %s\n",
		len(dirs), len(dirSet), len(loose), absDst)

	copied, skipped, err := copySliceConcurrent(absSrc, absDst, dirs, onWarn)

	if err != nil {
		return err
	}

	looseCount := copyLooseFiles(absSrc, absDst, loose)

	fmt.Fprintf(os.Stderr, "copy-sources: done — %d copied, %d skipped (dst existed) + %d loose files copied\n",
		copied, skipped, looseCount)

	return nil
}

// dropRepoRoot removes any directory that resolves to the source root itself —
// copying the whole repository is forbidden under all conditions.
func dropRepoRoot(srcRoot string, dirs []string) []string {
	out := dirs[:0]

	for _, d := range dirs {
		c := strings.Trim(filepath.ToSlash(filepath.Clean(d)), "/")

		if c == "" || c == "." {
			continue
		}

		if abs, err := filepath.Abs(filepath.Join(srcRoot, d)); err == nil && abs == srcRoot {
			continue
		}

		out = append(out, d)
	}

	return out
}

// dropDescendantDirs keeps only the topmost directory of every ancestor chain: a
// recursive copy of an ancestor already covers its descendants. Shallow-first so an
// ancestor is always seen before the descendants it subsumes.
func dropDescendantDirs(set map[string]struct{}) []string {
	all := make([]string, 0, len(set))

	for d := range set {
		all = append(all, d)
	}

	sort.Slice(all, func(i, j int) bool {
		ci, cj := strings.Count(all[i], "/"), strings.Count(all[j], "/")

		if ci != cj {
			return ci < cj
		}

		return all[i] < all[j]
	})

	var kept []string

	for _, d := range all {
		covered := false

		for _, k := range kept {
			if d == k || strings.HasPrefix(d, k+"/") {
				covered = true

				break
			}
		}

		if !covered {
			kept = append(kept, d)
		}
	}

	return kept
}

// ancestorYamakes is the ya.make at every strict ancestor of each kept dir, plus the
// repo-root ya.make. ya make walks these top-down at configure time (RECURSE/PEERDIR
// resolution), so they must exist even though a recursive dir copy may not include
// them (a kept dir's ancestors are, by dropDescendantDirs, not themselves copied).
func ancestorYamakes(dirs []string) []string {
	set := map[string]struct{}{"ya.make": {}}

	for _, d := range dirs {
		parts := strings.Split(d, "/")

		for i := 1; i < len(parts); i++ {
			set[strings.Join(parts[:i], "/")+"/ya.make"] = struct{}{}
		}
	}

	out := make([]string, 0, len(set))

	for y := range set {
		out = append(out, y)
	}

	return out
}

// copyJob is one regular file or symlink to copy, relative to the source root.
type copyJob struct {
	rel     string
	mode    os.FileMode
	symlink bool
}

// copyStat is one worker's report of a finished file, drained by the single printer.
type copyStat struct {
	rel     string
	err     error
	skipped bool // dst already present — labelled `skip`, not counted as copied
}

// copySliceConcurrent runs the copy pipeline: one producer walks the dirs (stat-only —
// never opening a file, so a FIFO/socket can't block it) and streams the regular files
// / symlinks into jobCh as it finds them, GOMAXPROCS workers copy and report into
// statCh, and one printer drains statCh — so progress is `{n} {copy|skip} rel`,
// numbered monotonically by that single goroutine with no up-front counting pass. The
// `skip` lines are files whose dst already existed (idempotent re-run): listed but not
// re-copied. Returns the counts actually copied and skipped.
func copySliceConcurrent(srcRoot, dst string, dirs []string, onWarn func(Warn)) (copied, skipped int, err error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, 0, err
	}

	jobCh := make(chan copyJob, 256)
	statCh := make(chan copyStat, 256)

	// Producer: walk and stream jobs, creating dst dirs as it goes.
	go func() {
		defer close(jobCh)

		for _, d := range dirs {
			src := filepath.Join(srcRoot, d)

			if abs, err := filepath.Abs(src); err == nil && abs == srcRoot {
				continue // never copy the repo root
			}

			fi, err := os.Lstat(src)

			if err != nil || !fi.IsDir() {
				onWarn(Warn{Kind: WarnMissingInclude, Message: "copy-sources: skip (not a directory in repo): " + d})

				continue
			}

			_ = filepath.WalkDir(src, func(p string, de os.DirEntry, err error) error {
				if err != nil {
					return nil // skip unreadable entries; don't abort the whole slice
				}

				rel, err := filepath.Rel(srcRoot, p)

				if err != nil {
					return nil
				}

				info, err := de.Info()

				if err != nil {
					return nil
				}

				switch {
				case info.Mode()&os.ModeSymlink != 0:
					jobCh <- copyJob{rel: rel, mode: info.Mode(), symlink: true}
				case de.IsDir():
					_ = os.MkdirAll(filepath.Join(dst, rel), 0o755)
				case info.Mode().IsRegular():
					jobCh <- copyJob{rel: rel, mode: info.Mode()}
				}

				return nil
			})
		}
	}()

	// Workers: copy each file, then report it.
	var wg sync.WaitGroup

	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := range jobCh {
				skipped, err := copyOne(filepath.Join(srcRoot, j.rel), filepath.Join(dst, j.rel), j)
				statCh <- copyStat{rel: j.rel, err: err, skipped: skipped}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(statCh)
	}()

	// Printer: the single drainer — owns the counter, so numbering is monotonic.
	n := 0

	var firstErr error

	for s := range statCh {
		n++

		verb := "copy"

		if s.skipped {
			verb = "skip"
		}

		fmt.Fprintf(os.Stderr, "%d %s %s\n", n, verb, s.rel)

		switch {
		case s.err != nil:
			if firstErr == nil {
				firstErr = s.err
			}
		case s.skipped:
			skipped++
		default:
			copied++
		}
	}

	return copied, skipped, firstErr
}

// copyOne copies a single enumerated job: a symlink is recreated (not followed), a
// regular file is content-copied with its mode. Parent dirs are created as needed.
// skipped is true when dst already existed and nothing was touched, so the caller can
// label it `skip` rather than `copy` in its progress output.
func copyOne(src, dst string, j copyJob) (skipped bool, err error) {
	// Idempotent re-run / overlap with a prior copy: if dst already exists, the file
	// was placed on an earlier pass. Skip it without touching src — the source lives
	// on a slow arc/FUSE mount, so we don't pay the read I/O (or risk an EPERM) for a
	// no-op. Per the invariant: no src operation until dst is confirmed absent.
	if _, err := os.Lstat(dst); err == nil {
		return true, nil
	}

	if j.symlink {
		link, err := os.Readlink(src)

		if err != nil {
			return false, err
		}

		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		_ = os.Remove(dst)

		return false, os.Symlink(link, dst)
	}

	return false, copyFileMode(src, dst, j.mode)
}

// copyLooseFiles copies individual files (ancestor ya.makes, root-level configs) that a
// recursive dir copy did not already place. Sequential — there are few, each small.
func copyLooseFiles(srcRoot, dst string, rels []string) int {
	n := 0

	for _, rel := range rels {
		target := filepath.Join(dst, rel)

		// dst first: if it's already present (brought in by a recursive dir copy),
		// skip before touching src — no src op until dst is confirmed absent.
		if _, err := os.Lstat(target); err == nil {
			continue
		}

		src := filepath.Join(srcRoot, rel)
		fi, err := os.Lstat(src)

		if err != nil || fi.IsDir() {
			continue
		}

		if copyFileMode(src, target, fi.Mode()) == nil {
			n++
		}
	}

	return n
}

// copyFile copies one file's contents into dst (creating parents), preserving mode.
func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)

	if err != nil {
		// EPERM/EACCES reading the source (restricted file on the arc/FUSE mount):
		// skip it rather than failing the whole slice. Nothing is written to dst.
		if errors.Is(err, os.ErrPermission) {
			return nil
		}

		return err
	}

	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())

	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()

		return err
	}

	return out.Close()
}
