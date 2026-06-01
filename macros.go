package main

import (
	"strconv"
	"strings"
)

// ENV interns IF/macro variable names into a small, dense id space (the conf's
// fixed-ish flag set), separate from STR — analogous to STR/VFS. It lets
// Environment store bindings in ENV-indexed slices instead of three string-keyed
// maps, so Clone is a slice copy and Set/Get are array ops (the macro maps were
// ~8% of map-probing CPU plus per-module Clone churn). Single-writer (gen
// goroutine), like internTable.
type ENV uint32

var envTable = struct {
	ids   map[string]ENV
	names []string
}{ids: make(map[string]ENV, 256), names: []string{""}} // index 0 reserved = "not interned"

func internEnv(name string) ENV {
	if id, ok := envTable.ids[name]; ok {
		return id
	}

	id := ENV(len(envTable.names))
	envTable.ids[name] = id
	envTable.names = append(envTable.names, name)

	return id
}

// identEnv returns the ENV for an IF identifier, preferring the id interned into
// the node at parse time; a zero Env (e.g. an ExprIdent built outside the parser,
// as in tests) falls back to interning the name on demand.
func identEnv(x *ExprIdent) ENV {
	if x.Env != 0 {
		return x.Env
	}

	return internEnv(x.Name)
}

type envKind uint8

const (
	envAbsent envKind = iota
	envStr            // string binding; bools fold to "yes"/"no" (evalEq normalizes them)
	envInt            // integer binding, stored as its decimal STR
)

// envStore holds the ENV-indexed bindings behind a pointer so copies of an
// Environment (a value type) share — and can grow — the same state, exactly as
// the old shared maps did; Clone makes a fresh store.
type envStore struct {
	val  []STR
	kind []envKind
}

type Environment struct{ s *envStore }

func newEnvironment() Environment { return Environment{s: &envStore{}} }

func (s *envStore) ensure(e ENV) {
	if int(e) < len(s.kind) {
		return
	}

	n := len(s.kind) * 2

	if n <= int(e) {
		n = int(e) + 1
	}

	nk := make([]envKind, n)
	copy(nk, s.kind)
	s.kind = nk

	nv := make([]STR, n)
	copy(nv, s.val)
	s.val = nv
}

func (s *envStore) lookup(e ENV) (envKind, STR) {
	if int(e) < len(s.kind) {
		return s.kind[e], s.val[e]
	}

	return envAbsent, 0
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

// Bool reads name as a boolean IF-flag. The IF flag namespace is unified
// and lazy: an unset name is indistinguishable from an explicit false
// (mirroring upstream ymake — $X EvalValue returns "" for unbound vars,
// and NYMake::IsTrue treats empty / any falseWord as false). The only
// typed error is an int binding used in boolean position.
func (e Environment) Bool(name string) bool {
	return e.boolID(internEnv(name), name)
}

// boolID is Bool keyed by a pre-interned ENV (the IF-eval hot path passes the id
// parsed into ExprIdent; name is only for the int-misuse error message).
func (e Environment) boolID(id ENV, name string) bool {
	switch k, v := e.s.lookup(id); k {
	case envStr:
		return stringIsTruthy(internTable.strs[v])
	case envInt:
		ThrowFmt("macros: identifier %q has int binding but is used in boolean position", name)
	}

	return false
}

// stringIsTruthy mirrors upstream's NYMake::IsTrue + util/string/type
// IsFalse: empty or any case-insensitive false-word reads as false; every
// other non-empty value reads as true.
func stringIsTruthy(v string) bool {
	if v == "" {
		return false
	}

	switch strings.ToLower(v) {
	case "false", "f", "no", "n", "off", "0", "net":
		return false
	}

	return true
}

func (e Environment) String(name string) string {
	// envStr stores the string (bools as "yes"/"no"); envInt stores the decimal
	// form — both round-trip via the value STR.
	if k, v := e.s.lookup(internEnv(name)); k != envAbsent {
		return internTable.strs[v]
	}

	if isImplicitBuildVar(name) {
		return ""
	}

	ThrowFmt("macros: unknown IF identifier %q", name)

	return ""
}

func (e Environment) Clone() Environment {
	return Environment{s: &envStore{
		val:  append([]STR(nil), e.s.val...),
		kind: append([]envKind(nil), e.s.kind...),
	}}
}

// setStr binds name to a string value (the unified bool/string slot). SetBool
// folds to "yes"/"no" — observably identical to the old bool binding, since
// Bool reads via stringIsTruthy and String/evalEq map bool↔"yes"/"no".
func (e Environment) setStr(name, v string) {
	id := internEnv(name)
	e.s.ensure(id)
	e.s.kind[id] = envStr
	e.s.val[id] = internString(v)
}

func (e Environment) setInt(name string, n int) {
	id := internEnv(name)
	e.s.ensure(id)
	e.s.kind[id] = envInt
	e.s.val[id] = internString(strconv.Itoa(n))
}

func (e Environment) SetBool(name string, v bool) {
	if v {
		e.setStr(name, "yes")
	} else {
		e.setStr(name, "no")
	}
}

func (e Environment) SetString(name, v string) {
	e.setStr(name, v)
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

func (e Environment) HasBinding(name string) bool {
	k, _ := e.s.lookup(internEnv(name))

	return k != envAbsent
}

func (e Environment) SetDefaultString(name, value string) {
	if e.HasBinding(name) {
		return
	}

	e.setStr(name, value)
}

func EvalCond(e Expr, env Environment) bool {
	switch x := e.(type) {
	case *ExprIdent:
		if x.Name == "yes" {
			return true
		}

		if x.Name == "no" {
			return false
		}

		return env.boolID(identEnv(x), x.Name)
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

	return false
}

func evalAtom(e Expr, env Environment) any {
	switch x := e.(type) {
	case *ExprIdent:
		if x.Name == "yes" || x.Name == "no" {
			return x.Name
		}

		switch k, v := env.s.lookup(identEnv(x)); k {
		case envStr:
			return internTable.strs[v]
		case envInt:
			n, _ := strconv.Atoi(internTable.strs[v])

			return n
		}

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

	return nil
}

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

	return false
}

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

var DefaultIfEnv = makeDefaultIfEnv()

func makeDefaultIfEnv() Environment {
	e := newEnvironment()

	for _, n := range []string{
		"OS_LINUX", "LINUX",
		"CLANG", "TRUE", "USE_SSE4",
		"OPENSOURCE",
		"USE_ARCADIA_PYTHON", "PYTHON3",
	} {
		e.SetBool(n, true)
	}

	e.SetString("CXX_RT", "libcxxrt")
	e.SetString("OPENSOURCE_PROJECT", "")
	e.SetString("SANITIZER_TYPE", "")
	e.SetString("undefined", "undefined")
	e.SetString("memory", "memory")
	e.SetString("address", "address")
	e.SetString("thread", "thread")
	e.SetString("leak", "leak")
	e.SetString("MODULE_TAG", "PY3")
	e.SetString("_USE_ICONV", "dynamic")
	e.SetString("ALLOCATOR", "")
	e.SetString("PY2", "PY2")
	e.SetString("OS_SDK", "")

	e.setInt("ANDROID_API", 0)

	return e
}
