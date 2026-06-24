package main

import (
	"regexp"
	"strconv"
	"strings"
)

var DefaultIfEnv = makeDefaultIfEnv()

var intSTR = func() [1024]STR {
	var a [1024]STR

	for i := range a {
		a[i] = internStr(strconv.Itoa(i))
	}

	return a
}()

func evalAtomString(nodes []CondNode, i int32, env Environment) string {
	switch v := evalAtomNode(nodes, i, env).(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case bool:
		if v {
			return "yes"
		}

		return "no"
	}

	return ""
}

func identEnvNode(n *CondNode) ENV {
	if n.Env != 0 {
		return n.Env
	}

	return internEnv(n.Name)
}

const (
	envAbsent EnvKind = iota
	envStr
	envInt
)

func evalCond(nodes []CondNode, env Environment) bool {
	return evalCondAt(nodes, int32(len(nodes)-1), env)
}

func evalCondAt(nodes []CondNode, i int32, env Environment) bool {
	n := &nodes[i]

	switch n.Kind {
	case ckIdent:
		if n.Name == "yes" {
			return true
		}

		if n.Name == "no" {
			return false
		}

		return env.boolID(identEnvNode(n), n.Name)
	case ckNot:
		return !evalCondAt(nodes, n.L, env)
	case ckAnd:
		return evalCondAt(nodes, n.L, env) && evalCondAt(nodes, n.R, env)
	case ckOr:
		return evalCondAt(nodes, n.L, env) || evalCondAt(nodes, n.R, env)
	case ckString:
		throwFmt("macros: bare string %q cannot be evaluated as a boolean condition", n.Name)
	case ckInt:
		throwFmt("macros: bare integer %d cannot be evaluated as a boolean condition", n.Ival)
	case ckEq:
		return evalEq(nodes, n, env)
	case ckLt:
		return evalLt(nodes, n, env)
	case ckStartsWith:
		return strings.HasPrefix(evalAtomString(nodes, n.L, env), evalAtomString(nodes, n.R, env))
	case ckVersionCmp:
		return evalVersionCmp(nodes, n, env)
	case ckMatches:
		return throw2(regexp.MatchString(evalAtomString(nodes, n.R, env), evalAtomString(nodes, n.L, env)))
	case ckDefined:
		d := &nodes[n.L]

		if d.Kind != ckIdent {
			throwFmt("macros: DEFINED expects a variable name, got kind %d", d.Kind)
		}

		k, _ := env.s.lookup(identEnvNode(d))

		return k != envAbsent
	}

	throwFmt("macros: unhandled cond kind %d", n.Kind)

	return false
}

func evalAtomNode(nodes []CondNode, i int32, env Environment) any {
	n := &nodes[i]

	switch n.Kind {
	case ckIdent:
		if n.Name == "yes" || n.Name == "no" {
			return n.Name
		}

		switch k, v := env.s.lookup(identEnvNode(n)); k {
		case envStr:
			return v.string()
		case envInt:
			x, _ := strconv.Atoi(v.string())

			return x
		}

		if isImplicitBuildVar(n.Name) {
			return n.Name
		}

		throwFmt("macros: unknown IF identifier %q", n.Name)

		return nil
	case ckString:
		return n.Name
	case ckInt:
		return n.Ival
	}

	throwFmt("macros: unexpected cond kind %d in comparator operand position", n.Kind)

	return nil
}

func evalVersionCmp(nodes []CondNode, n *CondNode, env Environment) bool {
	cmp := compareVersions(evalAtomString(nodes, n.L, env), evalAtomString(nodes, n.R, env))

	switch n.Name {
	case "VERSION_LT":
		return cmp < 0
	case "VERSION_LE":
		return cmp <= 0
	case "VERSION_GT":
		return cmp > 0
	case "VERSION_GE":
		return cmp >= 0
	case "VERSION_EQ":
		return cmp == 0
	}

	throwFmt("macros: unknown version operator %q", n.Name)

	return false
}

func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")

	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int

		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}

		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}

		if av != bv {
			if av < bv {
				return -1
			}

			return 1
		}
	}

	return 0
}

func evalEq(nodes []CondNode, n *CondNode, env Environment) bool {
	l := evalAtomNode(nodes, n.L, env)
	r := evalAtomNode(nodes, n.R, env)

	switch lv := l.(type) {
	case string:
		if rv, ok := r.(bool); ok {
			if rv {
				return lv == "yes"
			}

			return lv == "no"
		}

		if rv, ok := r.(int); ok {
			return lv == strconv.Itoa(rv)
		}

		rv, ok := r.(string)

		if !ok {
			throwFmt("macros: == operand type mismatch: left is string %q, right is %T", lv, r)
		}

		return lv == rv
	case int:
		rv, ok := r.(int)

		if !ok {
			throwFmt("macros: == operand type mismatch: left is int %d, right is %T", lv, r)
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
			throwFmt("macros: == operand type mismatch: left is bool %v, right is %T", lv, r)
		}

		return lv == rv
	}

	throwFmt("macros: == operand has unsupported dynamic type %T", l)

	return false
}

func evalLt(nodes []CondNode, n *CondNode, env Environment) bool {
	l := evalAtomNode(nodes, n.L, env)
	r := evalAtomNode(nodes, n.R, env)

	li, lok := l.(int)
	ri, rok := r.(int)

	if !lok || !rok {
		throwFmt("macros: < requires int operands, got left=%T right=%T", l, r)
	}

	return li < ri
}

func makeDefaultIfEnv() Environment {
	e := newEnvironment()

	for _, n := range []ENV{
		envOS_LINUX, envLINUX,
		envCLANG, envTRUE, envUSE_SSE4,
		envUSE_ARCADIA_PYTHON, envPYTHON3,

		envUSE_PREBUILT_TOOLS,
	} {
		e.setBool(n, true)
	}

	e.setString(envARCADIA_ROOT, "$(S)")
	e.setString(envARCADIA_BUILD_ROOT, "$(B)")

	e.setString(envCXX_RT, "libcxxrt")

	e.setString(envCORE_LIBS_OPTIMIZATION, "-O3")
	e.setString(envOPENSOURCE_PROJECT, "")
	e.setString(envSANITIZER_TYPE, "")
	e.setString(envundefined, "undefined")
	e.setString(envmemory, "memory")
	e.setString(envaddress, "address")
	e.setString(envthread, "thread")
	e.setString(envleak, "leak")
	e.setString(envMODULE_TAG, "PY3")
	e.setString(envALLOCATOR, "")
	e.setString(envPY2, "PY2")
	e.setString(envOS_SDK, "")

	e.setInt(envANDROID_API, 0)

	return e
}
