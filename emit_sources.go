package main

import (
	"fmt"
	"strings"
)

type SourceEmit struct {
	Ref     NodeRef
	OutPath VFS

	// Extra holds additional compiled objects produced by the same source whose
	// generating statement is shared with the primary object (e.g. a GRPC()
	// inline .proto yields both <base>.pb.cc.o and <base>.grpc.pb.cc.o). They
	// archive immediately after the primary object with the same SrcMeta.
	Extra []SourceEmit
}

// emitOneSource dispatches a module source to its per-extension emitter. Each
// emitLibrary*Source lives next to the node emitter it drives (emit_cc.go,
// emit_r6.go, emit_ev.go, …), like emitBisonY in emit_bison_y.go.
func emitOneSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	switch {
	case strings.HasSuffix(srcRel, ".proto"):
		return emitLibraryProtoSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".fbs64"),
		strings.HasSuffix(srcRel, ".fbs"):
		return emitLibraryFlatcSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".rodata"):
		return emitLibraryRodataSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"),
		// Uppercase .C is a C++ source upstream (_SRC("C") → $_SRC_cpp,
		// ymake.core.conf:3466); HasSuffix is case-sensitive so it does not
		// collide with .c. isCxxSource classifies it as C++.
		strings.HasSuffix(srcRel, ".C"):
		return emitLibraryCSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		return emitLibraryAsmSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".rl6"):
		return emitLibraryRagel6Source(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".y"),
		strings.HasSuffix(srcRel, ".ypp"):
		return emitBisonY(ctx, instance, srcRel, in, in.BisonGenExt)
	case strings.HasSuffix(srcRel, ".ev"):
		return emitLibraryEvSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".rl"):
		return emitLibraryRagel5Source(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".h.in"):
		return emitLibraryHInSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		return emitLibraryCInSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".sc"):
		return emitLibrarySCSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".gperf"):
		return emitLibraryGperfSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".cfgproto"):
		// The config-proto induced PEERDIR (codegen/protos/protobuf) is wired in
		// collectModule. The .cfgproto.pb.cc/.pb.h codegen-output emission (and the
		// resulting object in this module's own archive) is a separate node-count
		// class, out of T-45 scope (induced-peer reachability/ordering only). Skip
		// the object here without the unsupported-source warning.
		return nil
	}

	// An unmodelled codegen source extension (e.g. .gperf, .pyx not yet wired
	// here). Under --keep-going this warns and the source is skipped so gen can
	// complete (its compiled object and any headers it would yield are absent —
	// a node-count gap to close later); in strict mode onWarn makes it fatal.
	ctx.onWarn(Warn{
		Kind:    WarnUnsupportedSource,
		Message: fmt.Sprintf("%s: unsupported source extension in %q", instance.Path.rel(), srcRel),
	})

	return nil
}
