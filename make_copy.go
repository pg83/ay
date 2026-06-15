package main

import (
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

	for _, rel := range fs.readSourceRels() {
		// Every read is a file; its directory is the build-relevant unit. A read at
		// the repo root (dir ".") is the root ya.make — covered by ancestorYamakes.
		if d := filepath.ToSlash(filepath.Dir(rel)); d != "." && d != "" {
			dirSet[d] = struct{}{}
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

	yamakes := ancestorYamakes(dirs)

	fmt.Fprintf(os.Stderr, "copy-sources: %d directories (from %d read dirs) + %d ancestor ya.make files -> %s\n",
		len(dirs), len(dirSet), len(yamakes), absDst)

	copied, err := copySliceConcurrent(absSrc, absDst, dirs, onWarn)

	if err != nil {
		return err
	}

	yamakeCount := copyAncestorYamakes(absSrc, absDst, yamakes)

	fmt.Fprintf(os.Stderr, "copy-sources: done — %d files + %d ancestor ya.make files copied\n", copied, yamakeCount)

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
	rel string
	err error
}

// copySliceConcurrent runs the copy pipeline: one producer walks the dirs (stat-only —
// never opening a file, so a FIFO/socket can't block it) and streams the regular files
// / symlinks into jobCh as it finds them, GOMAXPROCS workers copy and report into
// statCh, and one printer drains statCh — so progress is `{n} rel`, numbered
// monotonically by that single goroutine with no up-front counting pass. Returns the
// count copied.
func copySliceConcurrent(srcRoot, dst string, dirs []string, onWarn func(Warn)) (int, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
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
				err := copyOne(filepath.Join(srcRoot, j.rel), filepath.Join(dst, j.rel), j)
				statCh <- copyStat{rel: j.rel, err: err}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(statCh)
	}()

	// Printer: the single drainer — owns the counter, so numbering is monotonic.
	copied, n := 0, 0
	var firstErr error

	for s := range statCh {
		n++

		fmt.Fprintf(os.Stderr, "%d %s\n", n, s.rel)

		if s.err != nil {
			if firstErr == nil {
				firstErr = s.err
			}
		} else {
			copied++
		}
	}

	return copied, firstErr
}

// copyOne copies a single enumerated job: a symlink is recreated (not followed), a
// regular file is content-copied with its mode. Parent dirs are created as needed.
func copyOne(src, dst string, j copyJob) error {
	if j.symlink {
		link, err := os.Readlink(src)

		if err != nil {
			return err
		}

		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		_ = os.Remove(dst)

		return os.Symlink(link, dst)
	}

	return copyFileMode(src, dst, j.mode)
}

// copyAncestorYamakes copies the ancestor ya.make files that a recursive dir copy did
// not already place. Sequential — there are few, and each is one small file.
func copyAncestorYamakes(srcRoot, dst string, yamakes []string) int {
	n := 0

	for _, y := range yamakes {
		src := filepath.Join(srcRoot, y)
		fi, err := os.Lstat(src)

		if err != nil || fi.IsDir() {
			continue
		}

		target := filepath.Join(dst, y)

		if _, err := os.Lstat(target); err == nil {
			continue // already brought in by a recursive dir copy
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
