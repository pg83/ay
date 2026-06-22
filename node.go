package main

import "encoding/json"

type ArgChunks [][]STR

func (c ArgChunks) marshalJSON() ([]byte, error) {
	return json.Marshal(c.flat())
}

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

	Resources []STR `json:"-"`
}

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

func depRefs(refs ...NodeRef) []NodeRef {
	var out []NodeRef

	for _, r := range refs {
		if r != 0 {
			out = append(out, r)
		}
	}

	return out
}

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

type InputChunks [][]VFS

func (c InputChunks) marshalJSON() ([]byte, error) {
	return json.Marshal(c.flat())
}

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

func (n *Node) flatInputs() []VFS {
	return n.Inputs.flat()
}
