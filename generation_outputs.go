package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"html"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
)

const ucBlockSize = 16 << 20

type GenerationOutputs struct {
	profile string
	stages  string
	context string
	graph   string
	log     string
	evlog   string
	html    string
}

type ConfigureError struct {
	Message string
	Row     int
	Column  int
}

type ConfigureErrors struct {
	mu    sync.Mutex
	byDir map[string]map[ConfigureError]bool
	log   []string
}

func newConfigureErrors() *ConfigureErrors {
	return &ConfigureErrors{byDir: map[string]map[ConfigureError]bool{}}
}

func (c *ConfigureErrors) add(where string, e ConfigureError) {
	c.mu.Lock()

	defer c.mu.Unlock()

	dir := configureErrorDir(where)

	if c.byDir[dir] == nil {
		c.byDir[dir] = map[ConfigureError]bool{}
	}

	c.byDir[dir][e] = true
}

func (c *ConfigureErrors) addLog(line string) {
	c.mu.Lock()

	defer c.mu.Unlock()

	c.log = append(c.log, line)
}

func configureErrorDir(where string) string {
	switch {
	case strings.HasPrefix(where, "$S/"):
		return strings.ReplaceAll(strings.ReplaceAll(where, "$S/", ""), "/ya.make", "")
	case strings.HasPrefix(where, "$B/"):
		dir := where[:max(strings.LastIndexByte(where, '/'), 0)]

		return strings.ReplaceAll(dir, "$B/", "")
	default:
		return where
	}
}

func (c *ConfigureErrors) sorted() map[string][][3]any {
	out := make(map[string][][3]any, len(c.byDir))

	for dir, set := range c.byDir {
		errs := make([]ConfigureError, 0, len(set))

		for e := range set {
			errs = append(errs, e)
		}

		slices.SortFunc(errs, func(a, b ConfigureError) int {
			if a.Message != b.Message {
				return strings.Compare(a.Message, b.Message)
			}

			if a.Row != b.Row {
				return a.Row - b.Row
			}

			return a.Column - b.Column
		})

		triples := make([][3]any, len(errs))

		for i, e := range errs {
			triples[i] = [3]any{e.Message, e.Row, e.Column}
		}

		out[dir] = triples
	}

	return out
}

type MakeFileEntry struct {
	Path   string
	Owners string
}

func (e MakeFileEntry) MarshalJSON() ([]byte, error) {
	if e.Owners == "" {
		return json.Marshal(map[string]string{"PATH": e.Path})
	}

	return json.Marshal(map[string]string{"OWNERS": e.Owners, "PATH": e.Path})
}

func writeGenerationOutputs(o GenerationOutputs, g *Graph, dropSrcInputs bool, errs *ConfigureErrors, makeFiles []MakeFileEntry) {
	if len(g.Graph) == 0 {
		makeFiles = nil
	}

	writeOutputFile(o.graph, func(w io.Writer) {
		writeGraphCompact(w, g, dropSrcInputs)
	})

	writeOutputFile(o.context, func(w io.Writer) {
		writeContextJSON(w, errs.sorted(), makeFiles)
	})

	writeOutputFile(o.profile, func(w io.Writer) { io.WriteString(w, "{}\n") })
	writeOutputFile(o.stages, func(w io.Writer) { io.WriteString(w, "{}\n") })

	text := strings.Join(errs.log, "\n")

	if text != "" {
		text += "\n"
	}

	writeOutputFile(o.log, func(w io.Writer) { io.WriteString(w, text) })

	writeOutputFile(o.html, func(w io.Writer) {
		io.WriteString(w, `<pre style="white-space:pre-wrap;overflow: auto;width:auto;"><code>`+html.EscapeString(text)+"</code></pre>\n")
	})

	writeOutputFile(o.evlog, func(w io.Writer) { writeConfigureEvlog(w, errs) })
}

func writeContextJSON(w io.Writer, errs map[string][][3]any, makeFiles []MakeFileEntry) {
	owners := map[string]any{}

	for _, entry := range makeFiles {
		if entry.Owners != "" && (entry.Path == "$S" || strings.HasPrefix(entry.Path, "$S/")) {
			addContextOwner(owners, entry.Path[min(len(entry.Path), 3):], strings.Fields(entry.Owners))
		}
	}

	if makeFiles == nil {
		makeFiles = []MakeFileEntry{}
	}

	enc := json.NewEncoder(w)

	enc.SetEscapeHTML(false)

	throw(enc.Encode(struct {
		ConfigureErrors map[string][][3]any `json:"configure_errors"`
		Tests           map[string]string   `json:"tests"`
		StrippedTests   map[string]string   `json:"stripped_tests"`
		MakeFiles       []MakeFileEntry     `json:"make_files"`
		Owners          map[string]any      `json:"owners"`
	}{errs, map[string]string{}, map[string]string{}, makeFiles, owners}))
}

func addContextOwner(tree map[string]any, path string, owners []string) {
	logins, groups := []string{}, []string{}

	for _, owner := range owners {
		if group, ok := strings.CutPrefix(owner, "rb:"); ok {
			groups = append(groups, group)
		} else if group, ok := strings.CutPrefix(owner, "g:"); ok {
			groups = append(groups, group)
		} else {
			logins = append(logins, owner)
		}
	}

	if len(logins) == 0 && len(groups) == 0 {
		return
	}

	node := tree

	for _, part := range strings.Split(path, "/") {
		if part == "" {
			continue
		}

		items, _ := node["items"].(map[string]any)

		if items == nil {
			items = map[string]any{}
			node["items"] = items
		}

		child, _ := items[part].(map[string]any)

		if child == nil {
			child = map[string]any{"items": map[string]any{}, "owners": map[string]any{"logins": []string{}, "groups": []string{}}}
			items[part] = child
		}

		node = child
	}

	node["owners"] = map[string]any{"logins": logins, "groups": groups}
}

func writeConfigureEvlog(w io.Writer, errs *ConfigureErrors) {
	enc := json.NewEncoder(w)

	enc.SetEscapeHTML(false)

	now := float64(time.Now().UnixNano()) / 1e9

	for dir, triples := range errs.sorted() {
		for _, t := range triples {
			throw(enc.Encode(map[string]any{
				"timestamp": now, "namespace": "ymake", "event": "NEvent.TDisplayMessage",
				"value": map[string]any{"Type": "Error", "Where": dir, "Message": t[0], "Row": t[1], "Column": t[2]},
			}))
		}
	}
}

func writeOutputFile(path string, write func(io.Writer)) {
	if path == "" {
		return
	}

	f := throw2(os.Create(path))
	buffered := bufio.NewWriterSize(f, 1<<20)

	switch {
	case strings.HasSuffix(path, ".uc"):
		uc := newUCWriter(buffered)

		write(uc)
		uc.close()
	case strings.HasSuffix(path, ".zst"):
		var data bytes.Buffer

		write(&data)

		enc := throw2(zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithZeroFrames(true)))

		throw2(buffered.Write(enc.EncodeAll(data.Bytes(), nil)))
		throw(enc.Close())
	default:
		write(buffered)
	}

	throw(buffered.Flush())
	throw(f.Close())
}

type ucWriter struct {
	w     io.Writer
	enc   *zstd.Encoder
	block []byte
	frame []byte
}

func newUCWriter(w io.Writer) *ucWriter {
	uc := &ucWriter{w: w, enc: throw2(zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithZeroFrames(true)))}

	uc.record([]byte("{\"codec\": \"zstd_1\"}\n"))

	return uc
}

func (uc *ucWriter) Write(p []byte) (int, error) {
	n := len(p)

	for len(p) > 0 {
		take := min(len(p), ucBlockSize-len(uc.block))

		uc.block = append(uc.block, p[:take]...)
		p = p[take:]

		if len(uc.block) == ucBlockSize {
			uc.flushBlock()
		}
	}

	return n, nil
}

func (uc *ucWriter) flushBlock() {
	uc.frame = binary.LittleEndian.AppendUint64(uc.frame[:0], uint64(len(uc.block)))
	uc.frame = uc.enc.EncodeAll(uc.block, uc.frame)
	uc.record(uc.frame)
	uc.block = uc.block[:0]
}

func (uc *ucWriter) record(data []byte) {
	var size [4]byte

	binary.LittleEndian.PutUint32(size[:], uint32(len(data)))
	throw2(uc.w.Write(size[:]))
	throw2(uc.w.Write(data))
}

func (uc *ucWriter) close() {
	if len(uc.block) > 0 {
		uc.flushBlock()
	}

	uc.flushBlock()
	throw(uc.enc.Close())
}
