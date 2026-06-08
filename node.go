package main

type Cmd struct {
	CmdArgs []STR   `json:"cmd_args"`
	Cwd     STR     `json:"cwd,omitempty"`
	Env     EnvVars `json:"env,omitempty"`
	Stdout  string  `json:"stdout,omitempty"`
}

type Node struct {
	Cache            *bool            `json:"cache,omitempty"`
	Cmds             []Cmd            `json:"cmds"`
	Env              EnvVars          `json:"env"`
	Inputs           []VFS            `json:"inputs"`
	KV               KV               `json:"kv"`
	Outputs          []VFS            `json:"outputs"`
	Platform         *Platform        `json:"platform"`
	Requirements     Requirements     `json:"requirements"`
	Sandboxing       bool             `json:"sandboxing"`
	SelfUID          UID              `json:"self_uid"`
	StatsUID         string           `json:"stats_uid"`
	Tags             []string         `json:"tags"`
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

func nodeHasHostTag(tags []string) bool {
	for _, t := range tags {
		if t == "tool" {
			return true
		}
	}

	return false
}

func bindNodePlatform(n *Node, p *Platform) *Node {
	if n == nil || p == nil {
		return n
	}

	n.Platform = p

	return n
}

// platformTarget is the node's platform target string for the graph output and
// UID — "" for an unbound node (Platform nil), matching the former string field's
// zero value.
func platformTarget(p *Platform) string {
	if p == nil {
		return ""
	}

	return string(p.Target)
}
