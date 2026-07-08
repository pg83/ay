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

func (a ARG) any() ANY {
	return a.str().any()
}
