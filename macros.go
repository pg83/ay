package main

// macros.go — IF-predicate evaluator and the bound-variable environment
// the M2 macro routing pipeline uses to decide which branches a parsed
// `*IfStmt` keeps. The Expr ADT is in `yamake.go` next to the parser
// that builds it; the evaluator and the canonical env live here so
// later PRs (PR-20 wiring, PR-22 archiver-platform sweep) extend just
// `DefaultIfEnv` rather than touching the parser.
//
// The contract per D27 is intentionally strict: an unknown identifier
// in an IF expression throws. Silent default-to-false is the failure
// mode that hides "we never bound this var" until the comparator
// reports a missing-node, which is one round too late.

// EvalCond evaluates an IF predicate against a fixed env. Throws
// (D27) on:
//
//   - unknown identifier — the canonical env (DefaultIfEnv) must be
//     extended with the new var rather than letting the call silently
//     return false;
//   - unhandled Expr type — defensive guard for future ADT widenings
//     that forget to extend the switch.
func EvalCond(e Expr, env map[string]bool) bool {
	switch x := e.(type) {
	case *ExprIdent:
		v, ok := env[x.Name]

		if !ok {
			ThrowFmt("macros: unknown IF identifier %q (extend env in macros.go's DefaultIfEnv)", x.Name)
		}

		return v
	case *ExprNot:
		return !EvalCond(x.Of, env)
	case *ExprAnd:
		return EvalCond(x.Left, env) && EvalCond(x.Right, env)
	case *ExprOr:
		return EvalCond(x.Left, env) || EvalCond(x.Right, env)
	}

	ThrowFmt("macros: unhandled Expr type %T", e)

	return false // unreachable; ThrowFmt panics
}

// DefaultIfEnv is the bound-variable environment matching the
// reference graph (M2 target = `default-linux-aarch64` + clang +
// musl). Extending this set is the documented way to teach EvalCond
// about a new identifier; PR-20 will trip the unknown-identifier
// throw exactly when a real ya.make in the archiver closure
// references something we have not bound, at which point the env
// gets a new entry — not the evaluator a new fallback.
var DefaultIfEnv = map[string]bool{
	"OS_LINUX":                          true,
	"OS_WINDOWS":                        false,
	"OS_DARWIN":                         false,
	"OS_IOS":                            false,
	"OS_ANDROID":                        false,
	"OS_EMSCRIPTEN":                     false,
	"ARCH_AARCH64":                      true,
	"ARCH_X86_64":                       false,
	"ARCH_I386":                         false,
	"ARCH_ARM7":                         false,
	"ARCH_ARM64":                        false,
	"CLANG":                             true,
	"CLANG_CL":                          false,
	"GCC":                               false,
	"MSVC":                              false,
	"MUSL":                              true,
	"WITH_VALGRIND":                     false,
	"TSTRING_IS_STD_STRING":             false,
	"NO_CUSTOM_CHAR_PTR_STD_COMPARATOR": false,
	"NEED_CHECK":                        false,
	"TRUE":                              true,
	"FALSE":                             false,
}
