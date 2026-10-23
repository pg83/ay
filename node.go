package main

type ArgChunks [][]ANY

func (c ArgChunks) flat() []ANY {
	total := 0

	for _, ch := range c {
		total += len(ch)
	}

	out := make([]ANY, 0, total)

	for _, ch := range c {
		out = append(out, ch...)
	}

	return out
}

type Cmd struct {
	CmdArgs ArgChunks `json:"cmd_args"`
	Cwd     VFS       `json:"cwd,omitempty"`
	Env     EnvVars   `json:"env,omitempty"`
	Stdout  VFS       `json:"stdout,omitempty"`
}

type Node struct {
	DisableCache        bool          `json:"-"`
	ResourceURIs        []STR         `json:"-"`
	Module              bool          `json:"-"`
	Cmds                []Cmd         `json:"cmds"`
	Env                 EnvVars       `json:"env"`
	Inputs              InputChunks   `json:"inputs"`
	BuildFromInputs     InputChunks   `json:"-"`
	TextInputs          []VFS         `json:"-"`
	InputNames          []STR         `json:"-"`
	InputClosure        Closure       `json:"-"`
	InputClosures       []Closure     `json:"-"`
	KV                  *KV           `json:"kv"`
	KVExts              []KVExt       `json:"-"`
	Outputs             []VFS         `json:"outputs"`
	Platform            *Platform     `json:"platform"`
	Requirements        *Requirements `json:"requirements"`
	Ref                 NodeRef       `json:"-"`
	PresetUID           *UID          `json:"-"`
	UIDSalt             STR           `json:"-"`
	DepRefs             []NodeRef     `json:"-"`
	ForeignDepRefs      []NodeRef     `json:"-"`
	Resources           []STR         `json:"-"`
	sourceInputsPending bool
}

func (n *Node) buildFromInputs() InputChunks {
	if n.BuildFromInputs != nil {
		return n.BuildFromInputs
	}

	return n.Inputs
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
				yield(ref)
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
