package main

import (
	"fmt"
	"slices"
	"strings"
)

const (
	peerRequested PeerRequest = 1 << (36 + iota)
	peerRustHost
	peerGo
	peerRos
	peerMlaunchPackage
	peerSandboxPy2
	peerSandboxPy3
	peerYqlShared
	peerSdcShare
	peerProgram
	peerYqlCurrent
	peerExplicitTags
	peerNativePy2
	peerNativePy3
	peerPackageContents
	peerOptional
)

func genRequestedPeer(ctx *GenCtx, request ModuleInstance) *ModuleEmitResult {
	if entry := ctx.memoEntry(request); entry != nil {
		if entry.result == nil {
			return nil
		}

		if entry.resolved == 0 {
			if request.Demand != demandNone {
				ctx.consumeModuleWarnings(entry.result)
			} else if ctx.warningSink != nil && entry.result.deferredWarnings != nil {
				ctx.warningSink.dependencies = append(ctx.warningSink.dependencies, entry.result.deferredWarnings)
			}

			return entry.result
		}

		peer := request

		peer.Peer, peer.PeerFrom = 0, 0
		peer.Kind = ModuleKind(entry.resolved >> 8 & 31)
		peer.Language = Language(entry.resolved >> 1 & 15)
		peer.Demand = ModuleDemand(entry.resolved & variantDemandMask >> variantDemandShift)
		peer.YqlCurrent = entry.resolved&(1<<13) != 0

		if entry.resolved&variantPIC != 0 {
			peer.Platform = peer.Platform.pic()
		}

		return genModule(ctx, peer)
	}

	if request.Peer&peerOptional != 0 && !peerYaMakeExists(ctx.fs, request.Path) {
		ctx.memoPut(request, nil)

		return nil
	}

	var declaration *ModuleStmt

	exc := try(func() {
		declaration = stmtsModuleDeclaration(moduleStmts(ctx, request.Path.relString()))
	})

	if exc != nil {
		if request.Demand != demandNone && !ctx.keepGoing && ctx.emit.deferPublication == 0 {
			throw(exc)
		}

		warning := Warn{Kind: WarnModuleFailed, Message: fmt.Sprintf("%s: %s", request.Path.relString(), exc.Error())}
		result := &ModuleEmitResult{}

		if request.Demand == demandNone || ctx.emit.deferPublication > 0 {
			result.deferredWarnings = &moduleWarnings{own: []Warn{warning}}

			if ctx.warningSink != nil {
				ctx.warningSink.dependencies = append(ctx.warningSink.dependencies, result.deferredWarnings)
			}
		} else {
			ctx.onWarn(warning)
		}

		ctx.memoPut(request, result)

		return result
	}

	peer := ctx.resolvePeerInstance(request, declaration)
	result := genModule(ctx, peer)
	entry := ctx.memoPut(request, result)

	entry.resolved = ctx.instanceVariant(peer)

	return result
}

type PeerRequest uint64

func makePeerRequest(parent ModuleInstance, d *ModuleData, env Environment) PeerRequest {
	r := peerRequested

	if d.moduleStmt == nil {
		return r
	}

	if isRustModule(d.moduleStmt.Name) && envRustHost(env) {
		r |= peerRustHost
	}

	if isGoModuleType(d.moduleStmt.Name) {
		r |= peerGo
	}

	if d.ros != nil {
		r |= peerRos | PeerRequest(parent.Language)<<32
	}

	if d.multimodule == tokMlaunchLibrary && parent.Language == LangPackage {
		r |= peerMlaunchPackage
	}

	if isUnionModule(d.moduleStmt.Name) || d.moduleStmt.Name == tokPackageFinal || d.moduleStmt.Name == tokCiGroup {
		r |= peerProgram
	}

	if d.yqlCurrent {
		r |= peerYqlCurrent
	}

	language := peerEntryLanguage(parent, d)
	packagePeer := d.multimodule == tokPackage || d.unit.Type == tokPackageFinal
	py2 := language == LangPy2 || packagePeer

	py3 := packagePeer || !isGoModuleType(d.unit.Type) && d.nots == nil && d.docs == nil &&
		d.moduleStmt.Name != tokGenLibrary && d.moduleStmt.Name != tokResourcesLibrary && language != LangPy2

	if kind, _ := env.s.lookup(envPeerdirTags); kind != envAbsent {
		py2, py3 = false, false
		var empty, share bool

		for tag := range strings.FieldsSeq(env.string(envPeerdirTags)) {
			switch tag {
			case "PY2":
				py2 = true
			case "PY3":
				py3 = true
			case "YQL_UDF_SHARED":
				r |= peerYqlShared
			case "__EMPTY__":
				empty = true
			case "SDC_INSTALL_SHARE":
				share = true
			}
		}

		if share && !empty {
			r |= peerSdcShare
		}
	}

	if py2 {
		r |= peerSandboxPy2
	}

	if py3 {
		r |= peerSandboxPy3
	}

	if d.peerTags != nil {
		r |= peerExplicitTags

		if slices.Contains(d.peerTags, "PY2_NATIVE") {
			r |= peerNativePy2
		}

		if slices.Contains(d.peerTags, "PY3_NATIVE") {
			r |= peerNativePy3
		}

		for _, language := range [...]Language{LangCPP, LangPy, LangPy2, LangGo} {
			if slices.Contains(d.peerTags, protoModuleTag(language, false)) {
				r |= 1 << language
			}

			if slices.Contains(d.peerTags, protoModuleTag(language, true)) {
				r |= 1 << (language + 16)
			}
		}
	}

	return r
}

func (ctx *GenCtx) resolvePeerInstance(request ModuleInstance, declaration *ModuleStmt) ModuleInstance {
	peer, rules := request, request.Peer

	peer.Peer, peer.PeerFrom = 0, 0
	var name TOK

	if declaration != nil {
		name = declaration.Name
	}

	if name == tokSandboxTask {
		if rules&peerSandboxPy2 == 0 {
			throwFmt("gen: %s: PEERDIR to %s is prohibited by incompatible tags", request.PeerFrom.relString(), peer.Path.relString())
		}

		peer.Language = LangPy2
	}

	if peer.Language == LangPy2 && !hasPy2Variant(name) {
		peer.Language = LangCPP
	}

	if rules&peerRustHost != 0 && name == tokCrate {
		peer.Language = LangRustHost
	}

	if rules&peerGo != 0 {
		switch {
		case name == tokProtoLibrary || name == tokFbsLibrary || name == tokRosLibrary || name == tokMlaunchLibrary || name == tokMlaunchComponent || name == tokSdcDiagsLibrary:
			peer.Language = LangGo
			fallthrough
		case isGoModuleType(name):
			if peer.Demand == demandNone {
				peer.Demand = demandSelf
			}
		}
	}

	if rules&peerRos != 0 && name == tokRosLibrary {
		peer.Language = Language(rules >> 32 & 15)
	}

	if rules&peerMlaunchPackage != 0 && name == tokMlaunchLibrary || name == tokPackage && rules&peerPackageContents == 0 {
		peer.Language = LangPackage
	}

	if name == tokSdcUnion {
		peer.Language = LangCPP

		if rules&peerSdcShare != 0 {
			peer.Language = LangPackage
		}
	}

	if isYqlUdfStaticModule(name) && rules&peerYqlShared != 0 {
		peer.Kind = KindBin
	}

	if name == tokGoDll {
		peer.Platform = peer.Platform.pic()
	}

	if name == tokSandboxPy233Task {
		variants := ctx.sandboxPythonTaskVariants(peer, name)

		switch {
		case rules&peerSandboxPy2 != 0 && variants&(1<<LangPy2) != 0:
			peer.Language = LangPy2
		case rules&peerSandboxPy3 != 0 && variants&(1<<LangCPP) != 0:
			peer.Language = LangCPP
		default:
			throwFmt("gen: %s: PEERDIR to %s is prohibited by incompatible tags", request.PeerFrom.relString(), peer.Path.relString())
		}
	}

	if rules&peerProgram != 0 && name == tokPy3Program {
		peer.Kind = KindBin
	}

	if rules&peerYqlCurrent != 0 {
		peer.YqlCurrent = name == tokYqlLibrary || isYqlUdfStaticModule(name)
	}

	if rules&peerExplicitTags != 0 && name == tokPy23NativeLibrary {
		switch {
		case rules&peerNativePy2 != 0:
			peer.Language = LangPy2
		case rules&peerNativePy3 != 0:
			peer.Language = LangCPP
		default:
			throwFmt("gen: %s: no compatible Python header peer in %s", request.PeerFrom.relString(), peer.Path.relString())
		}
	}

	if rules&peerExplicitTags != 0 && name == tokProtoLibrary {
		allowed := uint16(rules)

		if declaration.Schema {
			allowed = uint16(rules >> 16)
		}

		matched := false

		for _, language := range [...]Language{LangCPP, LangPy, LangPy2, LangGo} {
			if allowed&(1<<language) == 0 {
				continue
			}

			candidate := peer

			candidate.Language = language

			if peer.Demand == demandNone && ctx.roots[candidate] {
				candidate.Demand = ctx.rootDemand
			}

			if genModule(ctx, candidate).protoExcluded {
				continue
			}

			peer, matched = candidate, true

			break
		}

		if !matched {
			throwFmt("gen: %s: no compatible proto peer in %s", request.PeerFrom.relString(), peer.Path.relString())
		}
	}

	if peer.Demand == demandNone && ctx.roots[peer] {
		peer.Demand = ctx.rootDemand
	}

	if rules&peerGo != 0 && name == tokRosLibrary && peer.Language == LangGo {
		cpp := peer

		cpp.Language, cpp.Demand = LangCPP, demandNone
		genModule(ctx, cpp)
	}

	return peer
}
