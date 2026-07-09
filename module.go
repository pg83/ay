package main

import (
	"strings"
)

var (
	PlatformDefaultLinuxAArch64 = makePlatformID(OSLinux, ISAAArch64)
	PlatformDefaultLinuxX8664   = makePlatformID(OSLinux, ISAX8664)
)

const (
	OSLinux    OS  = "linux"
	OSDarwin   OS  = "darwin"
	OSWindows  OS  = "windows"
	ISAX8664   ISA = "x86_64"
	ISAAArch64 ISA = "aarch64"
	ISAArm64   ISA = "arm64"
)

const (
	LangNone Language = iota
	LangCPP
	LangProto
	LangGo
	LangPy
	LangJava

	LangDescProto
)

const (
	KindBin ModuleKind = iota
	KindLib
)

const (
	demandNone ModuleDemand = iota
	demandSelf
	demandLinked
)

type Language int

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
	case LangDescProto:
		return "desc_proto"
	}

	throwFmt("Language.string: unknown language %d", int(l))

	return ""
}

type ModuleKind int

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

func (k ModuleKind) String() string {
	return k.string()
}

type OS string

type ISA string

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
	NoExportDynSymbols bool
	IsCpp              bool
}

type ModuleDemand uint8

type ModuleInstance struct {
	Path     VFS
	Kind     ModuleKind
	Language Language
	Platform *Platform
	Demand   ModuleDemand
}

func newToolInstance(host *Platform, path string) ModuleInstance {
	return ModuleInstance{
		Path:     source(path),
		Kind:     KindBin,
		Demand:   demandLinked,
		Language: LangCPP,
		Platform: host,
	}
}

func (mi ModuleInstance) string() string {
	var b strings.Builder
	b.WriteString(mi.Path.relString())
	b.WriteString("[")
	b.WriteString(mi.Kind.string())
	b.WriteString("]")
	b.WriteString(":")
	b.WriteString(mi.Language.string())
	b.WriteString("@")
	b.WriteString(string(mi.Platform.Target))

	return b.String()
}

func (mi ModuleInstance) String() string {
	return mi.string()
}
