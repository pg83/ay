package main

type Cmd struct {
	CmdArgs []STR   `json:"cmd_args"`
	Cwd     STR     `json:"cwd,omitempty"`
	Env     EnvVars `json:"env,omitempty"`
	Stdout  string  `json:"stdout,omitempty"`
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

	// usesResources lists the fetched-resource patterns (bare names: CLANG,
	// YMAKE_PYTHON3, LLD_ROOT, …) this node's command references. Builders declare
	// it at the point they splice a $(PATTERN) tool path into cmd_args; the
	// resource emitter turns each into a dependency on that resource's fetch node.
	usesResources []string `json:"-"`
}

// withResources declares the fetched-resource patterns a node's command uses, at
// the site that builds the command. The resource emitter turns each into a
// dependency on that resource's fetch node (no command scanning).
func withResources(n *Node, patterns ...string) *Node {
	n.usesResources = append(n.usesResources, patterns...)

	return n
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
