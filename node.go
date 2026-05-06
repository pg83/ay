package main

// node.go — on-disk-aligned types mirroring the JSON shape produced by ymake.
//
// Field declaration order matters: encoding/json emits fields in the order
// they are declared in the struct, and the reference output in
// /home/pg/monorepo/yatool_orig/g.json lists keys alphabetically
// (cmds, deps, env, foreign_deps, host_platform, inputs, kv, outputs,
// platform, requirements, self_uid, stats_uid, tags, target_properties, uid).
// Keep the field ordering below in lockstep with that observation.
//
// `omitempty` is used only for fields g.json itself omits on most nodes:
// host_platform (present on ~half of nodes, always `true` when present) and
// foreign_deps (present on ~26 of ~3730 nodes). Every other field is
// always present in the on-disk JSON, even when empty, so they have no
// `omitempty` tag — that ensures empty arrays and maps render as `[]`/`{}`
// rather than vanishing.
//
// Rule authors do not assemble `Deps`/`ForeignDeps` directly — they call
// the Emitter, which hands back NodeRef values; Finalize resolves those
// refs to UID strings during the Merkle pass and populates the public
// `Deps`/`ForeignDeps` fields. The internal `DepRefs`/`ForeignDepRefs`
// fields carry the unresolved refs and are tagged `json:"-"` so they
// never serialize.

// Cmd is one command line in a node's `cmds` list.
type Cmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Env     map[string]string `json:"env"`
}

// Node is the on-disk representation of a build-graph node. Field order
// is alphabetical to match the reference g.json output.
//
// Public (JSON-tagged) fields are what the serializer writes. Internal
// fields (DepRefs, ForeignDepRefs) carry rule-author input that Finalize
// will resolve into the public Deps/ForeignDeps slices.
type Node struct {
	Cmds             []Cmd                  `json:"cmds"`
	Deps             []string               `json:"deps"`
	Env              map[string]string      `json:"env"`
	ForeignDeps      map[string][]string    `json:"foreign_deps,omitempty"`
	HostPlatform     bool                   `json:"host_platform,omitempty"`
	Inputs           []string               `json:"inputs"`
	KV               map[string]string      `json:"kv"`
	Outputs          []string               `json:"outputs"`
	Platform         string                 `json:"platform"`
	Requirements     map[string]interface{} `json:"requirements"`
	SelfUID          string                 `json:"self_uid"`
	StatsUID         string                 `json:"stats_uid"`
	Tags             []string               `json:"tags"`
	TargetProperties map[string]string      `json:"target_properties"`
	UID              string                 `json:"uid"`

	// Rule-author API; not serialized. Finalize resolves these into
	// Deps/ForeignDeps using the children's computed UIDs.
	DepRefs        []NodeRef            `json:"-"`
	ForeignDepRefs map[string][]NodeRef `json:"-"`
}
