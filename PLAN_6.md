# PLAN 6: Stream graph JSON for `make -j 0 -G`

## Goal

Reduce peak RSS after scanner hot paths are improved.

Current sg3 facts:

- graph JSON output size: 460 MB
- max RSS: 1.46 GB
- `writeGraph`: 0.85s cumulative CPU

JSON writing is not the main CPU problem, but materializing the whole graph contributes to memory pressure.

## Current Shape

For `-j 0 -G`, `cmdMake` does:

```go
g := GenWithModeWithResources(...)
applyGraphConf(g, conf)
writeGraph("-", g)
```

This builds a full `Graph` object before writing.

There is already a streaming path for execution:

```go
genStream(...)
```

But graph dump still uses full materialization.

## Proposed Model

Add a streaming graph writer for `-j 0 -G`:

- consume finalized nodes from `NewStreamingEmitter`
- write JSON incrementally in stable order
- append graph conf fields at the correct location
- avoid retaining all `*Node` objects when only JSON output is needed

This should happen after scanner optimizations, because scanner currently dominates CPU and heap allocation. Streaming JSON is the last memory cleanup step.

## Implementation Steps

1. Read `gjson_write.go` and identify required output shape.

2. Read `Graph` type in `node.go` or related files.

3. Add a writer API:

```go
type GraphStreamWriter struct { ... }
func NewGraphStreamWriter(w io.Writer, conf GraphConf) *GraphStreamWriter
func (w *GraphStreamWriter) OnNode(n *Node)
func (w *GraphStreamWriter) Finish(rootUIDs []string)
```

Exact names should follow local style after reading the code.

4. Wire `cmdMake` `-j 0 -G` path to use streaming writer.

5. Preserve existing `writeGraph` for tests and non-streaming callers until equivalence is proven.

6. Add byte-equivalence test:

- generate a small graph with existing full writer
- generate same graph with streaming writer
- compare bytes exactly

7. Run `go test ./...`.

8. Run sg2/sg3 validation.

## Correctness Checks

- Byte-exact JSON format for small fixture
- `normalize.py` equality for `sg2.aarch64`, `sg2.x86_64`
- no root UID ordering changes
- no node ordering changes

## Expected Impact

Peak RSS should drop after scanner caches are fixed. CPU impact should be small because `writeGraph` is currently only ~0.85s.

## Risks

- JSON ordering is part of normalized diff stability. Streaming writer must preserve exactly the same order as current `writeGraphIndented`.
- `applyGraphConf` currently mutates a full `Graph`; streaming path must reproduce those fields without changing output.
- This is not the first optimization to implement because it does not address the current CPU bottleneck.
