package main

import "strings"

type GeneratedPyAuxChunksResult struct {
	Refs    []NodeRef
	Outputs []VFS
}

func (e *EmitContext) emitGeneratedPyAuxChunks() *GeneratedPyAuxChunksResult {
	_, instance, d := e.ctx, e.instance, e.d

	if len(d.pySrcs) == 0 {
		return nil
	}

	reg := e.codegen

	var entries []PyProtoAuxEntry

	for i, srcRel := range d.pySrcs {
		info := reg.lookupSplit(dirKey(instance.Path.rel()), srcRel)

		if info == nil {
			continue
		}

		if i >= len(d.pySrcsFullName) || !d.pySrcsFullName[i] {
			continue
		}

		genInputs := info.SourceInputs
		src := build(instance.Path.rel(), "/", srcRel.string())

		entries = append(entries, PyProtoAuxEntry{path: src, key: generatedPyResourceKey(instance.Path.rel(), d, srcRel.string()), inputs: genInputs})

		if !d.pyBuildNoPYC {
			suffix := ".yapyc3"

			if strings.Contains(srcRel.string(), "/") {
				suffix = "." + d.pyYapycSuffix + ".yapyc3"
			}

			yp := build(instance.Path.rel(), "/", srcRel.string(), suffix)

			entries = append(entries, PyProtoAuxEntry{path: yp, key: generatedPyResourceKey(instance.Path.rel(), d, srcRel.string()+".yapyc3"), inputs: genInputs})
		}
	}

	if len(entries) == 0 {
		return nil
	}

	rawRes := e.emitRawAuxChunks(entries, "PY3", true, func(aux VFS, inputs []VFS, ref NodeRef) []VFS {
		return e.rawAuxInputClosure(aux, pyProtoSourceInputs(inputs), ref)
	})

	if rawRes == nil {
		return nil
	}

	res := &GeneratedPyAuxChunksResult{}

	for _, aux := range rawRes.Outputs {
		auxRef, auxOut := e.emitCC(aux)

		res.Refs = append(res.Refs, auxRef)
		res.Outputs = append(res.Outputs, auxOut)
	}

	return res
}

func generatedPyResourceKey(modulePath string, d *ModuleData, srcRel string) string {
	keyPrefix := ""

	if !d.pyTopLevel {
		keyPrefix = modulePath + "/"
	}

	return keyPrefix + srcRel
}

func (e *EmitContext) rawAuxInputClosure(aux VFS, seed []VFS, ref NodeRef) []VFS {
	ctx, _, d := e.ctx, e.instance, e.d
	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	emits := make([]IncludeDirective, 0, len(seed))

	for _, v := range seed {
		emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(v.rel())})
	}

	var psc []ARG

	if p := d.perSrcCFlagsFor(aux.str()); p != nil {
		psc = *p
	}

	e.codegen.register(&GeneratedFileInfo{
		OutputPath:     aux,
		ProducerRef:    ref,
		GeneratorRefs:  []NodeRef{rescompilerRef},
		ParsedIncludes: emits,
		Compile:        &CompileSpec{FlatOutput: d.flatSrc(aux.str()), ForceCxx: true, CFlags: concat(psc, []ARG{argX, argC})},
	})

	closure := walkClosure(e.scanner, aux, d.cc.ScanCfg)

	if len(closure) == 0 {
		return nil
	}

	return closure
}
