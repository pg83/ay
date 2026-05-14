package main

// node.go — on-disk-aligned types mirroring the JSON shape produced by ymake.
//
// Field declaration order matters: encoding/json emits in declaration order,
// and the reference /home/pg/monorepo/yatool_orig/sg.json lists keys
// alphabetically. Keep field ordering in lockstep.
//
// `omitempty` is used only on fields sg.json itself omits on most nodes —
// host_platform (~half of nodes, always true when present) and foreign_deps
// (~26 of ~3730). Everything else is always-present so empty maps/arrays
// render as `[]`/`{}` rather than vanish. stats_uid is json:"-": REF's
// 32-char hex derivation is out of scope and the normalizer also drops it.
//
// Rule authors assemble Deps/ForeignDeps indirectly via NodeRef; Finalize
// resolves refs to UID strings during the Merkle pass. The internal
// DepRefs/ForeignDepRefs fields carry the unresolved refs (json:"-").

// Cmd is one command line in a node's `cmds` list. Field order is
// alphabetical (cmd_args, cwd, env, stdout). Cwd/Stdout are omitempty: only
// LD cmd[2] (link_exe.py) and 58 of 83 AS nodes carry cwd; stdout appears
// on exactly one PR node. Emitting empty values would diverge byte-for-byte.
type Cmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env"`
	Stdout  string            `json:"stdout,omitempty"`
}

// Node is the on-disk representation of a build-graph node (alphabetical
// field order matches reference g.json). DepRefs/ForeignDepRefs carry
// rule-author input that Finalize resolves into public Deps/ForeignDeps.
//
// Cache is tri-state: nil omits the key (the common case); non-nil emits
// `cache: true`/`cache: false`. The only reference node with explicit
// `cache: false` is BI buildinfo_data.h (non-deterministic).
type Node struct {
	Cache            *bool                  `json:"cache,omitempty"`
	Cmds             []Cmd                  `json:"cmds"`
	Deps             []string               `json:"deps"`
	Env              map[string]string      `json:"env"`
	ForeignDeps      map[string][]string    `json:"foreign_deps,omitempty"`
	HostPlatform     bool                   `json:"host_platform,omitempty"`
	Inputs           []VFS                  `json:"inputs"`
	KV               map[string]string      `json:"kv"`
	Outputs          []VFS                  `json:"outputs"`
	Platform         string                 `json:"platform"`
	Requirements     map[string]interface{} `json:"requirements"`
	Sandboxing       bool                   `json:"sandboxing"`
	SelfUID          string                 `json:"self_uid"`
	StatsUID         string                 `json:"-"`
	Tags             []string               `json:"tags"`
	TargetProperties map[string]string      `json:"target_properties"`
	UID              string                 `json:"uid"`

	// Rule-author API; not serialized. Finalize resolves these into
	// Deps/ForeignDeps using the children's computed UIDs.
	DepRefs        []NodeRef            `json:"-"`
	ForeignDepRefs map[string][]NodeRef `json:"-"`
}
