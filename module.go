package main

// module.go — module-instance addressing (D30..D40).
//
// Per D30, a module's identity is the tuple (Path, Language, Target,
// Flags). Two distinct platforms (target aarch64 vs host x86_64) of
// the same `build/cow/on` directory are two different
// `ModuleInstance`s — they emit two different node sets, addressed by
// distinct memo keys, both flowing through one Emitter into one
// Graph.
//
// PR-23 only ships the address structure. The macro-driven flag
// inference (PR-25) and per-instance ADDINCL/CFLAGS digesting (M5)
// extend `Flags.Extra` rather than mutating this file.
//
// Why these specific types:
//
//   - `Language` is a string newtype now so the parser tag (cpp / proto
//     / go / py / java) flows through everything as a single token,
//     matched by the comparator's `module_lang`. M2 only emits cpp; the
//     rest are reserved per D30 §3.
//   - `PlatformID` is a string newtype identical to the on-disk
//     `node.platform` field. Comparing `node.Platform == string(id)`
//     stays a one-line operation.
//   - `FlagSet` is a small typed bag, comparable by value. Its
//     `Extra` field is a `\n`-joined sorted concatenation (string, not
//     []string, because slice fields disqualify a struct from being a
//     map key per D34). `NewFlagSet` is the only sanctioned builder;
//     direct literals are legal but skipping the sort is a defect.

import (
	"sort"
	"strings"
)

// Language is the parser-level tag identifying which rule emitter a
// module's ya.make routes through. M2 only exercises LangCPP; the
// other constants are reserved so the addressing scheme is forward-
// compatible with M5+ language polymorphism (D35).
type Language string

const (
	LangCPP   Language = "cpp"
	LangProto Language = "proto" // reserved for M5+ proto rules
	LangGo    Language = "go"    // reserved
	LangPy    Language = "py"    // reserved
	LangJava  Language = "java"  // reserved
)

// PlatformID is the on-disk `node.platform` string verbatim. The two
// canonical M2 values match the reference graph's
// `default-linux-aarch64` (target) and `default-linux-x86_64` (host).
type PlatformID string

const (
	PlatformDefaultLinuxAArch64 PlatformID = "default-linux-aarch64"
	PlatformDefaultLinuxX8664   PlatformID = "default-linux-x86_64"
)

// FlagSet is the per-instance flag bag. Booleans capture the M2 macro
// vocabulary (NO_LIBC / NO_UTIL / NO_RUNTIME / NO_PLATFORM /
// NO_COMPILER_WARNINGS) plus the host/target axis (PIC = host build).
// `IsCpp` is the per-source language tag the macro evaluator (PR-25)
// will set; PR-23 only constructs C-language instances.
//
// `Extra` is reserved for ADDINCL/CFLAGS digests in M5. PR-23 leaves
// it nil/empty; `NewFlagSet` enforces the sort discipline so future
// equality checks (memo lookup) are stable regardless of declaration
// order.
type FlagSet struct {
	NoLibc             bool
	NoUtil             bool
	NoRuntime          bool
	NoPlatform         bool
	NoCompilerWarnings bool
	IsCpp              bool
	PIC                bool
	// LibcMusl marks an instance as a member of the musl-libc subtree
	// (PR-32 D02). It is the dispatch key for the musl flavours of
	// EmitCC's composer pick (replaces the old path-prefix test
	// `instance.Path == "contrib/libs/musl" || HasPrefix(...musl/)`).
	// Today seeded heuristically by `inferFlagsFromPath` for any path
	// at or under `contrib/libs/musl`; M5+ will replace the heuristic
	// with macro-driven inference (parsing NO_PLATFORM + SET(MUSL no)
	// in the ya.make).
	LibcMusl bool
	// Extra carries opaque per-instance digests that two
	// ModuleInstances with otherwise-equal fields use to
	// disambiguate. Stored as a `\n`-joined sorted token
	// concatenation rather than a `[]string` because Go does not
	// allow slice-typed fields in a struct used as a map key.
	// Populate via NewFlagSet to ensure the sort discipline; PR-23
	// leaves it empty for every instance, M5's ADDINCL/CFLAGS
	// digest will start writing real tokens.
	Extra string
}

// NewFlagSet returns a FlagSet whose `Extra` field is the
// `\n`-joined sorted concatenation of the caller's varargs. The
// caller assigns the boolean fields on the returned struct directly.
//
// Why a joined string instead of `[]string`: slice fields
// disqualify a struct from being a map key, and ModuleInstance MUST
// be a map key (D34 — `genCtx.memo: map[ModuleInstance]...`). The
// `\n` separator is safe because flag tokens never contain
// newlines.
func NewFlagSet(extra ...string) FlagSet {
	if len(extra) == 0 {
		return FlagSet{}
	}

	e := append([]string{}, extra...)
	sort.Strings(e)

	return FlagSet{Extra: strings.Join(e, "\n")}
}

// ModuleInstance is the comparable-by-value identity of one rule-
// emission target (D30). Walker memoisation, cycle detection, and
// host-tool recursion all key on this struct.
type ModuleInstance struct {
	Path     string
	Language Language
	Target   PlatformID
	Flags    FlagSet
}

// WithHost returns a copy of mi with `Target` flipped to cfg.Host's
// platform ID and `Flags.PIC=true`. The same Path/Language carry over;
// the host instance shares the source tree, only the codegen flavour
// changes.
//
// D41: co-setting Flags.PIC=true is policy (M2/M3 host PROGRAMs use
// PIC), not derivation; emitter sites dispatch on Target, not PIC.
func (mi ModuleInstance) WithHost(cfg PlatformConfig) ModuleInstance {
	out := mi
	out.Target = cfg.Host.ID
	out.Flags.PIC = true

	return out
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
	b.WriteString(string(mi.Target))

	return b.String()
}

// inferFlagsFromPath is the M2 stopgap that derives a FlagSet from a
// module path alone, without parsing the module's ya.make. PR-25
// replaces this with macro-driven inference (NO_LIBC / NO_UTIL / etc.
// from the parsed UnknownStmt set). For PR-23 the heuristic is
// sufficient because the only modules walked end-to-end are the
// build/cow/on leaf (M1 acceptance) and the synthetic test fixtures
// (which never hit the heuristic — Gen calls inferFlagsFromPath only
// on the seed instance, and tests construct their own ModuleInstance
// directly).
//
// The `isPIC` parameter threads the host/target axis from the caller
// (Gen seeds target = false; future host-tool recursion will seed
// host = true).
func inferFlagsFromPath(path string, isPIC bool) FlagSet {
	fs := FlagSet{PIC: isPIC}

	if path == "build/cow/on" {
		fs.NoLibc = true
		fs.NoUtil = true
		fs.NoRuntime = true
	}

	// PR-32 D02: musl-prefix path heuristic doubles as the M2-stopgap
	// seed for `Flags.LibcMusl`. The path test is the SOLE remaining
	// musl-path-prefix dispatch in the codebase — every other call
	// site reads `Flags.LibcMusl` instead. Documented removable in
	// M5+ when ya.make-driven flag inference (parsing NO_PLATFORM +
	// path-conditional SET(MUSL no) etc.) replaces this heuristic.
	//
	// PR-39: contrib/libs/musl/full is a normal LIBRARY (ya.make has
	// SET(MUSL no)) that bridges the musl ABI to glibc — it must NOT
	// receive the musl CC bundle. It still carries NO_LIBC/NO_UTIL/
	// NO_RUNTIME (keeping effectiveNoPlatform=true to suppress default
	// peer injection); only LibcMusl is withheld so the CC dispatch
	// routes to composeTargetCC. TODO: remove when SET() parsing
	// lands in M3+.
	if path == "contrib/libs/musl" || strings.HasPrefix(path, "contrib/libs/musl/") {
		fs.NoLibc = true
		fs.NoUtil = true
		fs.NoRuntime = true

		if path != "contrib/libs/musl/full" {
			fs.LibcMusl = true
		}
	}

	return fs
}
