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
	v := evalAtomNode(nodes, i, env)

	if v.isNum {
		return strconv.Itoa(v.num)
	}

	return v.str
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

type atomVal struct {
	num   int
	str   string
	isNum bool
}

func atomTypeName(v atomVal) string {
	if v.isNum {
		return "int"
	}

	return "string"
}

func evalAtomNode(nodes []CondNode, i int32, env Environment) atomVal {
	n := &nodes[i]

	switch n.Kind {
	case ckIdent:
		if n.Name == "yes" || n.Name == "no" {
			return atomVal{str: n.Name}
		}

		switch k, v := env.s.lookup(identEnvNode(n)); k {
		case envStr:
			return atomVal{str: v.string()}
		case envInt:
			x, _ := strconv.Atoi(v.string())

			return atomVal{num: x, isNum: true}
		}

		if isImplicitBuildVar(n.Name) {
			return atomVal{str: n.Name}
		}

		throwFmt("macros: unknown IF identifier %q", n.Name)

		return atomVal{}
	case ckString:
		return atomVal{str: n.Name}
	case ckInt:
		return atomVal{num: n.Ival, isNum: true}
	}

	throwFmt("macros: unexpected cond kind %d in comparator operand position", n.Kind)

	return atomVal{}
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

	if l.isNum {
		if !r.isNum {
			throwFmt("macros: == operand type mismatch: left is int %d, right is %s", l.num, atomTypeName(r))
		}

		return l.num == r.num
	}

	if r.isNum {
		return l.str == strconv.Itoa(r.num)
	}

	return l.str == r.str
}

func evalLt(nodes []CondNode, n *CondNode, env Environment) bool {
	l := evalAtomNode(nodes, n.L, env)
	r := evalAtomNode(nodes, n.R, env)

	if !l.isNum || !r.isNum {
		throwFmt("macros: < requires int operands, got left=%s right=%s", atomTypeName(l), atomTypeName(r))
	}

	return l.num < r.num
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
