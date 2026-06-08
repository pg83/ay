package main

// ENV interns IF/macro variable names into a small, dense id space (the conf's
// fixed-ish flag set), separate from STR — analogous to STR/VFS. It lets
// Environment store bindings in ENV-indexed slices instead of three string-keyed
// maps, so Clone is a slice copy and Set/Get are array ops (the macro maps were
// ~8% of map-probing CPU plus per-module Clone churn). Single-writer (gen
// goroutine), like internTable.
type ENV uint32

// envTable, like argTable, stores no strings of its own: the name is interned
// once as a STR and the ENV records that STR. ids maps a name's STR to its ENV;
// strs maps an ENV back to that STR — a free ENV→STR conversion (ENV.str()).
var envTable = struct {
	ids  DenseMap[STR, uint32]
	strs []STR
}{strs: []STR{0}} // index 0 reserved = "not interned" (STR 0 is the empty string)

func internEnv(name string) ENV {
	st := internStr(name)

	if id, ok := envTable.ids.Get(st); ok {
		return ENV(id)
	}

	id := ENV(len(envTable.strs))
	envTable.strs = append(envTable.strs, st)
	envTable.ids.Put(st, uint32(id))

	return id
}

// str returns the STR backing this ENV — a free conversion (no re-interning).
func (id ENV) str() STR {
	return envTable.strs[id]
}

func (id ENV) String() string {
	return envTable.strs[id].String()
}

// internedEnv looks up name's ENV without interning it. Var-expansion probes
// arbitrary ${VAR} tokens that are usually not env vars; this stays read-only —
// interned() (a read-only STR probe) returns nil for a never-seen name, so a
// junk ${VAR} grows neither the STR nor the ENV table.
func internedEnv(name string) (ENV, bool) {
	st := interned(name)

	if st == nil {
		return 0, false
	}

	id, ok := envTable.ids.Get(*st)

	return ENV(id), ok
}
