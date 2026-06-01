package main

type Cmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Stdout  string            `json:"stdout,omitempty"`
}

type Node struct {
	Cache            *bool                  `json:"cache,omitempty"`
	Cmds             []Cmd                  `json:"cmds"`
	Env              map[string]string      `json:"env"`
	Inputs           []VFS                  `json:"inputs"`
	KV               map[string]interface{} `json:"kv"`
	Outputs          []VFS                  `json:"outputs"`
	Platform         string                 `json:"platform"`
	Requirements     map[string]interface{} `json:"requirements"`
	Sandboxing       bool                   `json:"sandboxing"`
	SelfUID          UID                    `json:"self_uid"`
	StatsUID         string                 `json:"stats_uid"`
	Tags             []string               `json:"tags"`
	TargetProperties map[string]string      `json:"target_properties"`
	UID              UID                    `json:"uid"`

	StatsTags []string `json:"-"`

	DepRefs        []NodeRef `json:"-"`
	ForeignDepRefs []NodeRef `json:"-"`
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

	n.Platform = string(p.Target)
	n.StatsTags = statsTagsForPlatform(p)

	return n
}
