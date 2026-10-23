package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	DefaultIfEnv = makeDefaultIfEnv()
	ifExprRe     = regexp.MustCompile(`\\(?s:.)|\$([[:alnum:]]*)\{([^${}]+)\}`)
)

var intSTR = func() [1024]STR {
	var a [1024]STR

	for i := range a {
		a[i] = internStr(strconv.Itoa(i))
	}

	return a
}()

const (
	envAbsent EnvKind = iota
	envStr
	envInt
	envVFS
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

		return env.boolID(n.Env, n.Name)
	case ckConfIdent:
		return env.string(n.Env) != ""
	case ckNot:
		return !evalCondAt(nodes, n.L, env)
	case ckAnd:
		return evalCondAt(nodes, n.L, env) && evalCondAt(nodes, n.R, env)
	case ckOr:
		return evalCondAt(nodes, n.L, env) || evalCondAt(nodes, n.R, env)
	case ckString:
		throwFmt("macros: bare string %q cannot be evaluated as a boolean condition", n.Name)
	case ckEq:
		return evalIfAtomString(nodes, n.L, env, true) == evalIfAtomString(nodes, n.R, env, false)
	case ckConfEq:
		return evalConfEq(nodes, n, env)
	case ckLt, ckGt:
		return evalLt(nodes, n, env)
	case ckConfLt:
		return evalConfLt(nodes, n, env)
	case ckStartsWith:
		return strings.HasPrefix(evalIfAtomString(nodes, n.L, env, true), evalIfAtomString(nodes, n.R, env, false))
	case ckEndsWith:
		return strings.HasSuffix(evalIfAtomString(nodes, n.L, env, true), evalIfAtomString(nodes, n.R, env, false))
	case ckVersionCmp:
		return evalVersionCmp(nodes, n, env)
	case ckMatches:

		return strings.Contains(evalIfAtomString(nodes, n.L, env, true), evalIfAtomString(nodes, n.R, env, false))
	case ckUnsupportedOp:
		throwFmt("macros: unsupported IF operator %q", n.Name)
	case ckDefined:
		d := &nodes[n.L]

		if d.Kind != ckIdent {
			throwFmt("macros: DEFINED expects a variable name, got kind %d", d.Kind)
		}

		k, _ := env.s.lookup(d.Env)

		return k != envAbsent
	}

	throwFmt("macros: bare integer %d cannot be evaluated as a boolean condition", n.Ival)

	return false
}

func evalIfAtomString(nodes []CondNode, i int32, env Environment, variable bool) string {
	n := &nodes[i]

	switch n.Kind {
	case ckIdent:
		if variable && n.Name != "yes" && n.Name != "no" {
			return env.string(n.Env)
		}

		return n.Name
	case ckString:
		return evalIfExpr(n.Name, env)
	case ckInt:
		return n.Name
	}

	throwFmt("macros: unexpected cond kind %d in comparator operand position", n.Kind)

	return ""
}

func evalIfExpr(value string, env Environment) string {
	return ifExprRe.ReplaceAllStringFunc(value, func(expr string) string {
		if expr[0] == '\\' {
			return expr[1:]
		}

		parts := ifExprRe.FindStringSubmatch(expr)
		macro, key := parts[1], parts[2]

		switch macro {
		case "":
			if strings.HasSuffix(key, resourceGlobalSuffix) {
				return expr
			}

			value, _ := env.lookup(key)

			return value
		case "ENV":
			return os.Getenv(key)
		}

		return expr
	})
}

func evalVersionCmp(nodes []CondNode, n *CondNode, env Environment) bool {
	cmp := compareVersions(evalIfAtomString(nodes, n.L, env, true), evalIfAtomString(nodes, n.R, env, false))

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

func evalLt(nodes []CondNode, n *CondNode, env Environment) bool {
	l := ifInteger(evalIfAtomString(nodes, n.L, env, true))
	r := ifInteger(evalIfAtomString(nodes, n.R, env, false))

	if n.Kind == ckGt {
		return l > r
	}

	return l < r
}

func ifInteger(value string) int {
	n, err := strconv.Atoi(value)

	if err != nil {
		return 0
	}

	return n
}

func makeDefaultIfEnv() Environment {
	e := newEnvironment()

	for _, n := range []ENV{
		envTRUE, envUSE_SSE4,
		envUSE_ARCADIA_PYTHON, envPYTHON3,
		envUseCow,

		envUSE_PREBUILT_TOOLS, envCONSISTENT_DEBUG,
		envTsUsePrebuiltNotsTool,
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
	e.setString(envCppProtoCompatibleTags, "CPP_PROTO CPP_PROTO_DIRECT_HEADERS CPP_PROTO_LEGACY_FOR_DIRECT_HEADERS")
	e.setString(envALLOCATOR, "")
	e.setString(envPY2, "PY2")
	e.setString(envOS_SDK, "")

	e.setInt(envANDROID_API, 0)

	return e
}
