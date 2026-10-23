package main

import (
	"path"
	"strings"
)

func allPytestSrcArgs(fs FS, module string, stmt UnknownStmt) []ANY {
	var dirs []string

	recursive, onlyTests := false, false

	for i := 0; i < len(stmt.Args); i++ {
		switch arg := stmt.Args[i].string(); arg {
		case "TOP_LEVEL":
		case "NAMESPACE":
			i++

			if i == len(stmt.Args) {
				throwFmt("ALL_PYTEST_SRCS NAMESPACE expects a value")
			}
		case "RECURSIVE":
			recursive = true
		case "ONLY_TEST_FILES":
			onlyTests = true
		default:
			dirs = append(dirs, arg)
		}
	}

	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	patterns := []string{"*.py"}

	if onlyTests {
		patterns = []string{"test_*.py", "*_test.py"}
	}

	patterns = append(patterns, "ya.make")

	var files []VFS

	for _, suffix := range patterns {
		for _, dir := range dedupKeepOrder(dirs) {
			pattern := path.Join(module, dir)

			if recursive {
				pattern += "/**"
			}

			for _, file := range ymakeGlob(fs, pattern+"/"+suffix) {
				rel := strings.TrimPrefix(file, module+"/")

				if rel == "ya.make" {
					continue
				}

				if path.Base(rel) == "ya.make" {
					throwFmt("ALL_PYTEST_SRCS: glob selected nested ya.make %s", rel)
				}

				files = append(files, source(file))
			}
		}
	}

	args := []ANY{strMr.any()}

	for _, file := range dedup(files) {
		args = append(args, file.any())
	}

	return args
}
