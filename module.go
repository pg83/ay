package main

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

type ModuleKind int

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
