package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdPerf(args []string) int {
	if len(args) < 2 || args[0] != "parser" {
		fmt.Fprintln(os.Stderr, "usage: ay perf parser <dir>")

		return 2
	}

	stop := startProfilesFromEnv()

	defer stop()

	return perfParser(args[1])
}

func cParserSource(path string) bool {
	switch {
	case strings.HasSuffix(path, ".cpp"),
		strings.HasSuffix(path, ".cc"),
		strings.HasSuffix(path, ".cxx"),
		strings.HasSuffix(path, ".c"),
		strings.HasSuffix(path, ".h"),
		strings.HasSuffix(path, ".hpp"),
		strings.HasSuffix(path, ".hxx"):
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
	iters, sink := 0, 0

	for time.Since(start) < minDur {
		for _, b := range datas {
			sink += parseCIncludes(b, block, 0)
		}

		iters++
	}

	dur := time.Since(start)
	_ = sink

	perPass := dur / time.Duration(iters)
	fmt.Printf("files=%d bytes=%d iters=%d per-pass=%v (%.0f MB/s)\n",
		len(datas), total, iters, perPass,
		float64(total)/(float64(perPass)/float64(time.Second))/1e6)

	return 0
}
