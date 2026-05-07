package main

import (
	"encoding/json"
	"os"
)

// gjson.go — reader for on-disk reference graphs in the g.json format.
//
// The reference graph (~27 MB, ~3,730 nodes) is small enough to slurp
// whole and decode in one pass; per D4 we do not pull in goccy/simdjson
// until profiling actually demands it. The decoded *Graph reuses the
// types defined in emitter.go and node.go: the JSON-tagged fields on
// *Node are populated, and the internal *Refs fields stay nil — that
// is exactly correct for an already-finalized reference graph, where
// the on-disk UIDs are the truth and no Merkle pass is needed.
//
// Conf and Inputs are typed map[string]interface{} so nested JSON
// shapes decode without a schema; D15 keeps them out of scope for the
// comparator, so callers should treat them as opaque blobs.

// LoadReference reads a g.json-shape file and returns a *Graph populated
// with all Nodes and result UIDs. Throws on IO error, malformed JSON,
// or a graph that decodes to zero nodes / zero results.
func LoadReference(path string) *Graph {
	data := Throw2(os.ReadFile(path))

	var g Graph
	Throw(json.Unmarshal(data, &g))

	if len(g.Graph) == 0 || len(g.Result) == 0 {
		ThrowFmt("LoadReference: empty graph or result in %s", path)
	}

	return &g
}
