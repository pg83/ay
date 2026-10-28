package main

import (
	"slices"
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
		return env.bool(n.Env)
	case ckConfIdent:
		return env.string(n.Env) != ""
	case ckNot:
		return !evalCondAt(nodes, n.L, env)
	case ckAnd:
		return evalCondAt(nodes, n.L, env) && evalCondAt(nodes, n.R, env)
	case ckOr:
		return evalCondAt(nodes, n.L, env) || evalCondAt(nodes, n.R, env)
	case ckString:
		return stringIsTruthy(evalIfExpr(n.Name, env))
	case ckEq:
		return evalIfAtomString(nodes, n.L, env) == evalIfAtomString(nodes, n.R, env)
	case ckConfEq:
		return evalConfEq(nodes, n, env)
	case ckLt, ckGt:
		return evalLt(nodes, n, env)
	case ckConfLt:
		return evalConfLt(nodes, n, env)
	case ckStartsWith:
		return strings.HasPrefix(evalIfAtomString(nodes, n.L, env), evalIfAtomString(nodes, n.R, env))
	case ckEndsWith:
		return strings.HasSuffix(evalIfAtomString(nodes, n.L, env), evalIfAtomString(nodes, n.R, env))
	case ckVersionCmp:
		return evalVersionCmp(nodes, n, env)
	case ckMatches:
		return strings.Contains(evalIfAtomString(nodes, n.L, env), evalIfAtomString(nodes, n.R, env))
	case ckUnsupportedOp:
		throwFmt("macros: unsupported IF operator %q", n.Name)
	case ckDefined:
		return env.hasBindingID(n.Env)
	case ckIsNum:
		_, err := strconv.ParseInt(evalIsNumOperand(n, env), 10, 64)

		return err == nil
	}

	throwFmt("macros: bare integer %d cannot be evaluated as a boolean condition", n.Ival)

	return false
}

func evalIfAtomString(nodes []CondNode, i int32, env Environment) string {
	n := &nodes[i]

	switch n.Kind {
	case ckIdent:
		return env.string(n.Env)
	case ckString:
		return evalIfExpr(n.Name, env)
	case ckDefined, ckIsNum:
		if evalCondAt(nodes, i, env) {
			return "yes"
		}

		return "no"
	}

	throwFmt("macros: unexpected cond kind %d in comparator operand position", n.Kind)

	return ""
}

func evalIsNumOperand(n *CondNode, env Environment) string {
	if env.hasBindingID(n.Env) {
		return env.string(n.Env)
	}

	return evalIfExpr(n.Name, env)
}

func evalIfExpr(value string, env Environment) string {
	return expandMakeExpression(value, env, func(name string) bool {
		return strings.HasSuffix(name, resourceGlobalSuffix)
	})
}

func evalVersionCmp(nodes []CondNode, n *CondNode, env Environment) bool {
	cmp := slices.Compare(ifVersion(evalIfAtomString(nodes, n.L, env)), ifVersion(evalIfAtomString(nodes, n.R, env)))

	switch n.Name {
	case "VERSION_LT":
		return cmp < 0
	case "VERSION_LE":
		return cmp <= 0
	case "VERSION_GT":
		return cmp > 0
	}

	return cmp >= 0
}

func ifVersion(value string) []uint64 {
	var out []uint64

	for rest := value; len(out) < 3; {
		end := strings.IndexAny(rest, ".-")
		part := rest

		if end >= 0 {
			part = rest[:end]
		}

		n, err := strconv.ParseUint(part, 10, 32)

		if err != nil {
			throwFmt("macros: cannot parse version %q", value)
		}

		out = append(out, n)

		if end < 0 {
			break
		}

		rest = rest[end+1:]
	}

	return out
}

func evalLt(nodes []CondNode, n *CondNode, env Environment) bool {
	l := ifInteger(evalIfAtomString(nodes, n.L, env))
	r := ifInteger(evalIfAtomString(nodes, n.R, env))

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
	e.setString(envPY2, "PY2")
	e.setString(envOS_SDK, "")

	e.setInt(envANDROID_API, 0)

	return e
}

func evalConfAtomString(nodes []CondNode, i int32, env Environment) string {
	n := &nodes[i]

	switch n.Kind {
	case ckConfIdent:
		return env.string(n.Env)
	case ckString:
		return n.Name
	case ckInt:
		return strconv.Itoa(n.Ival)
	}

	throwFmt("macros: unexpected cond kind %d in comparator operand position", n.Kind)

	return ""
}

func evalConfEq(nodes []CondNode, n *CondNode, env Environment) bool {
	if confStringOperand(&nodes[n.L]) && confStringOperand(&nodes[n.R]) {
		return evalConfAtomString(nodes, n.L, env) == evalConfAtomString(nodes, n.R, env)
	}

	return confNumber(nodes, n.L, env) == confNumber(nodes, n.R, env)
}

func evalConfLt(nodes []CondNode, n *CondNode, env Environment) bool {
	if confStringOperand(&nodes[n.L]) && confStringOperand(&nodes[n.R]) {
		return evalConfAtomString(nodes, n.L, env) < evalConfAtomString(nodes, n.R, env)
	}

	return confNumber(nodes, n.L, env) < confNumber(nodes, n.R, env)
}

func confStringOperand(n *CondNode) bool {
	return n.Kind == ckConfIdent || n.Kind == ckString
}

func confNumber(nodes []CondNode, i int32, env Environment) int {
	n := &nodes[i]

	switch n.Kind {
	case ckInt:
		return n.Ival
	case ckConfIdent, ckString:
		if evalConfAtomString(nodes, i, env) != "" {
			return 1
		}

		return 0
	}

	if evalCondAt(nodes, i, env) {
		return 1
	}

	return 0
}
