package main

import (
	"strconv"
	"strings"
)

var (
	// strYes/strNo are the pre-interned STR forms of the bool foldings, reused by
	// SetBool instead of re-interning "yes"/"no" on every per-module binding.
	strYes       = internStr("yes")
	strNo        = internStr("no")
	DefaultIfEnv = makeDefaultIfEnv()
)

var envTable = struct {
	ids   map[string]ENV
	names []string
}{ids: make(map[string]ENV, 256), names: []string{""}} // index 0 reserved = "not interned"

// intSTR holds the pre-interned decimal STR of the first len(intSTR) integers so
// SetInt avoids strconv.Itoa + internStr on the common small-int path.
var intSTR = func() [1024]STR {
	var a [1024]STR

	for i := range a {
		a[i] = internStr(strconv.Itoa(i))
	}

	return a
}()

// ENV interns IF/macro variable names into a small, dense id space (the conf's
// fixed-ish flag set), separate from STR — analogous to STR/VFS. It lets
// Environment store bindings in ENV-indexed slices instead of three string-keyed
// maps, so Clone is a slice copy and Set/Get are array ops (the macro maps were
// ~8% of map-probing CPU plus per-module Clone churn). Single-writer (gen
// goroutine), like internTable.
type ENV uint32

func internEnv(name string) ENV {
	if id, ok := envTable.ids[name]; ok {
		return id
	}

	id := ENV(len(envTable.names))
	envTable.ids[name] = id
	envTable.names = append(envTable.names, name)

	return id
}

func (id ENV) String() string {
	return envTable.names[id]
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

func newEnvironment() Environment {
	return Environment{s: &envStore{}}
}

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
func (e Environment) Bool(id ENV) bool {
	return e.boolID(id, id.String())
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

func (e Environment) String(id ENV) string {
	// envStr stores the string (bools as "yes"/"no"); envInt stores the decimal
	// form — both round-trip via the value STR.
	if k, v := e.s.lookup(id); k != envAbsent {
		return internTable.strs[v]
	}

	name := id.String()

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

// setStrID binds id to the pre-interned string value v (the unified bool/string
// slot). The whole env API is keyed by ENV and takes pre-interned STR values so
// no name or constant value is re-interned per binding (per-module buildIfEnv
// ran this thousands of times). SetBool folds to strYes/strNo — observably
// identical to the old bool binding, since Bool reads via stringIsTruthy and
// String/evalEq map bool↔"yes"/"no".
func (e Environment) setStrID(id ENV, v STR) {
	e.s.ensure(id)
	e.s.kind[id] = envStr
	e.s.val[id] = v
}

// SetStringID binds a pre-interned constant value (hoisted STR var); SetString
// is for computed values that must intern at the call.
func (e Environment) SetStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

func (e Environment) SetString(id ENV, v string) {
	e.setStrID(id, internStr(v))
}

func (e Environment) SetInt(id ENV, n int) {
	e.s.ensure(id)
	e.s.kind[id] = envInt

	if uint(n) < uint(len(intSTR)) {
		e.s.val[id] = intSTR[n]

		return
	}

	e.s.val[id] = internStr(strconv.Itoa(n))
}

func (e Environment) SetBool(id ENV, v bool) {
	if v {
		e.setStrID(id, strYes)
	} else {
		e.setStrID(id, strNo)
	}
}

func (e Environment) SetFromString(id ENV, v string) {
	switch v {
	case "yes":
		e.SetBool(id, true)
	case "no":
		e.SetBool(id, false)
	default:
		e.SetString(id, v)
	}
}

// SetFromStringID is SetFromString for an already-interned value (Platform.Flags
// iteration): no string compare, just map the STR to the stored slot, folding the
// yes/no STRs to themselves (they already are strYes/strNo).
func (e Environment) SetFromStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

// internedEnv looks up name's ENV without interning it (envTable read-only,
// like interned() for STR). Var-expansion probes arbitrary ${VAR} tokens that
// are usually not env vars; interning to check would grow the ENV table with
// junk, so presence is tested via this lookup first.
func internedEnv(name string) (ENV, bool) {
	id, ok := envTable.ids[name]

	return id, ok
}

func (e Environment) hasBindingID(id ENV) bool {
	k, _ := e.s.lookup(id)

	return k != envAbsent
}

// HasBinding reports whether name is bound, taking a string: it first checks
// whether name is interned at all, so a ${VAR} token that is not a known env
// var is reported unbound without being interned.
func (e Environment) HasBinding(name string) bool {
	id, ok := internedEnv(name)

	return ok && e.hasBindingID(id)
}

// Lookup returns name's bound value and whether it is bound, without interning
// name (same non-polluting rationale as HasBinding).
func (e Environment) Lookup(name string) (string, bool) {
	id, ok := internedEnv(name)

	if !ok {
		return "", false
	}

	k, v := e.s.lookup(id)

	if k == envAbsent {
		return "", false
	}

	return internTable.strs[v], true
}

func (e Environment) SetDefaultString(id ENV, v string) {
	if e.hasBindingID(id) {
		return
	}

	e.SetString(id, v)
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

func makeDefaultIfEnv() Environment {
	e := newEnvironment()

	for _, n := range []ENV{
		envOS_LINUX, envLINUX,
		envCLANG, envTRUE, envUSE_SSE4,
		envOPENSOURCE,
		envUSE_ARCADIA_PYTHON, envPYTHON3,
	} {
		e.SetBool(n, true)
	}

	e.SetString(envCXX_RT, "libcxxrt")
	e.SetString(envOPENSOURCE_PROJECT, "")
	e.SetString(envSANITIZER_TYPE, "")
	e.SetString(envundefined, "undefined")
	e.SetString(envmemory, "memory")
	e.SetString(envaddress, "address")
	e.SetString(envthread, "thread")
	e.SetString(envleak, "leak")
	e.SetString(envMODULE_TAG, "PY3")
	e.SetString(env_USE_ICONV, "dynamic")
	e.SetString(envALLOCATOR, "")
	e.SetString(envPY2, "PY2")
	e.SetString(envOS_SDK, "")

	e.SetInt(envANDROID_API, 0)

	return e
}
