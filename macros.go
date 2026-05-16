package main

import "fmt"

// macros.go — IF-predicate evaluator and the bound-variable environment
// for parsed `*IfStmt` branches. Expr ADT lives next to the parser in
// yamake.go; this file owns the evaluator and DefaultIfEnv.
//
// Contract: an unknown identifier throws. Silent default-to-false hides
// "we never bound this var" until the comparator reports a missing node.
//
// Value space is {bool, string, int}; Environment carries three typed
// maps with Lookup fallthrough bool → string → int (in practice each
// binding lives in exactly one map). Bool-only paths stay the hot path.

// Environment binds IF-condition identifiers to their typed values.
// Three disjoint maps so comparators (ExprEq, ExprLt) resolve operands of
// differing types. Boolean-only callers (ExprIdent in predicate position,
// ExprAnd/Or/Not) go through Bool, which throws on a non-bool binding.
type Environment struct {
	bools   map[string]bool
	strings map[string]string
	ints    map[string]int
}

// Lookup returns the typed value bound to name (or throws). Return type
// is `any` because callers (evalAtom, ExprEq operand resolution) branch
// on dynamic type. EvalCond's ExprIdent path goes through Bool for a
// typed value and a clean error.
func (e Environment) Lookup(name string) any {
	if v, ok := e.bools[name]; ok {
		return v
	}

	if v, ok := e.strings[name]; ok {
		return v
	}

	if v, ok := e.ints[name]; ok {
		return v
	}

	ThrowFmt("macros: unknown IF identifier %q (extend env in macros.go's DefaultIfEnv)", name)

	return nil // unreachable; ThrowFmt panics
}

// Bool returns the boolean binding for name. Bool bindings return directly;
// strings coerce (empty → false, non-empty → true) per upstream semantics
// for bare-ident use of a string variable. Int bindings in bool position
// throw (defect).
func (e Environment) Bool(name string) bool {
	if v, ok := e.bools[name]; ok {
		return v
	}

	if v, ok := e.strings[name]; ok {
		return v != ""
	}

	if _, ok := e.ints[name]; ok {
		ThrowFmt("macros: identifier %q has int binding but is used in boolean position", name)
	}

	ThrowFmt("macros: unknown IF identifier %q (extend env in macros.go's DefaultIfEnv)", name)

	return false // unreachable
}

func (e Environment) String(name string) string {
	if v, ok := e.strings[name]; ok {
		return v
	}

	if v, ok := e.bools[name]; ok {
		if v {
			return "yes"
		}

		return "no"
	}

	if v, ok := e.ints[name]; ok {
		return fmt.Sprintf("%d", v)
	}

	ThrowFmt("macros: unknown IF identifier %q (extend env in macros.go's DefaultIfEnv)", name)

	return "" // unreachable
}

// Clone returns a deep copy so callers can mutate per-instance (e.g. flip
// ARCH_AARCH64 ↔ ARCH_X86_64 for host targets) without trampling
// DefaultIfEnv. Maps are copied; contents are immutable scalars.
func (e Environment) Clone() Environment {
	out := Environment{
		bools:   make(map[string]bool, len(e.bools)),
		strings: make(map[string]string, len(e.strings)),
		ints:    make(map[string]int, len(e.ints)),
	}

	for k, v := range e.bools {
		out.bools[k] = v
	}

	for k, v := range e.strings {
		out.strings[k] = v
	}

	for k, v := range e.ints {
		out.ints[k] = v
	}

	return out
}

// SetBool overrides (or adds) a boolean binding. Per-instance env tweaks
// must Clone first to avoid leaking into DefaultIfEnv.
func (e Environment) SetBool(name string, v bool) {
	delete(e.strings, name)
	delete(e.ints, name)

	e.bools[name] = v
}

func (e Environment) SetString(name, v string) {
	delete(e.bools, name)
	delete(e.ints, name)

	e.strings[name] = v
}

func (e Environment) SetFromString(name, v string) {
	switch v {
	case "yes":
		e.SetBool(name, true)
	case "no":
		e.SetBool(name, false)
	default:
		e.SetString(name, v)
	}
}

// HasBinding reports whether name has any typed binding in this env.
// Used by the DEFAULT(name value) statement to mirror upstream's
// `if (vars.Get1(args[0]).empty())` no-op-on-pre-existing semantics
// (see devtools/ymake/lang/eval_context.cpp:335-339).
func (e Environment) HasBinding(name string) bool {
	if _, ok := e.bools[name]; ok {
		return true
	}

	if _, ok := e.strings[name]; ok {
		return true
	}

	if _, ok := e.ints[name]; ok {
		return true
	}

	return false
}

// SetDefaultString implements DEFAULT(name value): binds only when no
// prior binding exists, matching upstream
// TEvalContext::SetStatement/NMacro::DEFAULT. Value is string; later
// `IF (name)` coerces via Bool, `IF (name == "v")` via evalEq.
func (e Environment) SetDefaultString(name, value string) {
	if e.HasBinding(name) {
		return
	}

	e.strings[name] = value
}

// EvalCond evaluates an IF predicate against a fixed env. Throws on:
// unknown identifier; unhandled Expr type; bare ExprString/ExprInt in
// predicate position; ExprEq/ExprLt with mismatched or non-numeric operands.
func EvalCond(e Expr, env Environment) bool {
	switch x := e.(type) {
	case *ExprIdent:
		if x.Name == "yes" {
			return true
		}
		if x.Name == "no" {
			return false
		}

		return env.Bool(x.Name)
	case *ExprNot:
		return !EvalCond(x.Of, env)
	case *ExprAnd:
		return EvalCond(x.Left, env) && EvalCond(x.Right, env)
	case *ExprOr:
		return EvalCond(x.Left, env) || EvalCond(x.Right, env)
	case *ExprString:
		ThrowFmt("macros: bare string %q cannot be evaluated as a boolean condition", x.Value)
	case *ExprInt:
		ThrowFmt("macros: bare integer %d cannot be evaluated as a boolean condition", x.Value)
	case *ExprEq:
		return evalEq(x, env)
	case *ExprLt:
		return evalLt(x, env)
	}

	ThrowFmt("macros: unhandled Expr type %T", e)

	return false // unreachable; ThrowFmt panics
}

// evalAtom resolves a value-position Expr (operand of `==` or `<`) to its
// dynamic value. ExprIdent → Lookup; literal nodes return carried values.
// Anything else (bool combinator / nested comparator) throws — the parser
// grammar should not produce such a shape.
func evalAtom(e Expr, env Environment) any {
	switch x := e.(type) {
	case *ExprIdent:
		if x.Name == "yes" || x.Name == "no" {
			return x.Name
		}

		return env.Lookup(x.Name)
	case *ExprString:
		return x.Value
	case *ExprInt:
		return x.Value
	}

	ThrowFmt("macros: unexpected Expr type %T in comparator operand position", e)

	return nil // unreachable
}

// evalEq compares two atoms for equality. Same-type only; mixed types
// throw so a parser-level type confusion surfaces immediately.
func evalEq(x *ExprEq, env Environment) bool {
	l := evalAtom(x.Left, env)
	r := evalAtom(x.Right, env)

	switch lv := l.(type) {
	case string:
		rv, ok := r.(string)

		if !ok {
			ThrowFmt("macros: == operand type mismatch: left is string %q, right is %T", lv, r)
		}

		return lv == rv
	case int:
		rv, ok := r.(int)

		if !ok {
			ThrowFmt("macros: == operand type mismatch: left is int %d, right is %T", lv, r)
		}

		return lv == rv
	case bool:
		rv, ok := r.(bool)

		if !ok {
			ThrowFmt("macros: == operand type mismatch: left is bool %v, right is %T", lv, r)
		}

		return lv == rv
	}

	ThrowFmt("macros: == operand has unsupported dynamic type %T", l)

	return false // unreachable
}

// evalLt enforces numeric `<`. Both sides must be int; anything else
// throws.
func evalLt(x *ExprLt, env Environment) bool {
	l := evalAtom(x.Left, env)
	r := evalAtom(x.Right, env)

	li, lok := l.(int)
	ri, rok := r.(int)

	if !lok || !rok {
		ThrowFmt("macros: < requires int operands, got left=%T right=%T", l, r)
	}

	return li < ri
}

// DefaultIfEnv is the bound-variable environment for `IF` predicates —
// per-build bindings independent of instance.Platform.ISA. Every ARCH_*
// defaults to false; buildIfEnv (modules.go) flips the matching ISA's
// bits per instance. Other shape (OS_LINUX, CLANG, MUSL, …) reflects the
// reference closure's build configuration (linux + clang + musl).
// Extending this set is the documented way to teach EvalCond about a new
// identifier.
//
// Typed bindings include CXX_RT="libcxxrt" (SET-resolved selector for
// linux + non-Android + non-ARM6/7, wired directly because SET is not
// evaluated), SANITIZER_TYPE="" (sanitizers off), bare-ident sanitizer
// literals each = own name, and ANDROID_API=0 (defensive for libc_compat
// `<` branches gated by OS_ANDROID=false).
var DefaultIfEnv = Environment{
	bools: map[string]bool{
		"OS_LINUX":      true,
		"OS_WINDOWS":    false,
		"OS_DARWIN":     false,
		"OS_IOS":        false,
		"OS_ANDROID":    false,
		"OS_EMSCRIPTEN": false,
		"OS_FREEBSD":    false, // PR-27: contrib/libs/cxxsupp/libcxx
		"OS_CYGWIN":     false, // PR-27: util
		"SUN":           false, // PR-27: util
		"CYGWIN":        false, // PR-27: util
		// ARCH_ARM64 is the upstream alias for ARCH_AARCH64; buildIfEnv
		// flips them in lockstep so IF predicates see consistent bindings.
		"ARCH_AARCH64":                      false,
		"ARCH_X86_64":                       false,
		"ARCH_I386":                         false,
		"ARCH_ARM7":                         false,
		"ARCH_ARM64":                        false,
		"ARCH_ARM6":                         false,
		"ARCH_WASM32":                       false, // PR-27: contrib/libs/libunwind
		"ARCH_WASM64":                       false, // PR-27: contrib/libs/libunwind
		"CLANG":                             true,
		"CLANG_CL":                          false,
		"GCC":                               false,
		"MSVC":                              false,
		"MUSL":                              true,
		"USE_EAT_MY_DATA":                   false,
		"WITH_MAPKIT":                       false,
		"WITH_VALGRIND":                     false,
		"TSTRING_IS_STD_STRING":             false,
		"NO_CUSTOM_CHAR_PTR_STD_COMPARATOR": false,
		"NEED_CHECK":                        false,
		"TRUE":                              true,
		"FALSE":                             false,
		"FUZZING":                           false, // PR-27: contrib/libs/cxxsupp/libcxxrt
		"EXPORT_CMAKE":                      false, // PR-27: contrib/libs/cxxsupp/libcxxabi-parts
		"NO_CXX_RTTI":                       false, // PR-27: contrib/libs/cxxsupp/libcxxrt
		"NO_CXX_EXCEPTIONS":                 false, // PR-27: contrib/libs/cxxsupp/libcxxrt
		"USE_ARCADIA_COMPILER_RUNTIME":      false, // PR-27: library/cpp/sanitizer/include
		"PROVIDE_REALLOCARRAY":              false, // PR-27: contrib/libs/libc_compat (DEFAULT-set, M2 default = no)
		"PROVIDE_GETRANDOM_GETENTROPY":      false, // PR-27: contrib/libs/libc_compat
		"PROVIDE_QUEUE":                     false, // PR-27: contrib/libs/libc_compat
		"PROVIDE_GETSERVBYNAME":             false, // PR-27: contrib/libs/libc_compat
		"PROVIDE_MEMFD_CREATE":              false, // PR-27: contrib/libs/libc_compat
		"MUSL_LITE":                         false, // PR-30 D01: M2 default = full musl, not lite. Read by D02's defaultProgramPeerdirsFor to pick contrib/libs/musl/full when MUSL=yes && !MUSL_LITE.
		"OPENSOURCE_REPLACE_LINUX_HEADERS":  false, // PR-30: contrib/libs/linux-headers (used in IF(X AND EXPORT_CMAKE)).
		// OPENSOURCE=true: this source tree is the open-source Arcadia
		// export. Reference sg2.json was built from it, so IF(NOT
		// OPENSOURCE) branches that PEERDIR internal-only modules (e.g.
		// library/cpp/xml/document) must take the false arm.
		"OPENSOURCE":        true,  // M3: open-source Arcadia export (sg2.json reference).
		"YA_OPENSOURCE":     false, // M3: ya-tool open-source build flag.
		"EXTERNAL_PY_FILES": false, // M3: library/python/runtime_py3 external-py variant.
		// USE_ARCADIA_PYTHON=true: reference sg2.json was generated with
		// ARCADIA_PYTHON enabled. Gates library/python/symbols/* PEERDIRs
		// in contrib/libs/python/ya.make and the contrib/tools/python3
		// PEERDIR + Include ADDINCL in stage0pycc / tools/py3cc/bin
		// (each takes ELSE when true). Verified via REF stage0pycc
		// `-I$(S)/contrib/tools/python3/Include` and 14 symbols/* nodes.
		"USE_ARCADIA_PYTHON":                true,
		"PYTHON2":                           false,
		"PYTHON3":                           true,
		"USE_PYTHON3_PREV":                  false, // M3: use previous Python3 toolchain.
		"PREBUILT":                          false, // M3: use prebuilt tools (tools/py3cc, rescompiler, etc.).
		"PY_PROTOS_FOR":                     false, // M3: PROTO_LIBRARY PY_PROTOS_FOR flag; false = no Python proto.
		"YMAKE_DEBUG":                       false, // M3: devtools/ymake/diag ymake-debug mode.
		"USE_VANILLA_PROTOC":                false, // M3: protobuf runtime selector.
		"USE_PREBUILT_TOOLS":                false, // M3: tools/py3cc prebuilt path.
		"PYTHON_SQLITE3":                    false, // M3: tools/py3cc/slow sqlite3 variant.
		"USE_SYSTEM_OPENSSL":                false, // M3: contrib/libs/openssl system variant.
		"OPENSOURCE_REPLACE_OPENSSL":        false, // M3: contrib/libs/openssl export replacement.
		"ARCADIA_ICONV_NOCJK":               false, // M3: libiconv full table variant; false keeps CJK tables enabled.
		"PYBUILD_NO_PYC":                    false, // M3: Python build variant.
		"USE_LIGHT_PY2CC":                   false, // M3: Python 2 build variant.
		"PYBIND_SRC":                        false, // M3: pybind source variant.
		"PYTHON_FORBIDDEN_PROTOBUFS":        false, // M3: proto restrictions.
		"SANITIZER_ADDRESS_USE_AFTER_SCOPE": false, // M3: sanitizer variant.
		"ASAN":                              false, // M3: AddressSanitizer build.
		"TSAN":                              false, // M3: ThreadSanitizer build.
		"MSAN":                              false, // M3: MemorySanitizer build.
		"UBSAN":                             false, // M3: UndefinedBehaviorSanitizer build.
		"LSAN":                              false, // M3: LeakSanitizer build.
		"HAVE_OPENSSL":                      false, // M3: OpenSSL availability.
		"NO_OPENSSL":                        false, // M3: OpenSSL suppression flag.
		"YT_DISABLE_REF_COUNTED_TRACKING":   false, // M3: optional YT memory tracking flag.
		"YT_ENRICH_PROMISE_ABANDONED_WITH_BACKTRACE": false, // M3: optional YT diagnostics flag.
		"YT_CUSTOM_INTERNAL_BUILD":                   false, // M3: internal YT build flag.
		"YT_ROPSAN_ENABLE_ACCESS_CHECK":              false, // M3: optional YT ropsan flag.
		"YT_ROPSAN_ENABLE_SERIALIZATION_CHECK":       false, // M3: optional YT ropsan flag.
		"YT_ROPSAN_ENABLE_LEAK_DETECTION":            false, // M3: optional YT ropsan flag.
		"YT_ROPSAN_ENABLE_PTR_TAGGING":               false, // M3: optional YT ropsan flag.
		"DARWIN_ARM64":                               false, // M3: macOS ARM64 arch flag; false on Linux.
		"DARWIN_X86_64":                              false, // M3: macOS x86_64 arch flag; false on Linux.
		"OS_HAIKU":                                   false, // M3: Haiku OS; false on Linux.
		"OS_NETBSD":                                  false, // M3: NetBSD; false on Linux.
		"OS_OPENBSD":                                 false, // M3: OpenBSD; false on Linux.
		"OS_VXWORKS":                                 false, // M3: VxWorks RTOS; false on Linux.
		"OS_ZOS":                                     false, // M3: z/OS; false on Linux.
		"CPU_ARM":                                    false, // M3: generic ARM flag (not aarch64).
		"CPU_X86":                                    false, // M3: generic x86 flag (not x86_64).
		"NO_CPU_CHECK":                               false, // M3: CPU capability check suppression.
		"HAVE_POSIX_MEMALIGN":                        false, // M3: POSIX memalign availability.
		"HAVE_MREMAP":                                false, // M3: mremap syscall availability.
		"NO_UTIL":                                    false, // M3: util/generic suppression flag (also whitelist).
		"TCLANG":                                     false, // M3: ThinLTO clang variant.
		"CLANG_VER":                                  false, // M3: Clang version flag (bool use in some IFs).
		// M3 additional platform/arch booleans — all false on standard linux-aarch64 build.
		"ANDROID_ARMV7":                      false, // M3: Android ARMv7 target.
		"ANDROID_I686":                       false, // M3: Android i686 target.
		"ARCADIA_OPENSSL_DISABLE_ARMV7_TICK": false, // M3: OpenSSL armv7 tick disable.
		// ARCADIA_PCRE_ENABLE_JIT: intentionally NOT pre-bound.
		// contrib/libs/pcre/ya.make does DEFAULT(ARCADIA_PCRE_ENABLE_JIT yes)
		// then IF(ARCADIA_PCRE_ENABLE_JIT) for -DARCADIA_PCRE_ENABLE_JIT;
		// the DEFAULT→IF env-bridge in collectStmts establishes the
		// binding at DEFAULT time so the IF observes it. Pre-binding
		// would force HasBinding=true and DEFAULT's "skip if set" no-ops.
		"ARCH_I686":                          false, // M3: i686 32-bit x86 target.
		"ARCH_PPC64LE":                       false, // M3: PowerPC 64-bit LE target.
		"ARCH_TYPE_32":                       false, // M3: 32-bit architecture flag.
		"DISABLE_INSTRUCTION_SETS":           false, // M3: instruction-set disablement flag.
		"DONT_LINK_LEGACY_ZSTD06_BLOCKCODEC": false, // M3: zstd 0.6 blockcodec linkage flag.
		"IOS_ARMV7":                          false, // M3: iOS ARMv7 target.
		"IOS_I386":                           false, // M3: iOS i386 simulator target.
		"LINUX_ARMV7":                        false, // M3: Linux ARMv7 target.
		"MAPSMOBI_BUILD_TARGET":              false, // M3: MobileYandexMaps build target flag.
		"OPENSOURCE_REPLACE_PROTOBUF":        false, // M3: protobuf export replacement flag.
		"OS_IOSSIM":                          false, // M3: iOS simulator.
		"OS_NONE":                            false, // M3: no OS (bare metal / embedded).
		"OS_SDK":                             false, // M3: OS SDK flag.
		"USE_LTO":                            false, // M3: link-time optimization flag.
		"USE_SYSTEM_PYTHON":                  false, // M3: use system Python (not Arcadia bundle).
		"WINDOWS_I686":                       false, // M3: Windows i686 target.
	},
	strings: map[string]string{
		// CXX_RT: SET-derived runtime selector. linux + clang + non-Android
		// + non-ARM6/7 → libcxxrt. Wired directly because SET is not yet
		// evaluated.
		"CXX_RT": "libcxxrt",
		// OPENSOURCE_PROJECT: project selector used in some library
		// ya.makes (e.g. library/cpp/svnversion: yt-cpp-sdk branch adds
		// a PEERDIR that is absent in the standard Arcadia build). M3+
		// closure sees this identifier; empty string = standard build.
		"OPENSOURCE_PROJECT": "",
		// SANITIZER_TYPE: empty in unsanitized M2; comparisons against
		// the bare-ident sanitizer-type names below evaluate to false.
		"SANITIZER_TYPE": "",
		// Bare-ident sanitizer type literals — each maps to its own
		// name so `SANITIZER_TYPE == undefined` compares against the
		// literal "undefined" via string equality.
		"undefined": "undefined",
		"memory":    "memory",
		"address":   "address",
		"thread":    "thread",
		"leak":      "leak",
		// MODULE_TAG = "PY3" for our closure. contrib/libs/python/ya.make:32
		// gates on IF(MODULE_TAG == "PY2"); USE_PYTHON3 takes the ELSE arm,
		// which requires MODULE_TAG != "PY2". Mirrors `module PY3_LIBRARY`'s
		// SET(MODULE_LANG PY3) in build/conf/python.conf.
		"MODULE_TAG": "PY3",
		"PY2":        "PY2", // bare-ident literal used in `IF (MODULE_TAG == "PY2")`.
	},
	ints: map[string]int{
		// ANDROID_API: defensive default for libc_compat's `<`
		// branches; not reached on M2 (OS_ANDROID=false above).
		"ANDROID_API": 0,
	},
}
