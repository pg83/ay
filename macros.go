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

func evalAtomString(e Expr, env Environment) string {
	switch v := evalAtom(e, env).(type) {
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

func identEnv(x *ExprIdent) ENV {
	if x.Env != 0 {
		return x.Env
	}

	return internEnv(x.Name)
}

const (
	envAbsent EnvKind = iota
	envStr
	envInt
)

func evalCond(e Expr, env Environment) bool {
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
		return !evalCond(x.Of, env)
	case *ExprAnd:
		return evalCond(x.Left, env) && evalCond(x.Right, env)
	case *ExprOr:
		return evalCond(x.Left, env) || evalCond(x.Right, env)
	case *ExprString:
		throwFmt("macros: bare string %q cannot be evaluated as a boolean condition", x.Value)
	case *ExprInt:
		throwFmt("macros: bare integer %d cannot be evaluated as a boolean condition", x.Value)
	case *ExprEq:
		return evalEq(x, env)
	case *ExprLt:
		return evalLt(x, env)
	case *ExprStartsWith:
		return strings.HasPrefix(evalAtomString(x.Left, env), evalAtomString(x.Right, env))
	case *ExprVersionCmp:
		return evalVersionCmp(x, env)
	case *ExprMatches:
		return throw2(regexp.MatchString(evalAtomString(x.Right, env), evalAtomString(x.Left, env)))
	case *ExprDefined:
		id, ok := x.Of.(*ExprIdent)

		if !ok {
			throwFmt("macros: DEFINED expects a variable name, got %T", x.Of)
		}

		k, _ := env.s.lookup(identEnv(id))

		return k != envAbsent
	}

	throwFmt("macros: unhandled Expr type %T", e)

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
			return v.string()
		case envInt:
			n, _ := strconv.Atoi(v.string())

			return n
		}

		if isImplicitBuildVar(x.Name) {
			return x.Name
		}

		throwFmt("macros: unknown IF identifier %q", x.Name)

		return nil
	case *ExprString:
		return x.Value
	case *ExprInt:
		return x.Value
	}

	throwFmt("macros: unexpected Expr type %T in comparator operand position", e)

	return nil
}

func evalVersionCmp(x *ExprVersionCmp, env Environment) bool {
	cmp := compareVersions(evalAtomString(x.Left, env), evalAtomString(x.Right, env))

	switch x.Op {
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

	throwFmt("macros: unknown version operator %q", x.Op)

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

func evalLt(x *ExprLt, env Environment) bool {
	l := evalAtom(x.Left, env)
	r := evalAtom(x.Right, env)

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
