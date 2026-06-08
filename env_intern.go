package main

// ENV interns IF/macro variable names into a small, dense id space (the conf's
// fixed-ish flag set), separate from STR — analogous to STR/VFS. It lets
// Environment store bindings in ENV-indexed slices instead of three string-keyed
// maps, so Clone is a slice copy and Set/Get are array ops (the macro maps were
// ~8% of map-probing CPU plus per-module Clone churn). Single-writer (gen
// goroutine), like internTable.
type ENV uint32

var envTable = struct {
	ids   map[string]ENV
	names []string
}{ids: make(map[string]ENV, 256), names: []string{""}} // index 0 reserved = "not interned"

func internEnv(name string) ENV {
	if id, ok := envTable.ids[name]; ok {
		return id
	}

	id := ENV(len(envTable.names))
	envTable.ids[name] = id
	envTable.names = append(envTable.names, name)

	return id
}

func (id ENV) String() string {
	return envTable.names[id]
}

// internedEnv looks up name's ENV without interning it (envTable read-only,
// like interned() for STR). Var-expansion probes arbitrary ${VAR} tokens that
// are usually not env vars; interning to check would grow the ENV table with
// junk, so presence is tested via this lookup first.
func internedEnv(name string) (ENV, bool) {
	id, ok := envTable.ids[name]

	return id, ok
}
