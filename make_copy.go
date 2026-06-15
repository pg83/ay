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
	"sync/atomic"
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

	fmt.Fprintf(os.Stderr, "copy-sources: done — %d/%d dirs + %d ya.make files copied\n", copied, len(dirs), yamakeCount)

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

// copySliceConcurrent copies each dir's tree from srcRoot into dst, preserving the
// relative path, across GOMAXPROCS workers. Returns the count of dirs copied.
func copySliceConcurrent(srcRoot, dst string, dirs []string, onWarn func(Warn)) (int, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}

	jobs := make(chan string)

	var (
		wg       sync.WaitGroup
		copied   atomic.Int64
		errOnce  sync.Once
		firstErr error
	)

	setErr := func(e error) {
		errOnce.Do(func() { firstErr = e })
	}

	// Print every directory as a worker starts it, as `{started}/{all} path`, so a
	// slow/huge tree is visible (its line is the last printed) instead of looking hung.
	total := len(dirs)
	var started atomic.Int64

	workers := runtime.GOMAXPROCS(0)

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for d := range jobs {
				src := filepath.Join(srcRoot, d)

				if abs, err := filepath.Abs(src); err == nil && abs == srcRoot {
					continue // belt-and-suspenders: never copy the repo root
				}

				fi, err := os.Lstat(src)

				if err != nil || !fi.IsDir() {
					onWarn(Warn{Kind: WarnMissingInclude, Message: "copy-sources: skip (not a directory in repo): " + d})

					continue
				}

				fmt.Fprintf(os.Stderr, "%d/%d %s\n", started.Add(1), total, d)

				if err := copyTree(src, filepath.Join(dst, d)); err != nil {
					setErr(err)
				} else {
					copied.Add(1)
				}
			}
		}()
	}

	for _, d := range dirs {
		jobs <- d
	}

	close(jobs)
	wg.Wait()

	return int(copied.Load()), firstErr
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

// copyTree copies the directory tree at src into dst, preserving symlinks (recreated,
// not followed) and file modes. Directories are created as needed.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)

		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		info, err := de.Info()

		if err != nil {
			return err
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)

			if err != nil {
				return err
			}

			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Remove(target)

			return os.Symlink(link, target)
		case de.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		default:
			return copyFileMode(p, target, info.Mode())
		}
	})
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
