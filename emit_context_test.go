package main

import (
	"slices"
	"strings"
	"testing"
)

func TestEmitContext_SourceOccurrencesUseOneCompilePipeline(t *testing.T) {
	files := map[string]string{
		"mod/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(plain.cpp)
SRC(flat.cpp -DFLAT_COPY)
SRC_C_AVX2(vector.cpp -DVECTOR_COPY)
GLOBAL_SRCS(global.cpp)
END()
`,
		"mod/plain.cpp":  "int plain() { return 1; }\n",
		"mod/flat.cpp":   "int flat() { return 2; }\n",
		"mod/vector.cpp": "int vector() { return 3; }\n",
		"mod/global.cpp": "int global() { return 4; }\n",
	}

	g := testGen(newMemFS(files), "mod")
	flat := mustNodeByOutput(t, g, "$(B)/mod/flat.cpp.o")
	simd := mustNodeByOutput(t, g, "$(B)/mod/vector.cpp.avx2.o")
	global := mustNodeByOutput(t, g, "$(B)/mod/global.cpp.o")

	if !slices.Contains(anyStrs(flat.Cmds[0].CmdArgs.flat()), "-DFLAT_COPY") {
		t.Fatalf("flat compile flags = %v", anyStrs(flat.Cmds[0].CmdArgs.flat()))
	}

	for _, flag := range []string{"-mavx2", "-mfma", "-mbmi", "-mbmi2", "-DVECTOR_COPY"} {
		if !slices.Contains(anyStrs(simd.Cmds[0].CmdArgs.flat()), flag) {
			t.Fatalf("SIMD cmd missing %q: %v", flag, anyStrs(simd.Cmds[0].CmdArgs.flat()))
		}
	}

	regularAR := mustNodeByOutput(t, g, "$(B)/mod/libmod.a")
	globalAR := mustNodeByOutput(t, g, "$(B)/mod/libmod.global.a")
	regularArgs := strings.Join(anyStrs(regularAR.Cmds[0].CmdArgs.flat()), " ")
	globalArgs := strings.Join(anyStrs(globalAR.Cmds[0].CmdArgs.flat()), " ")

	for _, local := range []string{"plain.cpp.o", "flat.cpp.o", "vector.cpp.avx2.o"} {
		if !strings.Contains(regularArgs, local) {
			t.Fatalf("regular archive missing %q: %s", local, regularArgs)
		}
	}

	if strings.Contains(regularArgs, global.Outputs[0].string()) || !strings.Contains(globalArgs, global.Outputs[0].string()) {
		t.Fatalf("global object routing is wrong; regular=%s global=%s", regularArgs, globalArgs)
	}
}

func TestEmitContext_CompileMetadataBelongsToOccurrence(t *testing.T) {
	files := map[string]string{
		"mod/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRCS(shared.cpp)
SRC(shared.cpp -DFLAT_COPY)
END()
`,
		"mod/shared.cpp": "int shared() { return 1; }\n",
	}

	g := testGen(newMemFS(files), "mod")
	flagged, plain := 0, 0

	for _, node := range g.Graph {
		if node == nil || node.KV.P != pkCC {
			continue
		}

		args := anyStrs(node.Cmds[0].CmdArgs.flat())

		if !slices.Contains(args, "$(S)/mod/shared.cpp") {
			continue
		}

		if slices.Contains(args, "-DFLAT_COPY") {
			flagged++
		} else {
			plain++
		}
	}

	if flagged != 1 || plain != 1 {
		t.Fatalf("shared.cpp compilations: flagged=%d plain=%d, want one of each", flagged, plain)
	}
}

func TestEmitContext_CompileMetadataSurvivesGeneratedSource(t *testing.T) {
	files := map[string]string{
		"mod/ya.make": `LIBRARY()
NO_LIBC()
NO_RUNTIME()
NO_UTIL()
SRC(generated.c.in -DGENERATED_COPY)
END()
`,
		"mod/generated.c.in": "int generated() { return 1; }\n",
	}

	g := testGen(newMemFS(files), "mod")
	cc := mustNodeByOutput(t, g, "$(B)/mod/generated.c.o")

	if !slices.Contains(anyStrs(cc.Cmds[0].CmdArgs.flat()), "-DGENERATED_COPY") {
		t.Fatalf("generated source compile flags = %v", anyStrs(cc.Cmds[0].CmdArgs.flat()))
	}
}

func nodeTestEmitContext(emit *StreamingEmitter, instance ModuleInstance) *EmitContext {
	na := emit.nodeArenas()
	reg := newCodegenRegistry(na)
	scanner := &IncludeScanner{
		codegen:       reg,
		chunkDepRefs:  newChunkDepRefsCache(8),
		chunkDepArena: newBumpAllocator[NodeRef](),
	}

	return &EmitContext{
		ctx:      &GenCtx{emit: emit, na: na, target: instance.Platform, scannerTarget: scanner},
		instance: instance,
		scanner:  scanner,
		codegen:  reg,
	}
}

func TestEmitContext_NodeEmissionUsesBuildInputProducers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reserved bool
	}{{name: "node"}, {name: "reserved", reserved: true}} {
		t.Run(tc.name, func(t *testing.T) {
			emit := newStreamingEmitter(nil)
			instance := targetInstance("mod")
			e := nodeTestEmitContext(emit, instance)
			_ = e.emitNode(Node{Platform: instance.Platform, KV: &prKV})
			explicitRef := e.emitNode(Node{Platform: instance.Platform, KV: &prKV})
			generated := build("mod/generated.cpp")
			producerRef := emit.reserve()
			fires := 0
			pending := e.ctx.na.pendingEmit(func() {
				fires++
				e.emitReservedNode(Node{Platform: instance.Platform, KV: &prKV, Outputs: e.ctx.na.vfsList(generated)}, producerRef)
			})

			e.codegen.register(GeneratedFileInfo{OutputPath: generated, ProducerRef: producerRef, OnUse: pending})
			sourceOnly := e.ctx.na.vfsList(source("mod/header.h"))

			node := Node{
				Platform: instance.Platform,
				KV:       &ccKV,
				Inputs: e.ctx.na.inputList(
					e.ctx.na.vfsList(source("mod/source.cpp"), generated),
					e.ctx.na.vfsList(build("mod/unregistered.cpp"), generated),
					sourceOnly,
				),
				DepRefs: e.ctx.na.refList(explicitRef),
			}
			var consumerRef NodeRef

			if tc.reserved {
				consumerRef = emit.reserve()
				e.emitReservedNode(node, consumerRef)
			} else {
				consumerRef = e.emitNode(node)
			}

			consumer := emit.nodes.s[consumerRef]

			if !slices.Equal(consumer.DepRefs, []NodeRef{explicitRef, producerRef}) {
				t.Fatalf("DepRefs = %v, want [%v %v]", consumer.DepRefs, explicitRef, producerRef)
			}

			if emit.nodes.s[producerRef] == nil {
				t.Fatal("build input producer was not emitted")
			}

			if fires != 1 {
				t.Fatalf("producer emitted %d times, want 1", fires)
			}

			cachedConsumerRef := e.emitNode(Node{Platform: instance.Platform, KV: &ccKV, Inputs: node.Inputs})
			cachedConsumer := emit.nodes.s[cachedConsumerRef]

			if !slices.Equal(cachedConsumer.DepRefs, []NodeRef{producerRef}) {
				t.Fatalf("cached DepRefs = %v, want [%v]", cachedConsumer.DepRefs, producerRef)
			}
		})
	}
}
