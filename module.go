package main

import (
	"strings"
)

var (
	PlatformDefaultLinuxAArch64 = makePlatformID(OSLinux, ISAAArch64)
	PlatformDefaultLinuxX8664   = makePlatformID(OSLinux, ISAX8664)
)

type Language int

const (
	// LangNone is the zero value: an instance without an explicit language
	// (the pre-enum "" string).
	LangNone Language = iota
	LangCPP
	LangProto
	LangGo
	LangPy
	LangJava
)

func (l Language) string() string {
	switch l {
	case LangNone:
		return ""
	case LangCPP:
		return "cpp"
	case LangProto:
		return "proto"
	case LangGo:
		return "go"
	case LangPy:
		return "py"
	case LangJava:
		return "java"
	}

	throwFmt("Language.string: unknown language %d", int(l))

	return ""
}

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

func makePlatformID(os OS, isa ISA) PlatformID {
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

func newToolInstance(host *Platform, path string) ModuleInstance {
	return ModuleInstance{
		Path:     source(path),
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
	b.WriteString(mi.Language.string())
	b.WriteString("@")
	b.WriteString(string(mi.Platform.Target))

	return b.String()
}

// String implements fmt.Stringer — the fmt machinery finds it by name;
// internal code calls string().
func (mi ModuleInstance) String() string {
	return mi.string()
}
