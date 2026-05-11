package main

// macros.go — IF-predicate evaluator and the bound-variable environment
// the M2 macro routing pipeline uses to decide which branches a parsed
// `*IfStmt` keeps. The Expr ADT is in `yamake.go` next to the parser
// that builds it; the evaluator and the canonical env live here so
// later PRs (PR-22 archiver-platform sweep) extend just `DefaultIfEnv`
// rather than touching the parser.
//
// The contract per D27 is intentionally strict: an unknown identifier
// in an IF expression throws. Silent default-to-false is the failure
// mode that hides "we never bound this var" until the comparator
// reports a missing-node, which is one round too late.
//
// PR-27 widened the value space from booleans to {bool, string, int}
// so the parser ADT additions (ExprEq, ExprLt, ExprString, ExprInt)
// have somewhere to look up their operands. The Environment carries
// three typed maps; a name appears in at most one (otherwise the
// Lookup fallthrough order is bool → string → int, but in practice
// each binding is only ever in one). The bool-only path stays the
// hot path — every existing IF predicate in the M1 closure is
// boolean-valued and only the new comparator paths reach for typed
// lookups.

// Environment binds IF-condition identifiers to their typed values.
// Three disjoint maps, one per supported value type. PR-27 introduced
// this in place of the pre-PR-27 `map[string]bool` so comparators
// (ExprEq, ExprLt) can resolve operands of differing types; the
// boolean-only callers (ExprIdent in predicate position, ExprAnd,
// ExprOr, ExprNot) still go through the typed `Bool` lookup which
// throws on a non-bool binding.
type Environment struct {
	bools   map[string]bool
	strings map[string]string
	ints    map[string]int
}

// Lookup returns the typed value bound to name, or throws on miss.
// The return type is `any` because the caller (evalAtom, ExprEq's
// rhs/lhs resolution) only branches on the dynamic type. Callers that
// need a specific type — EvalCond's ExprIdent path expects bool — go
// through Bool to get a typed value plus a clean error message.
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

// Bool returns the boolean binding for name. Bool-typed bindings are
// returned directly. String-typed bindings are coerced: empty string →
// false, non-empty string → true (upstream ymake semantics for bare-ident
// use of a string variable, e.g. `IF (SANITIZER_TYPE OR ...)` where
// SANITIZER_TYPE is "" when sanitizers are off). Int-typed bindings are
// not expected in bool position; that remains a defect.
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

// Clone returns a deep-enough copy of the env that callers can mutate
// per-instance (e.g. flip ARCH_AARCH64 ↔ ARCH_X86_64 for host targets)
// without trampling DefaultIfEnv. The maps are copied; their contents
// are immutable scalars.
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

// SetBool overrides (or adds) a boolean binding. Helper for
// per-instance env tweaks like ARCH_AARCH64 ↔ ARCH_X86_64; callers
// must Clone first if they don't want their mutation to leak into
// DefaultIfEnv.
func (e Environment) SetBool(name string, v bool) {
	e.bools[name] = v
}

// EvalCond evaluates an IF predicate against a fixed env. Throws
// (D27) on:
//
//   - unknown identifier — the canonical env (DefaultIfEnv) must be
//     extended with the new var rather than letting the call silently
//     return false;
//   - unhandled Expr type — defensive guard for future ADT widenings
//     that forget to extend the switch;
//   - bare ExprString / ExprInt in predicate position — a string or
//     int has no boolean meaning on its own, only as a comparator
//     operand;
//   - ExprEq / ExprLt with mismatched or non-numeric operand types.
func EvalCond(e Expr, env Environment) bool {
	switch x := e.(type) {
	case *ExprIdent:
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

// evalAtom resolves a value-position Expr (operand of `==` or `<`) to
// its dynamic value. ExprIdent goes through Lookup (any type); literal
// nodes return their carried value. Anything else — a bool combinator
// or another comparator nested directly as an operand — throws,
// because the parser's grammar should never produce such a shape.
func evalAtom(e Expr, env Environment) any {
	switch x := e.(type) {
	case *ExprIdent:
		return env.Lookup(x.Name)
	case *ExprString:
		return x.Value
	case *ExprInt:
		return x.Value
	}

	ThrowFmt("macros: unexpected Expr type %T in comparator operand position", e)

	return nil // unreachable
}

// evalEq compares two atoms for equality. Same-type comparison only;
// mixed string/int throws so a parser-level type confusion surfaces
// immediately rather than silently returning false.
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

// DefaultIfEnv is the bound-variable environment matching the
// reference graph (M2 target = `default-linux-aarch64` + clang +
// musl). Extending this set is the documented way to teach EvalCond
// about a new identifier; PR-27's wider closure (libcxx / libcxxrt /
// libunwind / util) added the typed entries below.
//
// PR-27 extension: when the walker reaches the libcxx and libc_compat
// branches of the closure it encounters comparator operands that the
// previous bool-only env could not represent. The new typed bindings
// are:
//
//   - CXX_RT="libcxxrt" — what the SET chain in libcxx/ya.make
//     resolves to for M2 (linux + non-Android + non-ARM6/7). Walker
//     does not yet evaluate SET, so the chosen value is wired
//     directly into the env so `IF (CXX_RT == "libcxxrt")` takes
//     the intended branch.
//   - SANITIZER_TYPE="" — sanitizers are off in M2; bare-ident
//     literals "undefined" / "memory" then compare against this empty
//     value and yield false.
//   - "undefined" / "memory" / "address" / "thread" / "leak" —
//     bare-ident sanitizer-type literals appearing on the RHS of
//     `IF (SANITIZER_TYPE == undefined)` style predicates. Each is
//     bound to a string equal to its own name so the comparison
//     evaluates as a string-literal compare against SANITIZER_TYPE.
//   - ANDROID_API=0 — defensive; libc_compat's `<` branches sit
//     inside `IF (OS_ANDROID)` (false on M2) so this binding is not
//     reached today, but pinning it makes the value explicit if a
//     future closure walks an OS_ANDROID-conditional module.
//   - bool-typed FUZZING / EXPORT_CMAKE / NO_CXX_RTTI /
//     NO_CXX_EXCEPTIONS / USE_ARCADIA_COMPILER_RUNTIME /
//     PROVIDE_* — flag-style booleans the wider closure uses;
//     all false in M2.
var DefaultIfEnv = Environment{
	bools: map[string]bool{
		"OS_LINUX":                          true,
		"OS_WINDOWS":                        false,
		"OS_DARWIN":                         false,
		"OS_IOS":                            false,
		"OS_ANDROID":                        false,
		"OS_EMSCRIPTEN":                     false,
		"OS_FREEBSD":                        false, // PR-27: contrib/libs/cxxsupp/libcxx
		"OS_CYGWIN":                         false, // PR-27: util
		"SUN":                               false, // PR-27: util
		"CYGWIN":                            false, // PR-27: util
		"ARCH_AARCH64":                      true,
		"ARCH_X86_64":                       false,
		"ARCH_I386":                         false,
		"ARCH_ARM7":                         false,
		// PR-35o: ARCH_ARM64 is the upstream alias for ARCH_AARCH64
		// (Arcadia sets both together for the aarch64 target). The
		// `contrib/libs/cxxsupp/builtins/ya.make` bf16 SRCS block is
		// guarded by `IF (ARCH_ARM64 OR ARCH_X86_64)`; without the
		// alias the 5 bf16 .c.o nodes (extendbfsf2, truncdfbf2,
		// truncsfbf2, trunctfbf2, truncxfbf2) are skipped on aarch64
		// and miss from the L0 closure. `composeHostCC`'s ARCH flip
		// (gen.go:buildIfEnv) flips ARCH_ARM64 alongside ARCH_AARCH64
		// so host-PIC walks see the consistent x86_64 binding.
		"ARCH_ARM64": true,
		"ARCH_ARM6":  false,
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
		// M3 new identifiers.
		// OPENSOURCE=true: this source tree is the open-source Arcadia export.
		// The reference sg2.json was built from this tree, so IF(NOT OPENSOURCE)
		// branches that PEERDIR internal-only modules (e.g. library/cpp/xml/document)
		// must be taken false. OPENSOURCE=false would include modules missing from
		// the tree and cause gen failures; the M2 target (tools/archiver) does not
		// reach any OPENSOURCE-gated code so flipping this to true is M2-safe.
		"OPENSOURCE":                    true, // M3: open-source Arcadia export (sg2.json reference).
		"YA_OPENSOURCE":                 false, // M3: ya-tool open-source build flag.
		"EXTERNAL_PY_FILES":             false, // M3: library/python/runtime_py3 external-py variant.
		"USE_ARCADIA_PYTHON":            false, // M3: use Arcadia Python; false = use Arcadia Python3 bundle.
		"USE_PYTHON3_PREV":              false, // M3: use previous Python3 toolchain.
		"PREBUILT":                      false, // M3: use prebuilt tools (tools/py3cc, rescompiler, etc.).
		"PY_PROTOS_FOR":                 false, // M3: PROTO_LIBRARY PY_PROTOS_FOR flag; false = no Python proto.
		"YMAKE_DEBUG":                   false, // M3: devtools/ymake/diag ymake-debug mode.
		"USE_VANILLA_PROTOC":            false, // M3: protobuf runtime selector.
		"USE_PREBUILT_TOOLS":            false, // M3: tools/py3cc prebuilt path.
		"PYTHON_SQLITE3":                false, // M3: tools/py3cc/slow sqlite3 variant.
		"USE_SYSTEM_OPENSSL":            false, // M3: contrib/libs/openssl system variant.
		"OPENSOURCE_REPLACE_OPENSSL":    false, // M3: contrib/libs/openssl export replacement.
		"PYBUILD_NO_PYC":                false, // M3: Python build variant.
		"USE_LIGHT_PY2CC":               false, // M3: Python 2 build variant.
		"PYBIND_SRC":                    false, // M3: pybind source variant.
		"PYTHON_FORBIDDEN_PROTOBUFS":    false, // M3: proto restrictions.
		"SANITIZER_ADDRESS_USE_AFTER_SCOPE": false, // M3: sanitizer variant.
		"ASAN":                          false, // M3: AddressSanitizer build.
		"TSAN":                          false, // M3: ThreadSanitizer build.
		"MSAN":                          false, // M3: MemorySanitizer build.
		"UBSAN":                         false, // M3: UndefinedBehaviorSanitizer build.
		"LSAN":                          false, // M3: LeakSanitizer build.
		"HAVE_OPENSSL":                  false, // M3: OpenSSL availability.
		"NO_OPENSSL":                    false, // M3: OpenSSL suppression flag.
		"DARWIN_ARM64":                  false, // M3: macOS ARM64 arch flag; false on Linux.
		"DARWIN_X86_64":                 false, // M3: macOS x86_64 arch flag; false on Linux.
		"OS_HAIKU":                      false, // M3: Haiku OS; false on Linux.
		"OS_NETBSD":                     false, // M3: NetBSD; false on Linux.
		"OS_OPENBSD":                    false, // M3: OpenBSD; false on Linux.
		"OS_VXWORKS":                    false, // M3: VxWorks RTOS; false on Linux.
		"OS_ZOS":                        false, // M3: z/OS; false on Linux.
		"CPU_ARM":                       false, // M3: generic ARM flag (not aarch64).
		"CPU_X86":                       false, // M3: generic x86 flag (not x86_64).
		"NO_CPU_CHECK":                  false, // M3: CPU capability check suppression.
		"HAVE_POSIX_MEMALIGN":           false, // M3: POSIX memalign availability.
		"HAVE_MREMAP":                   false, // M3: mremap syscall availability.
		"NO_UTIL":                       false, // M3: util/generic suppression flag (also whitelist).
		"TCLANG":                        false, // M3: ThinLTO clang variant.
		"CLANG_VER":                     false, // M3: Clang version flag (bool use in some IFs).
		// M3 additional platform/arch booleans — all false on standard linux-aarch64 build.
		"ANDROID_ARMV7":                         false, // M3: Android ARMv7 target.
		"ANDROID_I686":                           false, // M3: Android i686 target.
		"ARCADIA_OPENSSL_DISABLE_ARMV7_TICK":     false, // M3: OpenSSL armv7 tick disable.
		"ARCADIA_PCRE_ENABLE_JIT":                false, // M3: PCRE JIT enable flag.
		"ARCH_I686":                              false, // M3: i686 32-bit x86 target.
		"ARCH_PPC64LE":                           false, // M3: PowerPC 64-bit LE target.
		"ARCH_TYPE_32":                           false, // M3: 32-bit architecture flag.
		"DISABLE_INSTRUCTION_SETS":               false, // M3: instruction-set disablement flag.
		"DONT_LINK_LEGACY_ZSTD06_BLOCKCODEC":     false, // M3: zstd 0.6 blockcodec linkage flag.
		"IOS_ARMV7":                              false, // M3: iOS ARMv7 target.
		"IOS_I386":                               false, // M3: iOS i386 simulator target.
		"LINUX_ARMV7":                            false, // M3: Linux ARMv7 target.
		"MAPSMOBI_BUILD_TARGET":                  false, // M3: MobileYandexMaps build target flag.
		"OPENSOURCE_REPLACE_PROTOBUF":            false, // M3: protobuf export replacement flag.
		"OS_IOSSIM":                              false, // M3: iOS simulator.
		"OS_NONE":                                false, // M3: no OS (bare metal / embedded).
		"OS_SDK":                                 false, // M3: OS SDK flag.
		"USE_LTO":                                false, // M3: link-time optimization flag.
		"USE_SYSTEM_PYTHON":                      false, // M3: use system Python (not Arcadia bundle).
		"WINDOWS_I686":                           false, // M3: Windows i686 target.
	},
	strings: map[string]string{
		// CXX_RT: SET-derived runtime selector. M2 (linux + clang +
		// non-Android + non-ARM6/7) lands on libcxxrt. The walker
		// does not yet evaluate SET, so the chosen value is wired
		// directly here.
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
	},
	ints: map[string]int{
		// ANDROID_API: defensive default for libc_compat's `<`
		// branches; not reached on M2 (OS_ANDROID=false above).
		"ANDROID_API": 0,
	},
}
