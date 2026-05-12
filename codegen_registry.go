package main

// codegen_registry.go — per-scanner registry of codegen-emitted file metadata.
//
// # Motivation
//
// The C/C++ include scanner resolves `#include` directives by walking the
// source tree. Generated files (`.pb.h`, `*_serialized.h`, `.rl6`-derived
// outputs, configure_file outputs, etc.) do not exist on disk at `gen` time,
// so the scanner cannot find them. The CodegenRegistry closes this gap by
// providing a flat `buildRootPath → producerInfo` map that the scanner can
// consult as a third existence tier.
//
// # Architecture (per-scanner, not genCtx-level)
//
// User arbitration confirmed 2026-05-11: one CodegenRegistry per
// IncludeScanner (target and host each get their own instance), NOT a single
// shared map on genCtx. Rationale: target and host walks can both produce
// `$(BUILD_ROOT)/<path>` outputs with the same filename but potentially
// different content (e.g. protobuf compiled for both axes); per-scanner keeps
// platform separation clean and mirrors the existing per-scanner cache
// architecture (resolveCache, sysincl, etc.).
//
// See docs/drafts/20260511-1700-codegen-include-registry.md §4.5 and §8
// (PR-M3-F-7-A) for full rationale.
//
// # Uniqueness invariant
//
// Upstream ymake enforces that no two nodes produce a file with the same
// `$(BUILD_ROOT)` output path within a build (DupSrc diagnostic at
// macro_processor.cpp:957). Register() asserts this invariant: a second write
// for the same key throws. If it ever fires, the build configuration is
// malformed and that is a separate defect to triage — fail-fast is the right
// response here.
//
// # Determinism
//
// All() returns entries sorted by OutputPath. The internal map is keyed by
// output path, but iteration order is only exposed via the sorted accessor, so
// no map-iteration order leaks into the output.

import "sort"

// GeneratedFileInfo describes one codegen-emitted file. Populated by each
// codegen emitter (EN, PB, EV, R5, R6, CF, BI, JV, PR, AR, PY) during the
// emit walk. F-7-B fills EmitsIncludes per emitter; F-7-A leaves it empty
// for all entries.
//
// PR-M3-L0-cascade-close-v2: ProducerRef captures the producer NodeRef so
// resolveCodegenDepRefs can thread it into consumer CC `deps[]`. Some
// emitters register the entry BEFORE the producer NodeRef is known (e.g.
// CP/PR/EN register their output paths early so the scanner's existence
// tier sees them, then the actual NodeRef is filled in via SetProducerRef
// once Emit returns). HasProducerRef discriminates "registered, no ref
// yet" from "registered, ref valid" — NodeRef{} (id=0) collides with the
// first emitted node so the explicit flag is mandatory.
type GeneratedFileInfo struct {
	// ProducerKvP is the node-kind key ("EN", "PB", "EV", "R5", "R6", "CF",
	// "BI", "JV", "PR", "AR", "PY"). Matches the KV["p"] value emitted by
	// the producer node.
	ProducerKvP string

	// OutputPath is the $(BUILD_ROOT)-rooted absolute path of this generated
	// file (e.g. "$(BUILD_ROOT)/devtools/ymake/diag/stats_enums.h_serialized.cpp").
	OutputPath string

	// EmitsIncludes lists the #include targets that the generated file contains.
	// Populated by F-7-B per emitter kind; left nil/empty by F-7-A. Each entry
	// is either:
	//   - a $(BUILD_ROOT)/... path (another generated file → transitive lookup)
	//   - a $(SOURCE_ROOT)/... path (real on-disk file)
	//   - a system-include name handled by the sysincl resolver
	//
	// Stored in discovery order; no deduplication required at this level (the
	// scanner's DFS visited-set deduplicates at traversal time).
	EmitsIncludes []string

	// ProducerRef is the NodeRef of the emitted producer node. Valid only when
	// HasProducerRef is true. resolveCodegenDepRefs uses this to thread the
	// producer ref into consumer CC `deps[]` for both #include-driven (header
	// closure) and input-driven (inputs[] $(BUILD_ROOT) paths) lookups.
	ProducerRef    NodeRef
	HasProducerRef bool
}

// CodegenRegistry is a per-scanner registry mapping every $(BUILD_ROOT)-prefixed
// generated file path to its producer metadata. Populated incrementally during
// the emit walk (codegen emitters fire before CC emitters per PEERDIR-DFS order).
// The scanner consults it as a third existence tier in F-7-C.
type CodegenRegistry struct {
	byOutput map[string]*GeneratedFileInfo
}

// NewCodegenRegistry allocates an empty CodegenRegistry. Pre-sized for the
// observed M3 codegen output count (~200 EN+PB+EV+R6 outputs in the
// devtools/ymake/bin closure).
func NewCodegenRegistry() *CodegenRegistry {
	return &CodegenRegistry{
		byOutput: make(map[string]*GeneratedFileInfo, 256),
	}
}

// Register records info under info.OutputPath.
//
// Precondition: info.OutputPath is non-empty and starts with "$(BUILD_ROOT)/".
// Throws if the same OutputPath is registered a second time — this mirrors
// upstream's DupSrc diagnostic (macro_processor.cpp:957) and enforces the
// build-system invariant that no two nodes produce the same output file.
func (r *CodegenRegistry) Register(info *GeneratedFileInfo) {
	if _, dup := r.byOutput[info.OutputPath]; dup {
		ThrowFmt("CodegenRegistry: duplicate producer for %q (existing kind=%q, new kind=%q)",
			info.OutputPath, r.byOutput[info.OutputPath].ProducerKvP, info.ProducerKvP)
	}

	r.byOutput[info.OutputPath] = info
}

// Lookup returns the GeneratedFileInfo for path, or (nil, false) if path is
// not registered. O(1) map lookup.
func (r *CodegenRegistry) Lookup(path string) (*GeneratedFileInfo, bool) {
	info, ok := r.byOutput[path]

	return info, ok
}

// SetProducerRef backfills the ProducerRef for an already-registered path.
// Throws if path is not registered (callers must have invoked Register first).
// Idempotent: re-setting with the same ref is a no-op; conflicting refs throw.
//
// PR-M3-L0-cascade-close-v2: most emitters call Register BEFORE Emit returns
// the NodeRef (the registry entry is needed by the scanner's existence-tier
// before the emit completes). This helper fills the NodeRef in after Emit so
// resolveCodegenDepRefs can lift it into consumer CC `deps[]`.
func (r *CodegenRegistry) SetProducerRef(path string, ref NodeRef) {
	info, ok := r.byOutput[path]
	if !ok {
		ThrowFmt("CodegenRegistry: SetProducerRef on unregistered path %q", path)
	}

	if info.HasProducerRef && info.ProducerRef != ref {
		ThrowFmt("CodegenRegistry: conflicting ProducerRef for %q (existing=%v, new=%v)",
			path, info.ProducerRef, ref)
	}

	info.ProducerRef = ref
	info.HasProducerRef = true
}

// All returns all registered entries sorted by OutputPath. Deterministic across
// runs because OutputPath is derived from source file paths (which are fixed).
// Allocates a new slice on each call; callers that need a stable snapshot
// should retain the result.
func (r *CodegenRegistry) All() []*GeneratedFileInfo {
	out := make([]*GeneratedFileInfo, 0, len(r.byOutput))

	for _, info := range r.byOutput {
		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].OutputPath < out[j].OutputPath
	})

	return out
}

// Len returns the number of registered entries.
func (r *CodegenRegistry) Len() int {
	return len(r.byOutput)
}
