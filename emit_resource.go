package main

import (
	"crypto/md5"
	encb64 "encoding/base64"
	enchex "encoding/hex"
	"sort"
	"strings"
)

var (
	objcopyScriptPath           = objcopyScriptVFS.string()
	rescompressorBinPath        = rescompressorBinVFS.string()
	rescompilerBinPath          = rescompilerBinVFS.string()
	rescompilersChunk           = []VFS{rescompilerBinVFS, rescompressorBinVFS}
	rescompilersWithScriptChunk = []VFS{rescompilerBinVFS, rescompressorBinVFS, objcopyScriptVFS}
	objcopyScriptChunk          = []VFS{objcopyScriptVFS}
	rawAuxKV                    = KV{P: pkPR, PC: pcYellow, ShowOut: true}
	pyObjcopyKV                 = KV{P: pkPY, PC: pcYellow, ShowOut: true}
)

const (
	hashLen    = 26
	rootCmdLen = 200
	maxCmdLen  = 8000
)

func resourceCanObjcopy(path, key string) bool {
	for _, bad := range []string{"${ARCADIA_BUILD_ROOT}", "${ARCADIA_SOURCE_ROOT}", "conftest.py"} {
		if strings.Contains(path, bad) || strings.Contains(key, bad) {
			return false
		}
	}

	return true
}

func resourceHashInto(buf []byte, list []string, moduleTag string) (string, []byte) {
	sort.Strings(list)

	buf = buf[:0]

	for i, s := range list {
		if i > 0 {
			buf = append(buf, ',')
		}

		buf = append(buf, s...)
	}

	buf = append(buf, moduleTag...)

	sum := md5.Sum(buf)

	return enchex.EncodeToString(sum[:])[:hashLen], buf
}

func renderResourceKvCmd(kv string) string {
	kv = strings.ReplaceAll(kv, "${ARCADIA_ROOT}/", "$(S)/")
	kv = strings.ReplaceAll(kv, "${ARCADIA_BUILD_ROOT}/", "$(B)/")

	return kv
}

func rootrelExpand(kv string, resolved string) string {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""

	idx := strings.Index(kv, marker)

	if idx < 0 {
		return kv
	}

	tail := kv[idx+len(marker):]
	end := strings.Index(tail, "\"}")

	if end < 0 {
		return kv
	}

	return kv[:idx] + resolved + tail[end+len("\"}"):]
}

func rootrelInputPath(kv string) (string, bool) {
	const marker = "${rootrel;context=TEXT;input=TEXT:\""

	idx := strings.Index(kv, marker)

	if idx < 0 {
		return "", false
	}

	tail := kv[idx+len(marker):]
	end := strings.Index(tail, "\"}")

	if end < 0 {
		return "", false
	}

	return tail[:end], true
}

type ObjcopyArgBlocks struct {
	pre  []STR
	post []STR
}

type ObjcopyEmitCtx struct {
	rescompilerLDRef   NodeRef
	rescompressorLDRef NodeRef
	blocks             ObjcopyArgBlocks
	na                 *NodeArenas
}

func newObjcopyEmitCtx(ctx *GenCtx, d *ModuleData, p *Platform) *ObjcopyEmitCtx {
	oc := &ObjcopyEmitCtx{na: ctx.na}

	oc.rescompilerLDRef, _ = ctx.tool(argToolsRescompiler)
	oc.rescompressorLDRef, _ = ctx.tool(argToolsRescompressor)
	oc.blocks = composeObjcopyArgBlocks(d.tc, p)

	return oc
}

func composeObjcopyArgBlocks(tc ModuleToolchain, p *Platform) ObjcopyArgBlocks {
	return ObjcopyArgBlocks{
		pre: []STR{
			tc.Python3,
			internStr(objcopyScriptPath),
			argCompiler.str(), tc.CXX,
			argObjcopy.str(), tc.Objcopy,
			argCompressor.str(), internStr(rescompressorBinPath),
			argRescompiler.str(), internStr(rescompilerBinPath),
			argOutputObj.str(),
		},
		post: []STR{argTarget.str(), internStr(p.Triple)},
	}
}

func objcopyCmdArgs(oc *ObjcopyEmitCtx, outputObj VFS, payload []STR) ArgChunks {
	return oc.na.chunkList(oc.blocks.pre, oc.na.strList((outputObj).fullSTR()), oc.blocks.post, payload)
}

type ResolvedResource struct {
	Input           VFS
	ProducerRef     NodeRef
	ProducerMainOut VFS
	SourceInputs    []VFS
}

func (e *EmitContext) resolveResourceInput(rawPath string, fallback VFS) ResolvedResource {
	_, instance := e.ctx, e.instance
	output := resourceOutputVFS(instance.Path.relString(), rawPath)

	if info := e.codegen.lookup(output); info != nil {
		return ResolvedResource{
			Input:           output,
			ProducerRef:     info.ProducerRef,
			ProducerMainOut: info.ProducerMainOut,
			SourceInputs:    info.SourceInputs,
		}
	}

	return ResolvedResource{Input: fallback}
}

func buildObjcopyNode(ctx *GenCtx, instance ModuleInstance, oc *ObjcopyEmitCtx, kv *KV, outputObj VFS, payload []STR, inputs InputChunks, deps []NodeRef) NodeRef {
	na := oc.na
	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	node := Node{
		Platform:     instance.Platform,
		Cmds:         na.cmdList(Cmd{CmdArgs: objcopyCmdArgs(oc, outputObj, payload), Env: env}),
		Env:          env,
		Inputs:       inputs,
		Outputs:      na.vfsList(outputObj),
		KV:           kv,
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		Resources:    instance.Platform.UsesPython3Clang,
		DepRefs:      deps,
	}

	return ctx.emit.emitNode(node)
}

type ResourceItem struct {
	Path  string
	Key   string
	Cmd   string
	Input VFS
	Extra []VFS
	Aux   []VFS
}

type ResourcePack struct {
	Tag        STR
	Items      []ResourceItem
	RawClosure func(aux VFS, inputs []VFS, ref NodeRef) Closure
}

func resourceChunkEnds(items []ResourceItem, objcopy bool) []int {
	var ends []int

	cmdLen := 0

	for i, it := range items {
		if it.Path == "-" {
			cmdLen += rootCmdLen + len(it.Key)

			if !objcopy {
				cmdLen += len(it.Path)
			}
		} else if objcopy {
			cmdLen += rootCmdLen + len(it.Path) + encb64.StdEncoding.EncodedLen(len(it.Key))
		} else {
			cmdLen += rootCmdLen + len(it.Path) + len(it.Key)
		}

		if cmdLen >= maxCmdLen {
			ends = append(ends, i+1)
			cmdLen = 0
		}
	}

	if cmdLen > 0 {
		ends = append(ends, len(items))
	}

	return ends
}

func (e *EmitContext) packResources(p ResourcePack) (refs []NodeRef, outs []VFS) {
	objItems := make([]ResourceItem, 0, len(p.Items))
	rawItems := make([]ResourceItem, 0, len(p.Items))

	for _, it := range p.Items {
		if resourceCanObjcopy(it.Path, it.Key) {
			objItems = append(objItems, it)
		} else {
			rawItems = append(rawItems, it)
		}
	}

	if len(rawItems) > 0 {
		r, o := e.packRawResourceChunks(rawItems, p)

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	if len(objItems) > 0 {
		r, o := e.packObjcopyResourceChunks(objItems, p)

		refs = append(refs, r...)
		outs = append(outs, o...)
	}

	return refs, outs
}

func (e *EmitContext) packObjcopyResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	na := ctx.na
	oc := newObjcopyEmitCtx(ctx, d, instance.Platform)
	unitElem := "$S/" + instance.Path.relString()
	tag := ""

	if p.Tag != 0 {
		tag = p.Tag.string()
	}

	hashScratch := ctx.resHashScratch[:0]
	hashBuf := ctx.resHashBuf
	b64Scratch := ctx.resB64Scratch

	defer func() {
		ctx.resHashScratch = hashScratch[:0]
		ctx.resHashBuf = hashBuf
		ctx.resB64Scratch = b64Scratch
	}()

	lo := 0

	for _, hi := range resourceChunkEnds(items, true) {
		chunk := items[lo:hi]

		lo = hi

		nPaths, nKvs, cand := 0, 0, 0

		for _, it := range chunk {
			if it.Path == "-" {
				nKvs++
			} else {
				nPaths++
			}

			cand += 1 + len(it.Extra)
		}

		payloadCap := 0

		if nPaths > 0 {
			payloadCap += 2 + 2*nPaths
		}

		if nKvs > 0 {
			payloadCap += 1 + nKvs
		}

		payload := make([]STR, 0, payloadCap)

		hashScratch = hashScratch[:0]

		if nPaths > 0 {
			payload = append(payload, argInputs.str())

			for _, it := range chunk {
				if it.Path != "-" {
					payload = append(payload, it.Input.fullSTR())
					hashScratch = append(hashScratch, it.Path)
				}
			}

			payload = append(payload, argKeys.str())

			for _, it := range chunk {
				if it.Path != "-" {
					b64Scratch = encb64.StdEncoding.AppendEncode(b64Scratch[:0], strBytes(it.Key))

					key := internBytes(b64Scratch)

					payload = append(payload, key)
					hashScratch = append(hashScratch, key.string())
				}
			}
		}

		if nKvs > 0 {
			payload = append(payload, argKvs.str())

			for _, it := range chunk {
				if it.Path == "-" {
					payload = append(payload, internStr(it.Cmd))
					hashScratch = append(hashScratch, it.Key)
				}
			}
		}

		deduper.reset()

		adjacent := make([]VFS, 0, cand)

		for _, it := range chunk {
			if it.Input != 0 && deduper.add(it.Input.strID()) {
				adjacent = append(adjacent, it.Input)
			}

			for _, v := range it.Extra {
				if v != 0 && deduper.add(v.strID()) {
					adjacent = append(adjacent, v)
				}
			}
		}

		for _, v := range rescompilersWithScriptChunk {
			deduper.add(v.strID())
		}

		var tail []VFS

		for _, it := range chunk {
			for _, v := range it.Aux {
				if v != 0 && deduper.add(v.strID()) {
					tail = append(tail, v)
				}
			}
		}

		inputs := na.inputList(rescompilersChunk, adjacent, objcopyScriptChunk)

		if len(tail) > 0 {
			inputs = append(inputs, tail)
		}

		hashScratch = append(hashScratch, unitElem)

		var hash string

		hash, hashBuf = resourceHashInto(hashBuf, hashScratch, tag)

		outputObj := build(instance.Path.relString(), "/objcopy_", hash, ".o")
		dataInputs := concat(adjacent, tail)
		deps := resolveCodegenDepRefsIncl(ctx, instance, na, dataInputs, depRefs(oc.rescompilerLDRef, oc.rescompressorLDRef)...)

		refs = append(refs, buildObjcopyNode(ctx, instance, oc, &pyObjcopyKV, outputObj, payload, inputs, deps))
		outs = append(outs, outputObj)
	}

	return refs, outs
}

func (e *EmitContext) packRawResourceChunks(items []ResourceItem, p ResourcePack) (refs []NodeRef, outs []VFS) {
	ctx, instance := e.ctx, e.instance
	na := ctx.na

	if p.RawClosure == nil {
		throwFmt("packResources: %s has raw-routed resource items but no RawClosure", instance.Path.relString())
	}

	rescompilerRef, _ := ctx.tool(argToolsRescompiler)
	unitElem := "$S/" + instance.Path.relString()
	dash := str2
	tag := ""

	if p.Tag != 0 {
		tag = p.Tag.string()
	}

	hashScratch := ctx.resHashScratch[:0]
	hashBuf := ctx.resHashBuf

	defer func() {
		ctx.resHashScratch = hashScratch[:0]
		ctx.resHashBuf = hashBuf
	}()

	lo := 0

	for _, hi := range resourceChunkEnds(items, false) {
		chunk := items[lo:hi]

		lo = hi

		deduper.reset()

		var adjacent []VFS

		hashScratch = hashScratch[:0]

		for _, it := range chunk {
			if it.Path == "-" {
				hashScratch = append(hashScratch, "-", it.Key)
			} else {
				hashScratch = append(hashScratch, it.Path, "-"+it.Key)
			}

			if it.Input != 0 && deduper.add(it.Input.strID()) {
				adjacent = append(adjacent, it.Input)
			}

			for _, v := range it.Extra {
				if v != 0 && deduper.add(v.strID()) {
					adjacent = append(adjacent, v)
				}
			}
		}

		hashScratch = append(hashScratch, unitElem)

		var hash string

		hash, hashBuf = resourceHashInto(hashBuf, hashScratch, tag)

		aux := build(instance.Path.relString(), "/", hash, "_raw.auxcpp")
		auxRef := ctx.emit.reserve()
		auxClosure := p.RawClosure(aux, adjacent, auxRef)
		auxLen := auxClosure.len()
		nodeCmd := make([]STR, 0, 2+2*len(chunk))

		nodeCmd = append(nodeCmd, internStr(rescompilerBinPath), aux.fullSTR())

		for _, it := range chunk {
			if it.Path == "-" {
				nodeCmd = append(nodeCmd, dash, internStr(it.Cmd))
			} else {
				nodeCmd = append(nodeCmd, it.Input.fullSTR(), internV("-", it.Key))
			}
		}

		deps := concat(resolveCodegenDepRefsIncl(ctx, instance, na, adjacent), depRefs(rescompilerRef))
		env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

		deduper.reset()

		for _, v := range adjacent {
			deduper.add(v.strID())
		}

		tail := make([]VFS, 0, 1+auxLen)

		if deduper.add(rescompilerBinVFS.strID()) {
			tail = append(tail, rescompilerBinVFS)
		}

		auxClosure.each(func(v VFS) {
			if v == aux {
				return
			}

			if deduper.add(v.strID()) {
				tail = append(tail, v)
			}
		})

		ctx.emit.emitReservedNode(Node{
			Platform:     instance.Platform,
			Cmds:         na.cmdList(Cmd{CmdArgs: na.chunkList(nodeCmd), Env: env}),
			Env:          env,
			Inputs:       na.inputList(adjacent, tail),
			Outputs:      na.vfsList(aux),
			KV:           &rawAuxKV,
			Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
			DepRefs:      deps,
		}, auxRef)

		ccRef, ccOut := e.emitCC(aux)

		refs = append(refs, ccRef)
		outs = append(outs, ccOut)
	}

	return refs, outs
}

type ObjcopyEmitResult struct {
	Refs            []NodeRef
	Outputs         []VFS
	PySrcTrailCount int
}

func (e *EmitContext) emitKvOnlyResource(tag STR, kvsHash, kvsCmd []string) ([]NodeRef, []VFS) {
	items := make([]ResourceItem, len(kvsHash))

	for i := range kvsHash {
		items[i] = ResourceItem{Path: "-", Key: kvsHash[i], Cmd: kvsCmd[i]}
	}

	return e.packResources(ResourcePack{Tag: tag, Items: items})
}

func (e *EmitContext) emitResourceFile(entries []ResourceEntry, moduleTag STR) (refs []NodeRef, outs []VFS) {
	ctx, instance, d := e.ctx, e.instance, e.d
	batch := make([]ResourceItem, 0, len(entries))

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}

		r, o := e.packResources(ResourcePack{Tag: moduleTag, Items: batch})

		refs = append(refs, r...)
		outs = append(outs, o...)
		batch = batch[:0]
	}

	for _, entry := range entries {
		if entry.Path == "-" {
			it := ResourceItem{Path: "-", Key: entry.Key}

			if inner, ok := rootrelInputPath(entry.Key); ok {
				r := e.resolveResourceInput(inner, copyFileInputVFS(ctx.fs, instance.Path, inner))

				it.Cmd = renderResourceKvCmd(rootrelExpand(entry.Key, r.Input.relString()))
				it.Aux = []VFS{r.Input, r.ProducerMainOut}
			} else {
				it.Cmd = renderResourceKvCmd(entry.Key)
			}

			batch = append(batch, it)
		} else {
			r := e.resolveResourceInput(entry.Path, copyFileInputVFS(ctx.fs, instance.Path, entry.Path))
			it := ResourceItem{Path: entry.Path, Key: entry.Key, Input: r.Input}

			if r.ProducerRef != 0 {
				cv := walkClosure(e.scanner, r.Input, d.cc.ScanCfg)

				eachBucketVFS(cv.buckets, func(v VFS) {
					if v.isBuild() {
						it.Aux = append(it.Aux, v)
					}
				})

				for _, v := range r.SourceInputs {
					if v.isSource() && objcopySourceLeafKept(v.relString()) {
						it.Aux = append(it.Aux, v)
					}
				}
			}

			it.Aux = append(it.Aux, r.ProducerMainOut)
			batch = append(batch, it)
		}

		if entry.EndsBatch {
			flushBatch()
		}
	}

	flushBatch()

	return refs, outs
}

func (e *EmitContext) emitResourceObjcopy() *ObjcopyEmitResult {
	_, _, d := e.ctx, e.instance, e.d
	hasKvOnly := d.pyMain != nil || len(d.noCheckImports) > 0 || e.hasEnginePySrcs() || len(d.yaConfJSON) > 0

	if len(e.resources) == 0 && len(d.pyPyiResources) == 0 && !hasKvOnly {
		return nil
	}

	out := &ObjcopyEmitResult{}
	pyMainRefs, pyMainOuts := e.emitPyMainObjcopy()

	out.Refs = append(out.Refs, pyMainRefs...)
	out.Outputs = append(out.Outputs, pyMainOuts...)

	noCheckRefs, noCheckOuts := e.emitNoCheckImportsObjcopy()

	out.Refs = append(out.Refs, noCheckRefs...)
	out.Outputs = append(out.Outputs, noCheckOuts...)

	yaConfRefs, yaConfOuts := e.emitYaConfJSONObjcopy()

	out.Refs = append(out.Refs, yaConfRefs...)
	out.Outputs = append(out.Outputs, yaConfOuts...)

	if len(e.resources) == 0 && len(d.pyPyiResources) == 0 {
		trailStart := len(out.Refs)
		srcRes := e.emitPySrcObjcopy()

		if srcRes != nil {
			out.Refs = append(out.Refs, srcRes.Refs...)
			out.Outputs = append(out.Outputs, srcRes.Outputs...)
		}

		out.PySrcTrailCount = len(out.Refs) - trailStart

		return out
	}

	moduleTag := d.unit.Tag
	py3BinProgramSide := d.unit.Tag == unitTagPy3Bin

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(e.resources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	trailStart := len(out.Refs)
	srcRes := e.emitPySrcObjcopy()

	if srcRes != nil {
		out.Refs = append(out.Refs, srcRes.Refs...)
		out.Outputs = append(out.Outputs, srcRes.Outputs...)
	}

	if !py3BinProgramSide {
		r, o := e.emitResourceFile(d.pyPyiResources, moduleTag)

		out.Refs = append(out.Refs, r...)
		out.Outputs = append(out.Outputs, o...)
	}

	out.PySrcTrailCount = len(out.Refs) - trailStart

	return out
}
