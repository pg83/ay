package main

var argTable = struct {
	ids  DenseMap[STR, uint32]
	strs []STR
}{
	strs: make([]STR, 1, 256),
}

type ARG uint32

func internArg(s string) ARG {
	return internArgSTR(internStr(s))
}

func internArgSTR(st STR) ARG {
	if id, ok := argTable.ids.get(st); ok {
		return ARG(id)
	}

	id := ARG(len(argTable.strs))
	argTable.strs = append(argTable.strs, st)
	argTable.ids.put(st, uint32(id))

	return id
}

func (a ARG) str() STR {
	return argTable.strs[a]
}

func (a ARG) string() string {
	return argTable.strs[a].string()
}

func (a ARG) String() string {
	return a.string()
}

func internArgs(ss []string) []ARG {
	if len(ss) == 0 {
		return nil
	}

	out := make([]ARG, len(ss))

	for i, s := range ss {
		out[i] = internArg(s)
	}

	return out
}

func appendArgStrs(dst []string, srcs ...[]ARG) []string {
	for _, s := range srcs {
		for _, a := range s {
			dst = append(dst, a.string())
		}
	}

	return dst
}

func argStrs(as []ARG) []string {
	if len(as) == 0 {
		return nil
	}

	out := make([]string, len(as))

	for i, a := range as {
		out[i] = a.string()
	}

	return out
}

func dedupARG(lists ...[]ARG) []ARG {
	deduper.reset()

	var out []ARG

	for _, l := range lists {
		for _, a := range l {
			if deduper.add(VFS(a)) {
				out = append(out, a)
			}
		}
	}

	return out
}
