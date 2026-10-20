package main

import "strings"

type CudaSrcsStmt struct {
	Sources       []ANY
	Architectures string
	EmbedPTX      bool
}

type CudaSettings struct {
	NvccFlags           []ANY
	Root                string
	TargetRoot          string
	Std                 string
	Version             string
	SkipCompressedDebug bool
	Sanitize            bool
	SplitSanitize       bool
}

func cudaSettings(env Environment) CudaSettings {
	version := env.string(envCUDA_VERSION)
	std := "20"

	if compareVersions(version, "12.0") < 0 {
		std = "17"
	}

	return CudaSettings{
		Root: env.string(envCUDA_ROOT), TargetRoot: env.string(envCUDA_TARGET_ROOT),
		Version: version, Std: std,
		SkipCompressedDebug: env.string(envCUDA_HOST_COMPILER_VERSION) == "14",
		Sanitize:            env.bool(envCUDA_SANITIZE), SplitSanitize: env.string(envCUDA_SANITIZE) != "",
	}
}

func cudaArchitectures(version string) string {
	var arches []string

	if compareVersions(version, "12.0") < 0 {
		arches = append(arches, "sm_35")
	}

	if compareVersions(version, "13.0") < 0 {
		arches = append(arches, "sm_50", "sm_52", "sm_60", "sm_61", "sm_70")
	}

	arches = append(arches, "sm_75")

	for _, group := range []struct {
		version string
		arches  []string
	}{
		{"11.0", []string{"sm_80"}}, {"11.1", []string{"sm_86"}},
		{"11.8", []string{"sm_89", "sm_90"}}, {"12.0", []string{"sm_90a"}},
		{"12.8", []string{"sm_100", "sm_100a", "sm_120", "sm_120a"}},
		{"12.9", []string{"sm_100f", "sm_103", "sm_103a", "sm_103f", "sm_120f"}},
		{"13.0", []string{"sm_121", "sm_121a", "sm_121f"}},
	} {
		if compareVersions(version, group.version) >= 0 {
			arches = append(arches, group.arches...)
		}
	}

	return strings.Join(arches, ":")
}

func cudaSourceBase(src string) string {
	if i := strings.LastIndexByte(src, '.'); i > strings.LastIndexByte(src, '/') {
		return src[:i]
	}

	return src
}
