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

var alwaysCopyDirs = []string{
	"library/cpp/sanitizer",
	"contrib/libs/glibcasm",
	"build/platform",
	"build/conf",
	"build/scripts",
}

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
		if d := filepath.ToSlash(filepath.Dir(rel)); d != "." && d != "" {
			dirSet[d] = struct{}{}
		} else {
			rootFiles = append(rootFiles, rel)
		}
	}

	recursiveDirs := dropRepoRoot(absSrc, append([]string(nil), alwaysCopyDirs...))
	shallowDirs := make([]string, 0, len(dirSet))

	for d := range dirSet {
		shallowDirs = append(shallowDirs, d)
	}

	sort.Strings(shallowDirs)
	shallowDirs = dropRepoRoot(absSrc, shallowDirs)
	shallowDirs = dropUnderRecursive(shallowDirs, recursiveDirs)

	loose := concat(ancestorYamakes(concat(recursiveDirs, shallowDirs)), rootFiles)

	fmt.Fprintf(os.Stderr, "copy-sources: %d read dirs (shallow) + %d always-dirs (recursive) + %d loose files -> %s\n",
		len(shallowDirs), len(recursiveDirs), len(loose), absDst)

	copied, skipped, err := copySliceConcurrent(absSrc, absDst, recursiveDirs, shallowDirs, onWarn)

	if err != nil {
		return err
	}

	looseCount := copyLooseFiles(absSrc, absDst, loose)

	fmt.Fprintf(os.Stderr, "copy-sources: done — %d copied, %d skipped (dst existed) + %d loose files copied\n",
		copied, skipped, looseCount)

	return nil
}

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

func dropUnderRecursive(dirs, recursive []string) []string {
	out := dirs[:0]

	for _, d := range dirs {
		covered := false

		for _, r := range recursive {
			if d == r || strings.HasPrefix(d, r+"/") {
				covered = true

				break
			}
		}

		if !covered {
			out = append(out, d)
		}
	}

	return out
}

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

type copyJob struct {
	rel     string
	symlink bool
}

type copyStat struct {
	rel     string
	err     error
	skipped bool
}

func copySliceConcurrent(srcRoot, dst string, recursiveDirs, shallowDirs []string, onWarn func(Warn)) (copied, skipped int, err error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, 0, err
	}

	jobCh := make(chan copyJob, 256)
	statCh := make(chan copyStat, 256)

	go func() {
		defer close(jobCh)

		emit := func(rel string, typ os.FileMode) {
			switch {
			case typ&os.ModeSymlink != 0:
				jobCh <- copyJob{rel: rel, symlink: true}
			case typ.IsRegular():
				jobCh <- copyJob{rel: rel}
			}
		}

		for _, d := range recursiveDirs {
			src := filepath.Join(srcRoot, d)

			if abs, err := filepath.Abs(src); err == nil && abs == srcRoot {
				continue
			}

			fi, err := os.Lstat(src)

			if err != nil || !fi.IsDir() {
				onWarn(Warn{Kind: WarnMissingInclude, Message: "copy-sources: skip (not a directory in repo): " + d})

				continue
			}

			_ = filepath.WalkDir(src, func(p string, de os.DirEntry, err error) error {
				if err != nil {
					return nil
				}

				rel, err := filepath.Rel(srcRoot, p)

				if err != nil {
					return nil
				}

				if de.IsDir() {
					_ = os.MkdirAll(filepath.Join(dst, rel), 0o755)

					return nil
				}

				emit(rel, de.Type())

				return nil
			})
		}

		for _, d := range shallowDirs {
			src := filepath.Join(srcRoot, d)
			entries, err := os.ReadDir(src)

			if err != nil {
				onWarn(Warn{Kind: WarnMissingInclude, Message: "copy-sources: skip (not a directory in repo): " + d})

				continue
			}

			_ = os.MkdirAll(filepath.Join(dst, d), 0o755)

			for _, de := range entries {
				emit(filepath.Join(d, de.Name()), de.Type())
			}
		}
	}()

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

func copyOne(src, dst string, j copyJob) (skipped bool, err error) {
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

	return false, copyFileMode(src, dst)
}

func copyLooseFiles(srcRoot, dst string, rels []string) int {
	n := 0

	for _, rel := range rels {
		target := filepath.Join(dst, rel)

		if _, err := os.Lstat(target); err == nil {
			continue
		}

		src := filepath.Join(srcRoot, rel)
		fi, err := os.Lstat(src)

		if err != nil || fi.IsDir() {
			continue
		}

		if copyFileMode(src, target) == nil {
			n++
		}
	}

	return n
}

func copyFileMode(src, dst string) error {
	in, err := os.Open(src)

	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil
		}

		return err
	}

	defer in.Close()

	fi, err := in.Stat()

	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())

	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()

		return err
	}

	return out.Close()
}
