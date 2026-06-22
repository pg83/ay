package main

import (
	"strconv"
	"strings"
)

type EnvKind uint8

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

func (e Environment) bool(id ENV) bool {
	return e.boolID(id, "")
}

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

func strIsTruthy(v STR) bool {
	switch v {
	case strYes:
		return true
	case strNo, 0:
		return false
	}

	return stringIsTruthy(v.string())
}

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

func (e Environment) setStrID(id ENV, v STR) {
	e.s.ensure(id)
	e.s.kind[id] = envStr
	e.s.val[id] = v
}

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

func (e Environment) setFromStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

func (e Environment) hasBindingID(id ENV) bool {
	k, _ := e.s.lookup(id)

	return k != envAbsent
}

func (e Environment) hasBinding(name string) bool {
	id := internedEnv(name)

	return id != 0 && e.hasBindingID(id)
}

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
