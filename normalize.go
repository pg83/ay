package main

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// normalize.go — NormalizeGraph subcommand.
//
// Produces a normalized reference JSON from the full ymake reference graph
// (sg.json) that can be compared byte-exactly against OUR gen output after
// PR-L4-C closes the remaining gaps. This is step L4-A in the byte-exact
// roadmap.
//
// Normalization steps:
//  1. Extract the transitive closure rooted at the target's root node
//     (LD for programs, AR for libraries) via BFS over deps[].
//  2. Drop the top-level conf key.
//  3. Recompute UIDs using our canonicalNodeBytes+sha1+base64url algorithm,
//     processing in post-order DFS (leaves first) so every child's new UID
//     is known before its parent is hashed.
//  4. Rewrite all deps[], foreign_deps values, and result[] to use new UIDs.
//  5. Drop stats_uid (set to "" so omitempty-equivalent logic drops it).
//  6. Set self_uid = uid for the 101 nodes where REF has self_uid != uid.
//  7. Order graph[] in DFS preorder from result[0] with declaration-order
//     children (empirically verified to match the reference byte-exactly).
//  8. Preserve sandboxing: true from the reference (present on all nodes).
//
// The output uses our writeNormalizedGraphIndented which adds sandboxing
// between requirements and self_uid, matching the reference key order.

// NormalizedNode mirrors the on-disk reference node shape including the
// sandboxing field that our Node struct intentionally omits (the emitter
// side — adding sandboxing to gen output — is PR-L4-C's responsibility).
// This type is only used for reading sg.json and writing normalized-ref.json;
// canonicalNodeBytes uses *Node (without sandboxing) per the UID algorithm.
type NormalizedNode struct {
	Cmds             []Cmd                  `json:"cmds"`
	Deps             []string               `json:"deps"`
	Env              map[string]string      `json:"env"`
	ForeignDeps      map[string][]string    `json:"foreign_deps,omitempty"`
	HostPlatform     bool                   `json:"host_platform,omitempty"`
	Inputs           []string               `json:"inputs"`
	KV               map[string]string      `json:"kv"`
	Outputs          []string               `json:"outputs"`
	Platform         string                 `json:"platform"`
	Requirements     map[string]interface{} `json:"requirements"`
	Sandboxing       bool                   `json:"sandboxing,omitempty"`
	SelfUID          string                 `json:"self_uid"`
	StatsUID         string                 `json:"stats_uid,omitempty"`
	Tags             []string               `json:"tags"`
	TargetProperties map[string]string      `json:"target_properties"`
	UID              string                 `json:"uid"`
}

// NormalizedGraph is the deserialization target for reference graph files
// that include sandboxing. It parallels emitter.go's Graph but uses
// NormalizedNode so the sandboxing field survives the decode.
type NormalizedGraph struct {
	Conf   map[string]interface{} `json:"conf"`
	Graph  []*NormalizedNode      `json:"graph"`
	Inputs map[string]interface{} `json:"inputs"`
	Result []string               `json:"result"`
}

// loadNormalizedReference reads an sg.json and returns a *NormalizedGraph.
// Throws on IO error, malformed JSON, or empty graph/result.
func loadNormalizedReference(path string) *NormalizedGraph {
	data := Throw2(os.ReadFile(path))

	var g NormalizedGraph
	Throw(json.Unmarshal(data, &g))

	if len(g.Graph) == 0 || len(g.Result) == 0 {
		ThrowFmt("loadNormalizedReference: empty graph or result in %s", path)
	}

	return &g
}

// toNode converts a NormalizedNode to a *Node for use with canonicalNodeBytes.
// The Sandboxing field is not transferred because our UID hash intentionally
// excludes it (the UID is a hash of build semantics, and sandboxing is a
// scheduler hint added outside the Merkle tree).
func toNode(nn *NormalizedNode) *Node {
	return &Node{
		Cmds:             nn.Cmds,
		Deps:             nn.Deps,
		Env:              nn.Env,
		ForeignDeps:      nn.ForeignDeps,
		HostPlatform:     nn.HostPlatform,
		Inputs:           nn.Inputs,
		KV:               nn.KV,
		Outputs:          nn.Outputs,
		Platform:         nn.Platform,
		Requirements:     nn.Requirements,
		SelfUID:          nn.SelfUID,
		StatsUID:         nn.StatsUID,
		Tags:             nn.Tags,
		TargetProperties: nn.TargetProperties,
		UID:              nn.UID,
	}
}

// findRootNode locates the subgraph root node for target within the reference
// graph. The search follows two rules in order:
//
//  1. LD node: kv.p == "LD" AND outputs[0] ends with "/<binaryName>" where
//     binaryName is the last path component of target (e.g. "archiver" for
//     "tools/archiver").
//  2. AR node: kv.p == "AR" AND outputs[0] contains "/<target>/" — used for
//     library targets like build/cow/on that have no LD.
//
// Throws if neither search finds exactly one candidate.
func findRootNode(g *NormalizedGraph, target string) *NormalizedNode {
	// binaryName is the last component of the target path.
	binaryName := target
	if idx := strings.LastIndex(target, "/"); idx >= 0 {
		binaryName = target[idx+1:]
	}

	suffix := "/" + binaryName
	targetInPath := "/" + target + "/"

	// Pass 1: LD nodes whose outputs[0] ends with /<binaryName>.
	var ldCandidates []*NormalizedNode

	for _, node := range g.Graph {
		if node.KV["p"] != "LD" {
			continue
		}

		if len(node.Outputs) > 0 && strings.HasSuffix(node.Outputs[0], suffix) {
			ldCandidates = append(ldCandidates, node)
		}
	}

	if len(ldCandidates) == 1 {
		return ldCandidates[0]
	}

	if len(ldCandidates) > 1 {
		ThrowFmt("normalize: found %d LD nodes for target %q (outputs[0] endswith %q); expected exactly 1",
			len(ldCandidates), target, suffix)
	}

	// Pass 2: AR nodes whose outputs[0] contains /<target>/.
	// When multiple are found, prefer the target-platform node (host_platform == false)
	// over host-platform nodes — mirrors the gen output which emits target-platform first.
	var arCandidates []*NormalizedNode

	for _, node := range g.Graph {
		if node.KV["p"] != "AR" {
			continue
		}

		if len(node.Outputs) > 0 && strings.Contains(node.Outputs[0], targetInPath) {
			arCandidates = append(arCandidates, node)
		}
	}

	if len(arCandidates) == 1 {
		return arCandidates[0]
	}

	if len(arCandidates) > 1 {
		// Filter to non-host (target) platform when there are multiple AR nodes.
		var targetPlatform []*NormalizedNode

		for _, node := range arCandidates {
			if !node.HostPlatform {
				targetPlatform = append(targetPlatform, node)
			}
		}

		if len(targetPlatform) == 1 {
			return targetPlatform[0]
		}

		ThrowFmt("normalize: found %d AR nodes for target %q (outputs[0] contains %q); expected exactly 1",
			len(arCandidates), target, targetInPath)
	}

	ThrowFmt("normalize: no LD or AR root node found for target %q", target)

	return nil // unreachable
}

// extractClosure returns the set of nodes reachable from root via BFS over
// deps[], keyed by their original UID.
func extractClosure(g *NormalizedGraph, root *NormalizedNode) map[string]*NormalizedNode {
	byUID := make(map[string]*NormalizedNode, len(g.Graph))

	for _, node := range g.Graph {
		byUID[node.UID] = node
	}

	closure := make(map[string]*NormalizedNode)
	queue := []string{root.UID}

	for len(queue) > 0 {
		uid := queue[0]
		queue = queue[1:]

		if _, seen := closure[uid]; seen {
			continue
		}

		node := byUID[uid]

		if node == nil {
			// Skip UIDs that appear in foreign_deps but are not present in
			// the input graph (e.g. host-platform tool LD nodes that sg.json
			// elides from its node list while keeping their UIDs in foreign_deps).
			continue
		}

		closure[uid] = node

		for _, dep := range node.Deps {
			if _, seen := closure[dep]; !seen {
				queue = append(queue, dep)
			}
		}

		for _, vals := range node.ForeignDeps {
			for _, dep := range vals {
				if _, seen := closure[dep]; !seen {
					queue = append(queue, dep)
				}
			}
		}
	}

	return closure
}

// reUIDClosure recomputes UIDs for all nodes in closure using our
// canonicalNodeBytes+computeUID algorithm. Processes nodes in post-order
// (leaves first) so every dependency's new UID is known before a parent
// is hashed. Returns a map from old UID → new UID.
//
// Post-order is required because our UID is a Merkle hash: the hash of a
// parent node's content includes its children's UIDs in deps[], so the
// children must be hashed first.
func reUIDClosure(closure map[string]*NormalizedNode) map[string]string {
	oldToNew := make(map[string]string, len(closure))

	// Iterative post-order DFS using an explicit stack to avoid stack
	// overflow on deep dependency chains (tools/archiver has ~3730 nodes).
	// Seeds in sorted order for deterministic output.
	finished := make(map[string]bool, len(closure))
	order := make([]string, 0, len(closure))

	type frame struct {
		uid            string
		childIdx       int
		childrenSorted []string
	}

	allUIDs := make([]string, 0, len(closure))

	for uid := range closure {
		allUIDs = append(allUIDs, uid)
	}

	sort.Strings(allUIDs)

	for _, startUID := range allUIDs {
		if finished[startUID] {
			continue
		}

		stack := []frame{{uid: startUID}}

		for len(stack) > 0 {
			top := &stack[len(stack)-1]

			if top.childrenSorted == nil {
				// First visit: build children list.
				node := closure[top.uid]
				// Collect all deps in closure.
				childSet := make(map[string]struct{})

				for _, dep := range node.Deps {
					if _, inClosure := closure[dep]; inClosure {
						childSet[dep] = struct{}{}
					}
				}

				for _, vals := range node.ForeignDeps {
					for _, dep := range vals {
						if _, inClosure := closure[dep]; inClosure {
							childSet[dep] = struct{}{}
						}
					}
				}

				children := make([]string, 0, len(childSet))

				for c := range childSet {
					children = append(children, c)
				}

				sort.Strings(children)
				top.childrenSorted = children

				if len(children) == 0 {
					top.childrenSorted = []string{} // mark as processed
				}
			}

			// Find next unfinished child.
			pushed := false

			for top.childIdx < len(top.childrenSorted) {
				childUID := top.childrenSorted[top.childIdx]
				top.childIdx++

				if !finished[childUID] {
					stack = append(stack, frame{uid: childUID})
					pushed = true

					break
				}
			}

			if !pushed {
				// All children done: process this node.
				stack = stack[:len(stack)-1]

				if !finished[top.uid] {
					finished[top.uid] = true
					order = append(order, top.uid)
				}
			}
		}
	}

	// Now process in post-order: leaves first, root last.
	for _, oldUID := range order {
		node := closure[oldUID]

		// Build a *Node with children's NEW UIDs filled in.
		n := toNode(node)

		// Rewrite deps[] to new UIDs.
		if len(n.Deps) > 0 {
			newDeps := make([]string, len(n.Deps))

			for i, dep := range n.Deps {
				if nu, ok := oldToNew[dep]; ok {
					newDeps[i] = nu
				} else {
					newDeps[i] = dep // dep outside closure: keep as-is
				}
			}

			n.Deps = newDeps
		}

		// Rewrite foreign_deps values to new UIDs.
		if len(n.ForeignDeps) > 0 {
			newFD := make(map[string][]string, len(n.ForeignDeps))

			for k, vals := range n.ForeignDeps {
				newVals := make([]string, len(vals))

				for i, dep := range vals {
					if nu, ok := oldToNew[dep]; ok {
						newVals[i] = nu
					} else {
						newVals[i] = dep
					}
				}

				newFD[k] = newVals
			}

			n.ForeignDeps = newFD
		}

		// Clear identity fields so hash covers content only.
		n.UID = ""
		n.SelfUID = ""
		n.StatsUID = ""

		canon := canonicalNodeBytes(n)
		newUID := computeUID(canon)
		oldToNew[oldUID] = newUID
	}

	return oldToNew
}

// dfsPreorder returns node UIDs in DFS preorder from startUID, visiting
// deps[] in their declaration order. Only nodes in `closure` are visited.
// This ordering matches the reference graph[] array order (empirically
// verified: DFS preorder from result[0] with declaration-order children
// reproduces sg.json's graph[] array exactly).
func dfsPreorder(startUID string, closure map[string]*NormalizedNode) []string {
	order := make([]string, 0, len(closure))
	visited := make(map[string]bool, len(closure))

	var dfs func(uid string)

	dfs = func(uid string) {
		if visited[uid] {
			return
		}

		visited[uid] = true
		order = append(order, uid)
		node := closure[uid]

		for _, dep := range node.Deps {
			if _, inClosure := closure[dep]; inClosure {
				dfs(dep)
			}
		}
	}

	dfs(startUID)

	return order
}

// normalizeGraph extracts and normalizes the subgraph for target from g.
// Returns the ordered slice of normalized nodes (DFS preorder) and the
// result UID slice (new UIDs for the original result[] entries).
func normalizeGraph(g *NormalizedGraph, target string) ([]*NormalizedNode, []string) {
	root := findRootNode(g, target)

	closure := extractClosure(g, root)

	oldToNew := reUIDClosure(closure)

	// DFS preorder ordering using the OLD UIDs (the closure map is keyed by
	// old UIDs, and deps[] still hold old UIDs at this point).
	preorder := dfsPreorder(root.UID, closure)

	// Build normalized nodes in DFS preorder.
	nodes := make([]*NormalizedNode, 0, len(preorder))

	for _, oldUID := range preorder {
		node := closure[oldUID]
		newUID := oldToNew[oldUID]

		// Build the output node with new UIDs.
		out := &NormalizedNode{
			Cmds:             node.Cmds,
			Env:              node.Env,
			HostPlatform:     node.HostPlatform,
			Inputs:           node.Inputs,
			KV:               node.KV,
			Outputs:          node.Outputs,
			Platform:         node.Platform,
			Requirements:     node.Requirements,
			Sandboxing:       node.Sandboxing,
			Tags:             node.Tags,
			TargetProperties: node.TargetProperties,
			UID:              newUID,
			SelfUID:          newUID, // normalize: self_uid := uid (step 6)
			StatsUID:         "",     // drop stats_uid (step 5)
		}

		// Rewrite deps[] to new UIDs.
		if len(node.Deps) > 0 {
			newDeps := make([]string, len(node.Deps))

			for i, dep := range node.Deps {
				if nu, ok := oldToNew[dep]; ok {
					newDeps[i] = nu
				} else {
					newDeps[i] = dep
				}
			}

			out.Deps = newDeps
		} else {
			out.Deps = node.Deps
		}

		// Rewrite foreign_deps values to new UIDs.
		if len(node.ForeignDeps) > 0 {
			newFD := make(map[string][]string, len(node.ForeignDeps))

			for k, vals := range node.ForeignDeps {
				newVals := make([]string, len(vals))

				for i, dep := range vals {
					if nu, ok := oldToNew[dep]; ok {
						newVals[i] = nu
					} else {
						newVals[i] = dep
					}
				}

				newFD[k] = newVals
			}

			out.ForeignDeps = newFD
		}

		nodes = append(nodes, out)
	}

	// Build result[] using new UIDs for all original result entries that are
	// in the closure. If none of the original result UIDs fall in the closure
	// (e.g. for a library sub-target like build/cow/on whose root is an AR
	// while the full sg.json result[] points at the LD of tools/archiver),
	// fall back to using the root node as the sole result.
	result := make([]string, 0, len(g.Result))

	for _, oldUID := range g.Result {
		if _, inClosure := closure[oldUID]; inClosure {
			result = append(result, oldToNew[oldUID])
		}
	}

	if len(result) == 0 {
		// Root node is not listed in g.Result; use it as the result.
		result = append(result, oldToNew[root.UID])
	}

	return nodes, result
}

// writeNormalizedGraphIndented writes the normalized graph as indented JSON
// to w. Field order matches the reference sg.json node key order:
//
//	cmds, deps, env, [foreign_deps], [host_platform], inputs, kv, outputs,
//	platform, requirements, [sandboxing], self_uid, tags, target_properties, uid
//
// stats_uid is intentionally omitted (step 5 of normalization).
// Trailing newline is appended (matching json.Encoder.Encode behaviour).
func writeNormalizedGraphIndented(w *bufio.Writer, nodes []*NormalizedNode, result []string) {
	buf := make([]byte, 0, 1<<20)

	buf = append(buf, '{', '\n')

	// "graph": [ ...nodes... ]
	buf = append(buf, `  "graph": `...)

	if len(nodes) == 0 {
		buf = append(buf, '[', ']', ',', '\n')
	} else {
		buf = append(buf, '[', '\n')

		for i, node := range nodes {
			buf = appendNormalizedNode(buf, node, "    ")

			if i < len(nodes)-1 {
				buf = append(buf, ',')
			}

			buf = append(buf, '\n')

			if len(buf) >= 256<<10 {
				Throw2(w.Write(buf))
				buf = buf[:0]
			}
		}

		buf = append(buf, `  ],`...)
		buf = append(buf, '\n')
	}

	// "inputs": {} — matches reference shape (conf is dropped per step 2).
	buf = append(buf, `  "inputs": {},`...)
	buf = append(buf, '\n')

	// "result": [ ...uids... ]
	buf = append(buf, `  "result": `...)
	buf = appendStringSlice(buf, result, "  ")
	buf = append(buf, '\n')

	buf = append(buf, '}', '\n')

	Throw2(w.Write(buf))
}

// appendNormalizedNode emits a NormalizedNode as a JSON object. Field order
// matches sg.json's alphabetical key order, with sandboxing preserved and
// stats_uid omitted.
func appendNormalizedNode(buf []byte, n *NormalizedNode, pad string) []byte {
	innerPad := pad + "  "
	buf = append(buf, pad...)
	buf = append(buf, '{', '\n')

	// cmds: []Cmd
	buf = append(buf, innerPad...)
	buf = append(buf, `"cmds": `...)
	buf = appendCmdSlice(buf, n.Cmds, innerPad)
	buf = append(buf, ',', '\n')

	// deps: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"deps": `...)
	buf = appendStringSlice(buf, n.Deps, innerPad)
	buf = append(buf, ',', '\n')

	// env: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"env": `...)
	buf = appendStringMap(buf, n.Env, innerPad)
	buf = append(buf, ',', '\n')

	// foreign_deps: map[string][]string, omitempty
	if len(n.ForeignDeps) > 0 {
		buf = append(buf, innerPad...)
		buf = append(buf, `"foreign_deps": `...)
		buf = appendStringSliceMap(buf, n.ForeignDeps, innerPad)
		buf = append(buf, ',', '\n')
	}

	// host_platform: bool, omitempty
	if n.HostPlatform {
		buf = append(buf, innerPad...)
		buf = append(buf, `"host_platform": true,`...)
		buf = append(buf, '\n')
	}

	// inputs: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"inputs": `...)
	buf = appendStringSlice(buf, n.Inputs, innerPad)
	buf = append(buf, ',', '\n')

	// kv: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"kv": `...)
	buf = appendStringMap(buf, n.KV, innerPad)
	buf = append(buf, ',', '\n')

	// outputs: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"outputs": `...)
	buf = appendStringSlice(buf, n.Outputs, innerPad)
	buf = append(buf, ',', '\n')

	// platform: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"platform": `...)
	buf = appendString(buf, n.Platform)
	buf = append(buf, ',', '\n')

	// requirements: map[string]interface{}
	buf = append(buf, innerPad...)
	buf = append(buf, `"requirements": `...)
	buf = appendInterfaceMap(buf, n.Requirements, innerPad)
	buf = append(buf, ',', '\n')

	// sandboxing: bool, omitempty
	if n.Sandboxing {
		buf = append(buf, innerPad...)
		buf = append(buf, `"sandboxing": true,`...)
		buf = append(buf, '\n')
	}

	// self_uid: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"self_uid": `...)
	buf = appendString(buf, n.SelfUID)
	buf = append(buf, ',', '\n')

	// stats_uid is intentionally omitted (normalization step 5).

	// tags: []string
	buf = append(buf, innerPad...)
	buf = append(buf, `"tags": `...)
	buf = appendStringSlice(buf, n.Tags, innerPad)
	buf = append(buf, ',', '\n')

	// target_properties: map[string]string
	buf = append(buf, innerPad...)
	buf = append(buf, `"target_properties": `...)
	buf = appendStringMap(buf, n.TargetProperties, innerPad)
	buf = append(buf, ',', '\n')

	// uid: string
	buf = append(buf, innerPad...)
	buf = append(buf, `"uid": `...)
	buf = appendString(buf, n.UID)
	buf = append(buf, '\n')

	buf = append(buf, pad...)
	buf = append(buf, '}')

	return buf
}

// cmdNormalize is the entry point for the `normalize` subcommand.
func cmdNormalize(args []string) int {
	// Use manual argument parsing mirroring the gen/compare/inspect pattern.
	var target, inPath, outPath string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--target":
			i++

			if i >= len(args) {
				ThrowFmt("normalize: --target requires an argument")
			}

			target = args[i]

		case "--in":
			i++

			if i >= len(args) {
				ThrowFmt("normalize: --in requires an argument")
			}

			inPath = args[i]

		case "--out":
			i++

			if i >= len(args) {
				ThrowFmt("normalize: --out requires an argument")
			}

			outPath = args[i]

		default:
			ThrowFmt("normalize: unknown flag %q", args[i])
		}
	}

	if target == "" {
		ThrowFmt("normalize: --target is required")
	}

	if inPath == "" {
		ThrowFmt("normalize: --in is required")
	}

	if outPath == "" {
		ThrowFmt("normalize: --out is required")
	}

	g := loadNormalizedReference(inPath)
	nodes, result := normalizeGraph(g, target)

	f := Throw2(os.Create(outPath))

	defer func() {
		Throw(f.Close())
	}()

	bw := bufio.NewWriterSize(f, 1<<20)
	writeNormalizedGraphIndented(bw, nodes, result)
	Throw(bw.Flush())

	return 0
}
