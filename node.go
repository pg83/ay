package main

type Cmd struct {
	CmdArgs []STR   `json:"cmd_args"`
	Cwd     STR     `json:"cwd,omitempty"`
	Env     EnvVars `json:"env,omitempty"`
	Stdout  STR     `json:"stdout,omitempty"`
}

type Node struct {
	Cache        *bool        `json:"cache,omitempty"`
	Cmds         []Cmd        `json:"cmds"`
	Env          EnvVars      `json:"env"`
	Inputs       []VFS        `json:"inputs"`
	KV           KV           `json:"kv"`
	Outputs      []VFS        `json:"outputs"`
	Platform     *Platform    `json:"platform"`
	Requirements Requirements `json:"requirements"`
	Sandboxing   bool         `json:"sandboxing"`
	SelfUID      UID          `json:"self_uid"`
	// Tags is nil for almost every node — its tags are the platform's (nodeTags).
	// The exception is the tagless test/lint run nodes, which set it to the
	// platform's TestTags so they render their own ("no") tags, not the platform's.
	Tags             []STR            `json:"tags"`
	TargetProperties TargetProperties `json:"target_properties"`
	UID              UID              `json:"uid"`

	DepRefs        []NodeRef `json:"-"`
	ForeignDepRefs []NodeRef `json:"-"`

	// usesResources lists the fetched-resource names (CLANG20, YMAKE_PYTHON3,
	// LLD_ROOT, …) whose tool the node's command invokes via $(B)/resources/<NAME>.
	// Builders set it in the &Node{} literal alongside that tool path; the resource
	// emitter turns each into a dependency on that resource's FETCH node.
	usesResources []string `json:"-"`
}

func nodeHasHostTag(tags []STR) bool {
	for _, t := range tags {
		if t == strTool {
			return true
		}
	}

	return false
}

// nodeTags is a node's effective tag list for the graph output and UID: its own
// Tags when set (the special tagless test/lint nodes carry their Platform.TestTags
// there), otherwise the platform's Tags.
func nodeTags(n *Node) []STR {
	if n.Tags != nil {
		return n.Tags
	}

	return n.Platform.Tags
}
