package main

import "strings"

func (e *EmitContext) emitPy2TestSources() *ObjcopyEmitResult {
	if len(e.pySrcsReg) == 0 {
		return nil
	}

	ctx, d := e.ctx, e.d
	result := &ObjcopyEmitResult{}

	var toolRef NodeRef
	var tool VFS

	if !d.pyBuildNoPYC {
		toolRef, tool = ctx.tool(argToolsPy2cc)
	}

	for gi := range d.pySrcGroups {
		var items []ResourceItem

		for _, ps := range e.pySrcsReg {
			if ps.Group != gi {
				continue
			}

			name := strings.ReplaceAll(strings.TrimSuffix(ps.Module.string(), ".py"), "/", ".")

			if !d.pyBuildNoPY {
				key := "/py_modules/" + name
				item := ResourceItem{Path: ps.Token.string(), Key: key}

				item.setInput(ps.Path)
				items = append(items, item, ResourceItem{Path: "-", Key: "resfs/src/" + key + "=${rootrel;context=TEXT;input=TEXT:\"" + ps.Token.string() + "\"}", Cmd: internStr("resfs/src/" + key + "=" + ps.Path.relString()), AuxSources: []VFS{ps.Path}})
			}

			if !d.pyBuildNoPYC {
				rel := ps.Token.string()

				if strings.Contains(rel, "/") {
					rel += "." + d.pyYapycSuffix
				}

				out := build(e.instance.Path.relString(), "/", rel, ".yapyc")

				ref := ctx.emit.emit(&Node{Platform: e.instance.Platform, KV: &pyCodegenKV, Env: envVarsVCSPyHash,
					Cmds:   ctx.na.cmdList(Cmd{CmdArgs: ctx.na.chunkList(ctx.na.anyList(tool.any(), internStr(ps.Path.relString()+"-").any(), ps.Path.any(), out.any())), Env: envVarsVCSPyHash}),
					Inputs: ctx.na.inputList(ctx.na.vfsList(tool, ps.Path)), Outputs: ctx.na.vfsList(out), DepRefs: depRefs(toolRef)})

				e.register(GeneratedFileInfo{OutputPath: out, SourcePath: ps.Path, ProducerRef: ref, SourceInputs: ctx.na.vfsList(ps.Path)})
				items = append(items, ResourceItem{Path: rel + ".yapyc", Key: "/py_code/" + name, BuildInput: out})
			}
		}

		refs, outs := e.packResources(ResourcePack{DontCompress: true, Items: items})

		result.Refs = append(result.Refs, refs...)
		result.Outputs = append(result.Outputs, outs...)
	}

	return result
}
