package main

import "encoding/json"

// argChunks is a command's chunked arg list. Like inputChunks it JSON-marshals
// FLAT — the chunking is an internal layout (zero-copy assembly from shared,
// pre-built blocks: a platform flag block, a module -I block, a per-source
// tail), not schema; uid hashing and the json writer emit the flattened
// element sequence.
type argChunks [][]STR

func (c argChunks) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.flat())
}

func (c argChunks) flat() []STR {
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
	CmdArgs argChunks `json:"cmd_args"`
	Cwd     STR       `json:"cwd,omitempty"`
	Env     EnvVars   `json:"env,omitempty"`
	Stdout  STR       `json:"stdout,omitempty"`
}

type Node struct {
	Cache *bool   `json:"cache,omitempty"`
	Cmds  []Cmd   `json:"cmds"`
	Env   EnvVars `json:"env"`
	// Inputs holds the node's input paths as a list of chunks: emitters hand over
	// their natural pieces ([1]{src}, the shared include closure, a tool tail)
	// WITHOUT flattening, so a large closure slice is referenced, never copied.
	// Consumers (uid, json writer, executor) iterate the chunks in order; the
	// flattened element sequence is the node's input list.
	Inputs       inputChunks  `json:"inputs"`
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

// inputChunks is the chunked input list. It JSON-marshals FLAT — the chunking
// is an internal layout (zero-copy assembly from shared slices), not schema;
// the hand-rolled writer (appendVFSChunks) emits the same flat array.
type inputChunks [][]VFS

func (c inputChunks) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.flat())
}

func (c inputChunks) flat() []VFS {
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

// flatInputs flattens the input chunks into one slice — for cold consumers
// (the PR output-inputs registry, tests); hot consumers iterate the chunks.
func (n *Node) flatInputs() []VFS {
	return n.Inputs.flat()
}

// srcChunk wraps a single VFS as an input chunk — the [1]{src} head of a CC
// node's chunked inputs.
func srcChunk(v VFS) []VFS {
	return []VFS{v}
}
