package main

import (
	"strconv"
	"strings"
)

type EnvKind uint8

// EnvStore holds the ENV-indexed bindings behind a pointer so Environment copies
// share and can grow the same state; Clone makes a fresh store.
type EnvStore struct {
	val  []STR
	kind []EnvKind
}

type Environment struct{ s *EnvStore }

func newEnvironment() Environment {
	return Environment{s: &EnvStore{}}
}

func (s *EnvStore) ensure(e ENV) {
	if int(e) < len(s.kind) {
		return
	}

	n := len(s.kind) * 2

	if n <= int(e) {
		n = int(e) + 1
	}

	nk := make([]EnvKind, n)
	copy(nk, s.kind)
	s.kind = nk

	nv := make([]STR, n)
	copy(nv, s.val)
	s.val = nv
}

func (s *EnvStore) lookup(e ENV) (EnvKind, STR) {
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

// bool reads name as a boolean IF-flag; an unset name reads as false. An int
// binding in boolean position is the only typed error.
func (e Environment) bool(id ENV) bool {
	return e.boolID(id, "")
}

// boolID is bool keyed by a pre-interned ENV; name (only for the error message)
// is derived lazily.
func (e Environment) boolID(id ENV, name string) bool {
	switch k, v := e.s.lookup(id); k {
	case envStr:
		return strIsTruthy(v)
	case envInt:
		if name == "" {
			name = id.string()
		}

		throwFmt("macros: identifier %q has int binding but is used in boolean position", name)
	}

	return false
}

// strIsTruthy is stringIsTruthy in id space: the strYes/strNo fast path never
// takes a view.
func strIsTruthy(v STR) bool {
	switch v {
	case strYes:
		return true
	case strNo, 0:
		return false
	}

	return stringIsTruthy(v.string())
}

// stringIsTruthy: empty or any case-insensitive false-word reads as false.
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

func (e Environment) string(id ENV) string {
	// envStr and envInt both round-trip via the value STR.
	if k, v := e.s.lookup(id); k != envAbsent {
		return v.string()
	}

	name := id.string()

	if isImplicitBuildVar(name) {
		return ""
	}

	throwFmt("macros: unknown IF identifier %q", name)

	return ""
}

func (e Environment) clone() Environment {
	return Environment{s: &EnvStore{
		val:  append([]STR(nil), e.s.val...),
		kind: append([]EnvKind(nil), e.s.kind...),
	}}
}

// setStrID binds id to the pre-interned value v. Keying by ENV with pre-interned
// STR avoids re-interning per binding, which ran thousands of times per module.
func (e Environment) setStrID(id ENV, v STR) {
	e.s.ensure(id)
	e.s.kind[id] = envStr
	e.s.val[id] = v
}

// setStringID binds a pre-interned constant; setString interns at the call.
func (e Environment) setStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

func (e Environment) setString(id ENV, v string) {
	e.setStrID(id, internStr(v))
}

func (e Environment) setInt(id ENV, n int) {
	e.s.ensure(id)
	e.s.kind[id] = envInt

	if uint(n) < uint(len(intSTR)) {
		e.s.val[id] = intSTR[n]

		return
	}

	e.s.val[id] = internStr(strconv.Itoa(n))
}

func (e Environment) setBool(id ENV, v bool) {
	if v {
		e.setStrID(id, strYes)
	} else {
		e.setStrID(id, strNo)
	}
}

func (e Environment) setFromString(id ENV, v string) {
	switch v {
	case "yes":
		e.setBool(id, true)
	case "no":
		e.setBool(id, false)
	default:
		e.setString(id, v)
	}
}

// setFromStringID is setFromString for an already-interned value: store the STR
// directly, no string compare.
func (e Environment) setFromStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

func (e Environment) hasBindingID(id ENV) bool {
	k, _ := e.s.lookup(id)

	return k != envAbsent
}

// hasBinding reports whether name is bound without interning it, so an unknown
// ${VAR} token does not pollute the table.
func (e Environment) hasBinding(name string) bool {
	id := internedEnv(name)

	return id != 0 && e.hasBindingID(id)
}

// lookup returns name's bound value and whether it is bound, without interning
// name (same rationale as hasBinding).
func (e Environment) lookup(name string) (string, bool) {
	id := internedEnv(name)

	if id == 0 {
		return "", false
	}

	k, v := e.s.lookup(id)

	if k == envAbsent {
		return "", false
	}

	return v.string(), true
}

func (e Environment) setDefaultString(id ENV, v string) {
	if e.hasBindingID(id) {
		return
	}

	e.setString(id, v)
}
