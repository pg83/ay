package main

type ENV uint32

var envTable = struct {
	ids  DenseMap[STR, uint32]
	strs []STR
}{strs: []STR{0}}

func internEnv(name string) ENV {
	return internEnvSTR(internStr(name))
}

func internEnvSTR(st STR) ENV {
	if id, ok := envTable.ids.get(st); ok {
		return ENV(id)
	}

	id := ENV(len(envTable.strs))
	envTable.strs = append(envTable.strs, st)
	envTable.ids.put(st, uint32(id))

	return id
}

func (id ENV) str() STR {
	return envTable.strs[id]
}

func (id ENV) string() string {
	return envTable.strs[id].string()
}

func (id ENV) String() string {
	return id.string()
}

func internedEnv(name string) ENV {
	st := interned(name)

	if st == 0 {
		return 0
	}

	id, _ := envTable.ids.get(st)

	return ENV(id)
}
