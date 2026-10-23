package main

import "strings"

type NativeJavaProgram struct {
	Call    JavaRunProgram
	DeclSeq int
}

func (e *EmitContext) emitNativeJavaProgram(program NativeJavaProgram) {
	dir := e.instance.Path.relString()
	r := parseJavaRunProgram(dir, program.Call)
	outputs := javaRunProgramOutputs(dir, r)

	if len(outputs) == 0 {
		throwFmt("RUN_JAVA_PROGRAM requires at least one output")
	}

	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		globals := map[string]VFS{}

		for _, decl := range e.peers.ResourceGlobals {
			globals[strings.TrimSuffix(decl.GlobalVar.string(), resourceGlobalSuffix)] = decl.Value
		}

		e.ctx.emitJavaRunProgram(dir, e.instance.Platform, e.d.runJavaJdk, globals, r, ref, e.runProgramInputVFS)
	})

	action := &SourceActionInfo{DeclSeq: program.DeclSeq}

	for _, output := range outputs {
		e.register(GeneratedFileInfo{SourceAction: action, OutputPath: output, ProducerRef: ref, ProducerMainOut: outputs[0], OnUse: pending})
	}

	for _, name := range r.out {
		if isCodegenProducingSrc(name) || isNativeSourceExt(name) {
			e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(dir, name).any(), Prio: stmtPrioDefault, Bucket: bkRunPython})
		}
	}
}
