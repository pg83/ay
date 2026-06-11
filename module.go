package main

import (
	"strings"
)

var (
	PlatformDefaultLinuxAArch64 = MakePlatformID(OSLinux, ISAAArch64)
	PlatformDefaultLinuxX8664   = MakePlatformID(OSLinux, ISAX8664)
)

type Language string

const (
	LangCPP   Language = "cpp"
	LangProto Language = "proto"
	LangGo    Language = "go"
	LangPy    Language = "py"
	LangJava  Language = "java"
)

type ModuleKind int

const (
	KindBin ModuleKind = iota
	KindLib
)

func (k ModuleKind) string() string {
	switch k {
	case KindBin:
		return "bin"
	case KindLib:
		return "lib"
	default:
		return "unknown"
	}
}

// String implements fmt.Stringer — the fmt machinery finds it by name;
// internal code calls string().
func (k ModuleKind) String() string {
	return k.string()
}

type OS string

const (
	OSLinux   OS = "linux"
	OSDarwin  OS = "darwin"
	OSWindows OS = "windows"
)

type ISA string

const (
	ISAX8664   ISA = "x86_64"
	ISAAArch64 ISA = "aarch64"
	ISAArm64   ISA = "arm64"
)

type PlatformID string

func MakePlatformID(os OS, isa ISA) PlatformID {
	return PlatformID("default-" + string(os) + "-" + string(isa))
}

type FlagSet struct {
	NoLibc             bool
	NoUtil             bool
	NoRuntime          bool
	NoPlatform         bool
	NoCompilerWarnings bool
	NoWShadow          bool
	IsCpp              bool
}

type ModuleInstance struct {
	Path     VFS
	Kind     ModuleKind
	Language Language
	Platform *Platform
}

func NewToolInstance(host *Platform, path string) ModuleInstance {
	return ModuleInstance{
		Path:     Source(path),
		Kind:     KindBin,
		Language: LangCPP,
		Platform: host,
	}
}

func (mi ModuleInstance) string() string {
	var b strings.Builder
	b.WriteString(mi.Path.rel())
	b.WriteString("[")
	b.WriteString(mi.Kind.string())
	b.WriteString("]")
	b.WriteString(":")
	b.WriteString(string(mi.Language))
	b.WriteString("@")
	b.WriteString(string(mi.Platform.Target))

	return b.String()
}

// String implements fmt.Stringer — the fmt machinery finds it by name;
// internal code calls string().
func (mi ModuleInstance) String() string {
	return mi.string()
}
