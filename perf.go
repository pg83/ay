package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func cmdPerfParser(_ GlobalFlags, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ay perf parser <dir>")

		return 2
	}

	return perfParser(args[0])
}

func cmdPerfDarts(_ GlobalFlags, args []string) int {
	return perfDarts()
}

func cmdPerfLink(_ GlobalFlags, args []string) int {
	count, size := 5000, 8192

	if len(args) >= 1 {
		count = throw2(strconv.Atoi(args[0]))
	}

	if len(args) >= 2 {
		size = throw2(strconv.Atoi(args[1]))
	}

	return perfLink(count, size)
}

func perfLink(count, size int) int {
	root := throw2(os.MkdirTemp("", "ay-perf-link-*"))

	defer func() { throw(os.RemoveAll(root)) }()

	cas := filepath.Join(root, "cas")

	throw(os.MkdirAll(cas, 0o755))

	blob := make([]byte, size)
	srcs := make([]string, count)

	for i := range srcs {
		p := filepath.Join(cas, strconv.Itoa(i))

		throw(os.WriteFile(p, blob, 0o444))
		srcs[i] = p
	}

	bench := func(name string, materialize func(src, dst string) error) {
		const minDur = 2 * time.Second

		out := filepath.Join(root, "out")

		var dur time.Duration

		iters := 0

		for dur < minDur {
			throw(os.MkdirAll(out, 0o755))

			start := time.Now()

			for i, src := range srcs {
				throw(materialize(src, filepath.Join(out, strconv.Itoa(i))))
			}

			dur += time.Since(start)
			iters++

			throw(os.RemoveAll(out))
		}

		ops := int64(iters) * int64(count)

		fmt.Printf("%-8s %5d files x %3d iters: %8.0f ns/op  %10.0f ops/s\n",
			name, count, iters, float64(dur.Nanoseconds())/float64(ops), float64(ops)/dur.Seconds())
	}

	fmt.Printf("materialize %d files of %d bytes from CAS:\n", count, size)
	bench("link", os.Link)
	bench("symlink", os.Symlink)
	bench("copy", copyFile)

	return 0
}

func cParserSource(path string) bool {
	switch {
	case extIsCOrHeaderSource(path):
		return true
	}

	return false
}

func perfParser(dir string) int {
	var (
		datas [][]byte
		total int64
	)

	throw(filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || !cParserSource(p) {
			return nil
		}

		datas = append(datas, throw2(os.ReadFile(p)))
		total += int64(len(datas[len(datas)-1]))

		return nil
	}))

	block := make([]IncludeDirective, directiveBlockHint)

	for _, b := range datas {
		parseCIncludes(b, block, 0)
	}

	const minDur = 3 * time.Second

	start := time.Now()
	iters := 0

	for time.Since(start) < minDur {
		for _, b := range datas {
			parseCIncludes(b, block, 0)
		}

		iters++
	}

	dur := time.Since(start)
	perPass := dur / time.Duration(iters)

	fmt.Printf("files=%d bytes=%d iters=%d per-pass=%v (%.0f MB/s)\n",
		len(datas), total, iters, perPass,
		float64(total)/(float64(perPass)/float64(time.Second))/1e6)

	return 0
}
