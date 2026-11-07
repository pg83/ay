package main

var envTable = struct {
	ids   DenseMap[STR, uint32]
	strs  PageVec[STR]
	count uint32
}{
	count: 1,
}

var recentEnvNames struct {
	text [8]string
	id   [8]ENV
	next int
}

func init() {
	envTable.strs.set(0, 0)
}

type ENV uint32

func internEnv(name string) ENV {
	return internEnvSTR(internStr(name))
}

func internEnvSTR(st STR) ENV {
	if id, ok := envTable.ids.get(st); ok {
		return ENV(id)
	}

	id := ENV(envTable.count)

	envTable.strs.set(envTable.count, st)
	envTable.count++
	envTable.ids.put(st, uint32(id))

	return id
}

func (id ENV) str() STR {
	return envTable.strs.get(uint32(id))
}

func (id ENV) string() string {
	return envTable.strs.get(uint32(id)).string()
}

func (id ENV) sharedString() string {
	return envTable.strs.get(uint32(id)).sharedString()
}

func internedEnvRecent(name string) ENV {
	c := &recentEnvNames

	for i := range c.text {
		if c.id[i] != 0 && c.text[i] == name {
			return c.id[i]
		}
	}

	id := internedEnv(name)

	if id != 0 {
		c.text[c.next], c.id[c.next] = id.string(), id
		c.next = (c.next + 1) & 7
	}

	return id
}

func internedEnv(name string) ENV {
	st := interned(name)

	if st == 0 {
		return 0
	}

	id, _ := envTable.ids.get(st)

	return ENV(id)
}
