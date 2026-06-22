package main

// ENV interns IF/macro variable names into a small, dense id space, separate
// from STR, so Environment stores bindings in ENV-indexed slices instead of
// string-keyed maps. Single-writer, like internTable.
type ENV uint32

// envTable stores no strings of its own: the name is interned once as a STR.
// ids maps a name's STR to its ENV; strs maps an ENV back to that STR.
var envTable = struct {
	ids  DenseMap[STR, uint32]
	strs []STR
}{strs: []STR{0}} // index 0 reserved = "not interned"

func internEnv(name string) ENV {
	return internEnvSTR(internStr(name))
}

// internEnvSTR interns an already-interned name STR into the ENV namespace.
func internEnvSTR(st STR) ENV {
	if id, ok := envTable.ids.get(st); ok {
		return ENV(id)
	}

	id := ENV(len(envTable.strs))
	envTable.strs = append(envTable.strs, st)
	envTable.ids.put(st, uint32(id))

	return id
}

// str returns the STR backing this ENV.
func (id ENV) str() STR {
	return envTable.strs[id]
}

func (id ENV) string() string {
	return envTable.strs[id].string()
}

// String implements fmt.Stringer; internal code calls string().
func (id ENV) String() string {
	return id.string()
}

// internedEnv looks up name's ENV without interning it: var-expansion probes
// arbitrary ${VAR} tokens, and a never-seen name must not grow either table.
func internedEnv(name string) ENV {
	st := interned(name)

	if st == 0 {
		return 0
	}

	id, _ := envTable.ids.get(st)

	return ENV(id) // slot 0 is reserved, so 0 doubles as "unknown"
}
