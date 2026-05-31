package main

import (
	"fmt"
	"strings"
)

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

// Bool reads name as a boolean IF-flag. The IF flag namespace is unified
// and lazy: an unset name is indistinguishable from an explicit false
// (mirroring upstream ymake — $X EvalValue returns "" for unbound vars,
// and NYMake::IsTrue treats empty / any falseWord as false). The only
// typed error is an int binding used in boolean position.
func (e Environment) Bool(name string) bool {
	if v, ok := e.bools[name]; ok {
		return v
	}

	if v, ok := e.strings[name]; ok {
		return stringIsTruthy(v)
	}

	if _, ok := e.ints[name]; ok {
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

	return ""
}

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

func (e Environment) SetDefaultString(name, value string) {
	if e.HasBinding(name) {
		return
	}

	e.strings[name] = value
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

	return false
}

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

var DefaultIfEnv = Environment{
	bools: map[string]bool{
		"OS_LINUX": true,
		"LINUX":    true,

		"CLANG":    true,
		"TRUE":     true,
		"USE_SSE4": true,

		"OPENSOURCE": true,

		"USE_ARCADIA_PYTHON": true,
		"PYTHON3":            true,
	},
	strings: map[string]string{

		"CXX_RT": "libcxxrt",

		"OPENSOURCE_PROJECT": "",

		"SANITIZER_TYPE": "",

		"undefined": "undefined",
		"memory":    "memory",
		"address":   "address",
		"thread":    "thread",
		"leak":      "leak",

		"MODULE_TAG": "PY3",

		"_USE_ICONV": "dynamic",
		"ALLOCATOR":  "",
		"PY2":        "PY2",
		"OS_SDK":     "",
	},
	ints: map[string]int{

		"ANDROID_API": 0,
	},
}
