package main

// module.go — module-instance addressing.
//
// A module's identity is the tuple (Path, Language, Platform, Flags). Two
// platforms of the same directory are two different ModuleInstances —
// distinct memo keys, separate node sets, one Graph.
//
// Language is a string newtype so the parser tag (cpp/proto/go/py/java)
// flows through as one token. PlatformID is a string newtype identical to
// on-disk node.platform. FlagSet is a comparable-by-value typed bag whose
// Extra field is a `\n`-joined sorted concatenation (string, not []string,
// because slice fields disqualify a struct from being a map key);
// NewFlagSet enforces the sort discipline.

import (
	"sort"
	"strings"
)

// Language is the parser-level tag picking which rule emitter a module
// routes through. Only LangCPP is exercised today; the other constants
// reserve forward-compatibility for language polymorphism.
type Language string

const (
	LangCPP   Language = "cpp"
	LangProto Language = "proto" // reserved for M5+ proto rules
	LangGo    Language = "go"    // reserved
	LangPy    Language = "py"    // reserved
	LangJava  Language = "java"  // reserved
)

// OS names the operating system axis of a Platform. Surfaced into
// the on-disk `node.platform` string via MakePlatformID.
type OS string

const (
	OSLinux   OS = "linux"
	OSDarwin  OS = "darwin"
	OSWindows OS = "windows"
)

// ISA names the instruction-set axis, independent of OS. Separate from OS
// avoids the combinatorial explosion of a single `<OS>-<ISA>` enum.
type ISA string

const (
	ISAX8664   ISA = "x86_64"
	ISAAArch64 ISA = "aarch64"
	ISAArm64   ISA = "arm64"
)

// PlatformID is the on-disk `node.platform` string, composed via
// MakePlatformID from an OS + ISA pair. Reference values include
// `default-linux-aarch64` and `default-linux-x86_64`.
type PlatformID string

// MakePlatformID composes the canonical `default-<os>-<isa>` form.
func MakePlatformID(os OS, isa ISA) PlatformID {
	return PlatformID("default-" + string(os) + "-" + string(isa))
}

var (
	PlatformDefaultLinuxAArch64 = MakePlatformID(OSLinux, ISAAArch64)
	PlatformDefaultLinuxX8664   = MakePlatformID(OSLinux, ISAX8664)
)

// FlagSet is the per-instance flag bag. Booleans capture the macro
// vocabulary (NO_LIBC / NO_UTIL / NO_RUNTIME / NO_PLATFORM /
// NO_COMPILER_WARNINGS) plus the host/target axis (PIC = host build).
// IsCpp is the per-source language tag; Extra reserves space for
// ADDINCL/CFLAGS digests. NewFlagSet enforces sort discipline so
// equality checks stay stable regardless of declaration order.
type FlagSet struct {
	NoLibc             bool
	NoUtil             bool
	NoRuntime          bool
	NoPlatform         bool
	NoCompilerWarnings bool
	IsCpp              bool
	PIC                bool
	// NoStdInc marks modules whose own compiles declare -nostdinc.
	// This is a generic compile property parsed from CFLAGS, used for
	// scanner base-path and no-stdinc compile-shape decisions.
	NoStdInc bool
	// Extra carries opaque per-instance digests as a `\n`-joined sorted
	// token concatenation (slice fields would disqualify the struct from
	// being a map key). Populate via NewFlagSet to keep the sort stable.
	Extra string
}

// NewFlagSet returns a FlagSet whose Extra is the `\n`-joined sorted
// concatenation of the varargs. The caller assigns boolean fields directly.
// Joined string (not []string) because slice fields would disqualify
// ModuleInstance from being a map key; `\n` is safe because flag tokens
// never contain newlines.
func NewFlagSet(extra ...string) FlagSet {
	if len(extra) == 0 {
		return FlagSet{}
	}

	e := append([]string{}, extra...)
	sort.Strings(e)

	return FlagSet{Extra: strings.Join(e, "\n")}
}

// ModuleInstance is the comparable-by-value identity of one rule-emission
// target. Walker memoisation, cycle detection, and host-tool recursion all
// key on this struct. Platform is one of the two CLI-constructed singletons
// (host or target) — pointer equality is well-defined and ModuleInstance
// remains a valid map key.
type ModuleInstance struct {
	Path     string
	Language Language
	Platform *Platform
	Flags    FlagSet
}

// NewToolInstance builds a ModuleInstance for a host-platform tool at
// `path`. Flags are inferred from path (via inferFlagsFromPath) so the
// tool's own ya.make properties — not the caller's — drive dispatch.
// `lang` carries over because language is a rule-engine selector, not a
// per-module fact. The older "swap path on a copied instance" pattern
// produced instances whose Flags described the wrong ya.make.
func NewToolInstance(host *Platform, path string, lang Language) ModuleInstance {
	return ModuleInstance{
		Path:     path,
		Language: lang,
		Platform: host,
		Flags:    inferFlagsFromPath(path, true),
	}
}

// String returns a stable diagnostic representation in the form
// "<path>:<lang>@<platform>" (flag bag elided). Used in error
// messages and ledger entries.
func (mi ModuleInstance) String() string {
	var b strings.Builder
	b.WriteString(mi.Path)
	b.WriteString(":")
	b.WriteString(string(mi.Language))
	b.WriteString("@")
	b.WriteString(string(mi.Platform.Target))

	return b.String()
}

// inferFlagsFromPath is the path-only stopgap that derives a FlagSet
// without parsing the module's ya.make. Replacement: macro-driven
// inference from the parsed UnknownStmt set. isPIC threads the
// host/target axis from the caller (target = false; host = true).
func inferFlagsFromPath(path string, isPIC bool) FlagSet {
	fs := FlagSet{PIC: isPIC}

	if path == "build/cow/on" {
		fs.NoLibc = true
		fs.NoUtil = true
		fs.NoRuntime = true
	}

	return fs
}
