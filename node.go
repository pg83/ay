package main

// node.go — on-disk-aligned types mirroring the JSON shape produced by ymake.
//
// Field declaration order matters: encoding/json emits in declaration order,
// and reference sg.json lists keys alphabetically. Keep in lockstep.
//
// `omitempty` is used only on fields sg.json itself omits on most nodes
// (host_platform, foreign_deps); everything else always serializes so empty
// maps/arrays render as `[]`/`{}` rather than vanish. stats_uid is part of
// the raw graph shape; hidden stats-preimage helpers stay json:"-".
//
// Rule authors assemble deps via NodeRef; Finalize resolves refs to UID
// strings during the Merkle pass.

// Cmd is one command line in a node's `cmds` list. Field order is
// alphabetical (cmd_args, cwd, env, stdout). Cwd/Stdout are omitempty: only
// LD cmd[2] (link_exe.py) and 58 of 83 AS nodes carry cwd; stdout appears
// on exactly one PR node. Emitting empty values would diverge byte-for-byte.
type Cmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Stdout  string            `json:"stdout,omitempty"`
}

// Node is the on-disk representation of a build-graph node (alphabetical
// field order matches reference sg.json). DepRefs/ForeignDepRefs carry
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
	Inputs           []VFS                  `json:"inputs"`
	KV               map[string]interface{} `json:"kv"`
	Outputs          []VFS                  `json:"outputs"`
	Platform         string                 `json:"platform"`
	Requirements     map[string]interface{} `json:"requirements"`
	Sandboxing       bool                   `json:"sandboxing"`
	SelfUID          string                 `json:"self_uid"`
	StatsUID         string                 `json:"stats_uid"`
	Tags             []string               `json:"tags"`
	TargetProperties map[string]string      `json:"target_properties"`
	UID              string                 `json:"uid"`

	// Hidden stats-preimage data. Raw refs hash pre-strip tags rather than
	// the visible post-strip raw JSON tags, so emitters bind the originating
	// Platform here before Finalize computes StatsUID.
	StatsTags []string `json:"-"`

	// Rule-author API; not serialized. Finalize resolves these into
	// Deps/ForeignDeps using the children's computed UIDs.
	DepRefs        []NodeRef            `json:"-"`
	ForeignDepRefs map[string][]NodeRef `json:"-"`
}

// nodeHasHostTag reports whether tags carry the baseline `"tool"` tag
// that Platform.Tags injects on the host axis. Drives `host_platform`
// JSON emission.
func nodeHasHostTag(tags []string) bool {
	for _, t := range tags {
		if t == "tool" {
			return true
		}
	}
	return false
}

// bindNodePlatform keeps the public node platform string and the hidden
// stats-preimage tags in sync with the source Platform.
func bindNodePlatform(n *Node, p *Platform) *Node {
	if n == nil || p == nil {
		return n
	}

	n.Platform = string(p.Target)
	n.StatsTags = statsTagsForPlatform(p)

	return n
}
