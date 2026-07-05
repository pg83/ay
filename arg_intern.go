package main

var argTable = struct {
	ids   DenseMap[STR, uint32]
	strs  PageVec[STR]
	count uint32
}{
	count: 1,
}

func init() {
	argTable.strs.set(0, 0)
}

type ARG uint32

func internArg(s string) ARG {
	return internArgSTR(internStr(s))
}

func internArgSTR(st STR) ARG {
	if id, ok := argTable.ids.get(st); ok {
		return ARG(id)
	}

	id := ARG(argTable.count)

	argTable.strs.set(argTable.count, st)
	argTable.count++
	argTable.ids.put(st, uint32(id))

	return id
}

func (a ARG) strID() uint32 {
	return uint32(a)
}

func (a ARG) str() STR {
	return argTable.strs.get(uint32(a))
}

func (a ARG) string() string {
	return argTable.strs.get(uint32(a)).string()
}

func (a ARG) sharedString() string {
	return argTable.strs.get(uint32(a)).sharedString()
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

func argSTRs(as []ARG) []STR {
	out := make([]STR, len(as))

	for i, a := range as {
		out[i] = a.str()
	}

	return out
}
