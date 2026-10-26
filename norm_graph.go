//go:build aynorm

package main

import (
	"bufio"
	"os"
	"sort"
	"sync"

	json "github.com/goccy/go-json"
)

func normRec(v any) any {
	switch t := v.(type) {
	case string:
		return normPath(t)
	case []any:
		for i := range t {
			t[i] = normRec(t[i])
		}

		return t
	case map[string]any:
		for k := range t {
			t[k] = normRec(t[k])
		}

		return t
	default:
		return v
	}
}

func normSortedStrings(in []string) []string {
	out := make([]string, len(in))

	for i := range in {
		out[i] = normPath(in[i])
	}

	sort.Strings(out)

	w := 0

	for i := range out {
		if i == 0 || out[i] != out[w-1] {
			out[w] = out[i]
			w++
		}
	}

	return out[:w]
}

func normStringsKeepOrder(in []string) []string {
	out := make([]string, len(in))

	for i := range in {
		out[i] = normPath(in[i])
	}

	return out
}

func orVal(v, def any) any {
	if v == nil {
		return def
	}

	return v
}

func canonContent(node *RawNode) map[string]any {
	inputs := normSortedStrings(node.Inputs)

	canon := map[string]any{
		"cmds":         normRec(orVal(node.Cmds, []any{})),
		"env":          normRec(orVal(node.Env, map[string]any{})),
		"inputs":       inputs,
		"kv":           normRec(orVal(node.Kv, map[string]any{})),
		"outputs":      normStringsKeepOrder(node.Outputs),
		"platform":     normPath(node.Platform),
		"requirements": normRec(orVal(node.Requirements, map[string]any{})),
	}

	return canon
}

type NodeID string

func (k *NodeID) UnmarshalJSON(b []byte) error {
	return k.unmarshalJSON(b)
}

func (k *NodeID) unmarshalJSON(b []byte) error {
	if len(b) >= 2 && b[0] == '"' {
		*k = NodeID(b[1 : len(b)-1])
	} else {
		*k = NodeID(b)
	}

	return nil
}

func idStrs(ids []NodeID) []string {
	out := make([]string, len(ids))

	for i, id := range ids {
		out[i] = string(id)
	}

	return out
}

type RawNode struct {
	UID          NodeID   `json:"uid"`
	Deps         []NodeID `json:"deps"`
	Inputs       []string `json:"inputs"`
	Outputs      []string `json:"outputs"`
	Platform     string   `json:"platform"`
	Cmds         any      `json:"cmds"`
	Env          any      `json:"env"`
	Kv           any      `json:"kv"`
	Requirements any      `json:"requirements"`
}

func graphSize(file string) int64 {
	return throw2(os.Stat(file)).Size()
}

func loadGraph(path string) ([]*RawNode, []NodeID) {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))

	dec.UseNumber()
	seekToGraph(dec, path)

	nodes := []*RawNode{}

	for dec.More() {
		node := &RawNode{}

		throw(dec.Decode(node))
		nodes = append(nodes, node)
	}

	return nodes, readGraphResult(dec, path)
}

type NodeSource struct {
	path   string
	nodes  []*RawNode
	result []NodeID
}

func fanoutNodes[R any](src NodeSource, workers int, process func(*RawNode) R, collect func(R)) []NodeID {
	if src.nodes != nil {
		sliceFanout(src.nodes, workers, process, collect)

		return src.result
	}

	return streamGraphFanout(src.path, workers, process, collect)
}

func sliceFanout[R any](nodes []*RawNode, workers int, process func(*RawNode) R, collect func(R)) {
	in := make(chan *RawNode, workers*2)
	results := make(chan R, workers*2)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for n := range in {
				results <- process(n)
			}
		}()
	}

	done := make(chan struct{})

	go func() {
		for r := range results {
			collect(r)
		}

		close(done)
	}()

	for _, n := range nodes {
		in <- n
	}

	close(in)
	wg.Wait()
	close(results)
	<-done
}

func streamGraphFanout[R any](path string, workers int, process func(*RawNode) R, collect func(R)) []NodeID {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))

	dec.UseNumber()
	seekToGraph(dec, path)

	nodes := make(chan *RawNode, workers*2)
	results := make(chan R, workers*2)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for n := range nodes {
				results <- process(n)
			}
		}()
	}

	done := make(chan struct{})

	go func() {
		for r := range results {
			collect(r)
		}

		close(done)
	}()

	for dec.More() {
		node := &RawNode{}

		throw(dec.Decode(node))
		nodes <- node
	}

	close(nodes)
	wg.Wait()
	close(results)
	<-done

	return readGraphResult(dec, path)
}

func readGraphResult(dec *json.Decoder, path string) []NodeID {
	throw2(dec.Token())

	var result []NodeID

	for dec.More() {
		key, ok := throw2(dec.Token()).(string)

		if !ok {
			throwFmt("dump: %s: expected object key", path)
		}

		if key == "result" {
			throw(dec.Decode(&result))

			continue
		}

		var skip json.RawMessage
		throw(dec.Decode(&skip))
	}

	return result
}
