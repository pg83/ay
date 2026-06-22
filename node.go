package main

import "encoding/json"

// argChunks is a command's chunked arg list. It JSON-marshals FLAT — the chunking
// is an internal zero-copy layout (shared pre-built blocks), not schema.
type ArgChunks [][]STR

func (c ArgChunks) marshalJSON() ([]byte, error) {
	return json.Marshal(c.flat())
}

// MarshalJSON implements json.Marshaler; internal code calls marshalJSON().
func (c ArgChunks) MarshalJSON() ([]byte, error) {
	return c.marshalJSON()
}

func (c ArgChunks) flat() []STR {
	total := 0

	for _, ch := range c {
		total += len(ch)
	}

	out := make([]STR, 0, total)

	for _, ch := range c {
		out = append(out, ch...)
	}

	return out
}

type Cmd struct {
	CmdArgs ArgChunks `json:"cmd_args"`
	Cwd     STR       `json:"cwd,omitempty"`
	Env     EnvVars   `json:"env,omitempty"`
	Stdout  STR       `json:"stdout,omitempty"`
}

type Node struct {
	Cache *bool   `json:"cache,omitempty"`
	Cmds  []Cmd   `json:"cmds"`
	Env   EnvVars `json:"env"`
	// Inputs holds the node's input paths as chunks: emitters hand over their
	// natural pieces WITHOUT flattening, so a large closure slice is referenced,
	// never copied. The flattened sequence is the node's input list.
	Inputs           InputChunks      `json:"inputs"`
	KV               KV               `json:"kv"`
	Outputs          []VFS            `json:"outputs"`
	Platform         *Platform        `json:"platform"`
	Requirements     Requirements     `json:"requirements"`
	Sandboxing       bool             `json:"sandboxing"`
	SelfUID          UID              `json:"self_uid"`
	TargetProperties TargetProperties `json:"target_properties"`
	UID              UID              `json:"uid"`

	DepRefs        []NodeRef `json:"-"`
	ForeignDepRefs []NodeRef `json:"-"`

	// Resources lists the fetched-resource names whose tool the node's command
	// invokes via $(B)/resources/<NAME>. The resource emitter turns each into a
	// dependency on that resource's FETCH node.
	Resources []STR `json:"-"`
}

// buildDeps yields every ref that must be built/restored before this node runs:
// DepRefs (build inputs), ForeignDepRefs (tool deps), then the resolved resource
// FETCH deps. The "deps" array, the UID, and the executor all use this sequence.
func (n *Node) buildDeps(fetchRefs *DenseMap[STR, NodeRef]) func(func(NodeRef) bool) {
	return func(yield func(NodeRef) bool) {
		for _, r := range n.DepRefs {
			if !yield(r) {
				return
			}
		}

		for _, r := range n.ForeignDepRefs {
			if !yield(r) {
				return
			}
		}

		for _, pat := range n.Resources {
			if ref, ok := fetchRefs.get(pat); ok {
				if !yield(ref) {
					return
				}
			}
		}
	}
}

// depRefs collects refs into a dep slice, dropping NodeRef(0) — the "absent"
// sentinel for an unresolved optional tool or producer. Returns nil when every
// ref is zero.
func depRefs(refs ...NodeRef) []NodeRef {
	var out []NodeRef

	for _, r := range refs {
		if r != 0 {
			out = append(out, r)
		}
	}

	return out
}

// dedupRefs returns refs with duplicate NodeRefs removed, keeping first
// occurrence and insertion order. Slices are tiny, so a linear scan beats a map.
func dedupRefs(refs []NodeRef) []NodeRef {
	out := refs[:0]

	for _, r := range refs {
		dup := false

		for _, seen := range out {
			if seen == r {
				dup = true

				break
			}
		}

		if !dup {
			out = append(out, r)
		}
	}

	return out
}

// inputChunks is the chunked input list. It JSON-marshals FLAT — the chunking
// is an internal zero-copy layout (shared slices), not schema.
type InputChunks [][]VFS

func (c InputChunks) marshalJSON() ([]byte, error) {
	return json.Marshal(c.flat())
}

// MarshalJSON implements json.Marshaler; internal code calls marshalJSON().
func (c InputChunks) MarshalJSON() ([]byte, error) {
	return c.marshalJSON()
}

func (c InputChunks) flat() []VFS {
	total := 0

	for _, ch := range c {
		total += len(ch)
	}

	out := make([]VFS, 0, total)

	for _, ch := range c {
		out = append(out, ch...)
	}

	return out
}

// flatInputs flattens the input chunks into one slice — for cold consumers;
// hot consumers iterate the chunks.
func (n *Node) flatInputs() []VFS {
	return n.Inputs.flat()
}
