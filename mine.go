package main

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"slices"

	"github.com/BurntSushi/toml"
)

func readYaConfSection(fs FS, rel, wantSection string) map[string]string {
	return yaConfStringTable(readYaConf(fs, rel)[wantSection])
}

func readYaConf(fs FS, rel string) map[string]any {
	var root map[string]any

	if _, err := toml.Decode(string(concatChunks(fs.read(rel))), &root); err != nil {
		fmt.Fprintf(os.Stderr, "\x1b[33mWarn: Failed to load %s: %v\x1b[0m\n", rel, err)

		return nil
	}

	return root
}

func yaConfStringTable(v any) map[string]string {
	tbl, ok := v.(map[string]any)

	if !ok {
		return map[string]string{}
	}

	out := make(map[string]string, len(tbl))

	for _, k := range slices.Sorted(maps.Keys(tbl)) {
		out[k] = yaConfFlagValue(tbl[k])
	}

	return out
}

func yaConfFlagValue(v any) string {
	s, ok := v.(string)

	if !ok {
		throwFmt("can only concatenate str (not \"%s\") to str", pythonTypeName(v))
	}

	return s
}

func pythonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case int64, int:
		return "int"
	case float64:
		return "float"
	case []any, []map[string]any:
		return "list"
	case map[string]any:
		return "dict"
	}

	return fmt.Sprintf("%T", v)
}

func hostOS() OS {
	return OS(chaosHostOS(runtime.GOOS))
}

func hostISA() ISA {
	goarch := chaosHostArch(runtime.GOARCH)

	switch goarch {
	case "amd64":
		return ISAX8664
	case "arm64":
		if goos := chaosHostOS(runtime.GOOS); goos == "darwin" || goos == "ios" {
			return ISAArm64
		}

		return ISAAArch64
	default:
		return ISA(goarch)
	}
}

func resolvePlatform(s string) (OS, ISA) {
	if s == "" {
		return hostOS(), hostISA()
	}

	return parsePlatformID(s)
}
