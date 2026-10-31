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
	Resources []STR     `json:"-"`
	CmdArgs   ArgChunks `json:"cmd_args"`
	Env       EnvVars   `json:"env,omitempty"`
	Cwd       ANY       `json:"cwd,omitempty"`
	Stdout    VFS       `json:"stdout,omitempty"`
}

type Node struct {
	Extra           *NodeExtra     `json:"-"`
	Cmds            []Cmd          `json:"cmds"`
	Env             EnvVars        `json:"env"`
	Inputs          InputChunks    `json:"-"`
	InputFixes      []FixID        `json:"-"`
	KV              *KV            `json:"kv"`
	KVExts          []KVExt        `json:"-"`
	Outputs         []VFS          `json:"outputs"`
	Platform        *Platform      `json:"platform"`
	Requirements    *Requirements  `json:"requirements"`
	PresetUID       *UID           `json:"-"`
	DepRefs         []NodeRef      `json:"-"`
	ForeignDepRefs  []NodeRef      `json:"-"`
	Resources       []STR          `json:"-"`
	ResourceGlobals []ResourceDecl `json:"-"`
	Ref             NodeRef        `json:"-"`
	UIDSalt         STR            `json:"-"`
	DisableCache    bool           `json:"-"`
	Module          bool           `json:"-"`
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
			if ref, ok := n.resourceRef(pat, fetchRefs); ok {
				yield(ref)
			}
		}
	}
}

func (n *Node) resourceRef(name STR, fetchRefs *DenseMap[STR, NodeRef]) (NodeRef, bool) {
	for _, decl := range n.ResourceGlobals {
		if decl.Name == name {
			return fetchRefs.get(decl.FetchKey)
		}
	}

	return fetchRefs.get(name)
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

	dedupers.with(func(deduper *DeDuper) {
		for _, r := range refs {
			if deduper.add(r.strID()) {
				out = append(out, r)
			}
		}
	})

	return out
}

type InputChunks [][]VFS

type NodeExtra struct {
	InputNames   []STR
	ResourceURIs []STR
}

func (n *Node) inputNames() []STR {
	if n.Extra != nil {
		return n.Extra.InputNames
	}

	return nil
}

func (n *Node) resourceURIs() []STR {
	if n.Extra != nil {
		return n.Extra.ResourceURIs
	}

	return nil
}
