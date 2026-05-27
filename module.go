package main

import (
	"sort"
	"strings"
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

func (k ModuleKind) String() string {
	switch k {
	case KindBin:
		return "bin"
	case KindLib:
		return "lib"
	default:
		return "unknown"
	}
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

var (
	PlatformDefaultLinuxAArch64 = MakePlatformID(OSLinux, ISAAArch64)
	PlatformDefaultLinuxX8664   = MakePlatformID(OSLinux, ISAX8664)
)

type FlagSet struct {
	NoLibc             bool
	NoUtil             bool
	NoRuntime          bool
	NoPlatform         bool
	NoCompilerWarnings bool
	NoWShadow          bool
	IsCpp              bool

	Extra string
}

func NewFlagSet(extra ...string) FlagSet {
	if len(extra) == 0 {
		return FlagSet{}
	}

	e := append([]string{}, extra...)
	sort.Strings(e)

	return FlagSet{Extra: strings.Join(e, "\n")}
}

type ModuleInstance struct {
	Path     string
	Kind     ModuleKind
	Language Language
	Platform *Platform
}

func NewToolInstance(host *Platform, path string) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Kind:     KindBin,
		Language: LangCPP,
		Platform: host,
	}
}

func (mi ModuleInstance) String() string {
	var b strings.Builder
	b.WriteString(mi.Path)
	b.WriteString("[")
	b.WriteString(mi.Kind.String())
	b.WriteString("]")
	b.WriteString(":")
	b.WriteString(string(mi.Language))
	b.WriteString("@")
	b.WriteString(string(mi.Platform.Target))

	return b.String()
}
