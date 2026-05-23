package main

import "fmt"

// macros.go — IF-predicate evaluator and DefaultIfEnv. Expr ADT lives
// next to the parser in yamake.go.
//
// Contract: an unknown ALL_CAPS build variable defaults to empty/false,
// matching ymake's practical "unset variable" behaviour for optional
// feature toggles. Non-build-var names still throw so parser/evaluator
// defects stay visible. Value space is {bool, string, int}; each binding
// lives in exactly one of the three typed maps.

// Environment binds IF-condition identifiers to their typed values.
// Three disjoint maps so comparators (ExprEq, ExprLt) resolve operands of
// differing types. Boolean-only callers (ExprIdent in predicate position,
// ExprAnd/Or/Not) go through Bool, which throws on a non-bool binding.
type Environment struct {
	bools   map[string]bool
	strings map[string]string
	ints    map[string]int
}

func isImplicitBuildVar(name string) bool {
	if name == "" {
		return false
	}

	hasUpper := false
	for i := 0; i < len(name); i++ {
		b := name[i]
		switch {
		case b >= 'A' && b <= 'Z':
			hasUpper = true
		case b >= '0' && b <= '9':
		case b == '_':
		default:
			return false
		}
	}

	return hasUpper
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

	if isImplicitBuildVar(name) {
		return ""
	}

	ThrowFmt("macros: unknown IF identifier %q", name)

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

	if isImplicitBuildVar(name) {
		return false
	}

	ThrowFmt("macros: unknown IF identifier %q", name)

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

	if isImplicitBuildVar(name) {
		return ""
	}

	ThrowFmt("macros: unknown IF identifier %q", name)

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

		if v, ok := env.bools[x.Name]; ok {
			return v
		}

		if v, ok := env.strings[x.Name]; ok {
			return v
		}

		if v, ok := env.ints[x.Name]; ok {
			return v
		}

		// Unbound implicit build vars (all-uppercase names) act as string
		// literals equal to their own name: IF (ARCADIA_CURL_DNS_RESOLVER == ARES)
		// evaluates correctly when ARCADIA_CURL_DNS_RESOLVER is "ARES".
		if isImplicitBuildVar(x.Name) {
			return x.Name
		}

		ThrowFmt("macros: unknown IF identifier %q", x.Name)

		return nil
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
		if rv, ok := r.(bool); ok {
			if rv {
				return lv == "yes"
			}

			return lv == "no"
		}
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
		if rv, ok := r.(string); ok {
			if lv {
				return "yes" == rv
			}

			return "no" == rv
		}
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

// DefaultIfEnv is the bound-variable environment for IF predicates —
// per-build bindings independent of instance.Platform.ISA. Every ARCH_*
// defaults to false; buildIfEnv (modules.go) flips the matching ISA's
// bits per instance. Other shape (OS_LINUX, CLANG, …) reflects the
// reference closure's build configuration (linux + clang); MUSL is
// supplied per-platform via flags and defaults to "no" when absent.
// Extend this set to teach EvalCond about a new identifier.
var DefaultIfEnv = Environment{
	bools: map[string]bool{
		"OS_LINUX":      true,
		"LINUX":         true, // legacy alias for OS_LINUX, used in some ya.make files
		"OS_WINDOWS":    false,
		"OS_DARWIN":     false,
		"OS_IOS":        false,
		"OS_ANDROID":    false,
		"OS_EMSCRIPTEN": false,
		"OS_FREEBSD":    false,
		"OS_CYGWIN":     false,
		"SUN":           false,
		"CYGWIN":        false,
		// ARCH_ARM64 is the upstream alias for ARCH_AARCH64; buildIfEnv
		// flips them in lockstep so IF predicates see consistent bindings.
		"ARCH_AARCH64":                      false,
		"ARCH_X86_64":                       false,
		"ARCH_I386":                         false,
		"ARCH_ARM7":                         false,
		"ARCH_ARM64":                        false,
		"ARCH_ARM6":                         false,
		"ARCH_WASM32":                       false,
		"ARCH_WASM64":                       false,
		"CLANG":                             true,
		"CLANG_CL":                          false,
		"GCC":                               false,
		"MSVC":                              false,
		"MUSL":                              false,
		"USE_EAT_MY_DATA":                   false,
		"WITH_MAPKIT":                       false,
		"WITH_VALGRIND":                     false,
		"TSTRING_IS_STD_STRING":             false,
		"NO_CUSTOM_CHAR_PTR_STD_COMPARATOR": false,
		"NEED_CHECK":                        false,
		"TRUE":                              true,
		"FALSE":                             false,
		"FUZZING":                           false,
		"EXPORT_CMAKE":                      false,
		"NO_CXX_RTTI":                       false,
		"NO_CXX_EXCEPTIONS":                 false,
		"USE_ARCADIA_COMPILER_RUNTIME":      false,
		"PROVIDE_REALLOCARRAY":              false,
		"PROVIDE_GETRANDOM_GETENTROPY":      false,
		"PROVIDE_QUEUE":                     false,
		"PROVIDE_GETSERVBYNAME":             false,
		"PROVIDE_MEMFD_CREATE":              false,
		"DLL_FOR":                           false,
		"DYNAMIC_BOOST":                     false,
		"PROFILE_MEMORY_ALLOCATIONS":        false,
		"USE_SSE4":                          true,
		// MUSL_LITE=false → defaultProgramPeerdirsForModule picks contrib/libs/musl/full
		// when MUSL=yes && !MUSL_LITE.
		"MUSL_LITE":                        false,
		"OPENSOURCE_REPLACE_LINUX_HEADERS": false,
		// OPENSOURCE=true: source tree is the open-source Arcadia export.
		// IF(NOT OPENSOURCE) branches that PEERDIR internal-only modules
		// (e.g. library/cpp/xml/document) must take the false arm.
		"OPENSOURCE":        true,
		"YA_OPENSOURCE":     false,
		"EXTERNAL_PY_FILES": false,
		// USE_ARCADIA_PYTHON=true gates library/python/symbols/* PEERDIRs
		// in contrib/libs/python/ya.make and the contrib/tools/python3
		// PEERDIR + Include ADDINCL in stage0pycc / tools/py3cc/bin
		// (each takes the ELSE arm when true).
		"USE_ARCADIA_PYTHON":                true,
		"PYTHON2":                           false,
		"PYTHON3":                           true,
		"USE_PYTHON3_PREV":                  false,
		"PREBUILT":                          false,
		"PY_PROTOS_FOR":                     false,
		"YMAKE_DEBUG":                       false,
		"USE_VANILLA_PROTOC":                false,
		"USE_PREBUILT_TOOLS":                false,
		"PYTHON_SQLITE3":                    false,
		"USE_SYSTEM_OPENSSL":                false,
		"OPENSOURCE_REPLACE_OPENSSL":        false,
		"ARCADIA_ICONV_NOCJK":               false,
		"PYBUILD_NO_PYC":                    false,
		"USE_LIGHT_PY2CC":                   false,
		"PYBIND_SRC":                        false,
		"PYTHON_FORBIDDEN_PROTOBUFS":        false,
		"SANITIZER_ADDRESS_USE_AFTER_SCOPE": false,
		"ASAN":                              false,
		"TSAN":                              false,
		"MSAN":                              false,
		"UBSAN":                             false,
		"LSAN":                              false,
		"HAVE_OPENSSL":                      false,
		"NO_OPENSSL":                        false,
		"YT_DISABLE_REF_COUNTED_TRACKING":   false,
		"YT_ENRICH_PROMISE_ABANDONED_WITH_BACKTRACE": false,
		"YT_CUSTOM_INTERNAL_BUILD":                   false,
		"YT_ROPSAN_ENABLE_ACCESS_CHECK":              false,
		"YT_ROPSAN_ENABLE_SERIALIZATION_CHECK":       false,
		"YT_ROPSAN_ENABLE_LEAK_DETECTION":            false,
		"YT_ROPSAN_ENABLE_PTR_TAGGING":               false,
		"DARWIN_ARM64":                               false,
		"DARWIN_X86_64":                              false,
		"OS_HAIKU":                                   false,
		"OS_NETBSD":                                  false,
		"OS_OPENBSD":                                 false,
		"OS_VXWORKS":                                 false,
		"OS_ZOS":                                     false,
		"CPU_ARM":                                    false,
		"CPU_X86":                                    false,
		"NO_CPU_CHECK":                               false,
		"HAVE_POSIX_MEMALIGN":                        false,
		"HAVE_MREMAP":                                false,
		"NO_UTIL":                                    false,
		"TCLANG":                                     false,
		"CLANG_VER":                                  false,
		"ANDROID_ARMV7":                              false,
		"ANDROID_I686":                               false,
		"ARCADIA_OPENSSL_DISABLE_ARMV7_TICK":         false,
		// ARCADIA_PCRE_ENABLE_JIT is intentionally NOT pre-bound.
		// contrib/libs/pcre/ya.make does DEFAULT(ARCADIA_PCRE_ENABLE_JIT yes)
		// then IF(ARCADIA_PCRE_ENABLE_JIT); the DEFAULT→IF env-bridge in
		// collectStmts establishes the binding at DEFAULT time. Pre-binding
		// would force HasBinding=true and DEFAULT no-ops.
		"ARCH_I686":                          false,
		"ARCH_PPC64LE":                       false,
		"ARCH_TYPE_32":                       false,
		"DISABLE_INSTRUCTION_SETS":           false,
		"DONT_LINK_LEGACY_ZSTD06_BLOCKCODEC": false,
		"IOS_ARMV7":                          false,
		"IOS_I386":                           false,
		"LINUX_ARMV7":                        false,
		"MAPSMOBI_BUILD_TARGET":              false,
		"OPENSOURCE_REPLACE_PROTOBUF":        false,
		"OS_IOSSIM":                          false,
		"OS_NONE":                            false,
		"OS_FREERTOS":                        false,
		"STATIC_STL":                         false,
		"USE_LTO":                            false,
		"USE_SYSTEM_PYTHON":                  false,
		"WINDOWS_I686":                       false,
	},
	strings: map[string]string{
		// CXX_RT is SET-derived (linux + clang + non-Android + non-ARM6/7
		// → libcxxrt); wired directly because SET is not evaluated.
		"CXX_RT": "libcxxrt",
		// OPENSOURCE_PROJECT selects per-project ya.make branches (e.g.
		// library/cpp/svnversion yt-cpp-sdk variant); empty = standard build.
		"OPENSOURCE_PROJECT": "",
		// SANITIZER_TYPE="" means unsanitized; equality against the
		// bare-ident sanitizer literals below evaluates to false.
		"SANITIZER_TYPE": "",
		// Bare-ident sanitizer type literals — each maps to its own name
		// so `SANITIZER_TYPE == undefined` compares string-to-string.
		"undefined": "undefined",
		"memory":    "memory",
		"address":   "address",
		"thread":    "thread",
		"leak":      "leak",
		// MODULE_TAG=PY3 mirrors `module PY3_LIBRARY`'s SET(MODULE_LANG PY3)
		// in build/conf/python.conf. contrib/libs/python/ya.make gates
		// IF(MODULE_TAG == "PY2") and takes the ELSE arm here.
		"MODULE_TAG": "PY3",
		// _USE_ICONV=dynamic mirrors build/conf/opensource.conf under
		// OPENSOURCE=yes; contrib/libs/libiconv/ya.make consumes it via
		// DEFAULT(USE_ICONV ${_USE_ICONV}).
		"_USE_ICONV": "dynamic",
		"ALLOCATOR":  "",
		"PY2":        "PY2", // bare-ident literal for IF (MODULE_TAG == "PY2").
		"OS_SDK":     "",
	},
	ints: map[string]int{
		// ANDROID_API: defensive default for libc_compat `<` branches;
		// not reached because OS_ANDROID=false.
		"ANDROID_API": 0,
	},
}
