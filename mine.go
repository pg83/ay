package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

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

	for k, val := range tbl {
		if s, ok := yaConfScalar(val); ok {
			out[k] = s
		}
	}

	return out
}

func yaConfScalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), true
	}

	return "", false
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
