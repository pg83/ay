package main

// Relocated here: these helpers are used only by tests; moved out of the
// production sources (gate-dead) so they no longer ship in the binary.

import (
	"strings"

	"github.com/zeebo/xxh3"
)

func computeUID(canonicalBytes []byte) UID {
	sum := xxh3.Hash128(canonicalBytes)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

func canonicalNodeBytes(n *Node) []byte {
	var c canonBuf
	c.writeNode(n)

	return c.buf
}

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}

	return false
}

func readYaConfSections(fs FS, wantSection string, rels ...string) map[string]string {
	out := map[string]string{}

	for _, rel := range rels {
		if !fs.IsFile(srcRootVFS, rel) {
			continue
		}

		raw := fs.Read(rel)

		section := ""

		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)

			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))

				continue
			}

			if section != wantSection {
				continue
			}

			key, val, ok := strings.Cut(line, "=")

			if !ok {
				continue
			}

			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			val = strings.Trim(val, `"`)

			if key != "" {
				out[key] = val
			}
		}
	}

	return out
}

func buildPySrcEntries(d *moduleData, modulePath string) []pySrcEntry {
	return buildPySrcEntriesFor(d, modulePath, d.pySrcs, d.pyTopLevel, d.pyNamespace)
}

func newInclArgMemo() inclArgMemo {
	return inclArgMemo{m: &DenseMap[VFS, STR]{}}
}

// testToolchain builds the module toolchain the way genModule does — from a
// resource-global closure declaring the build/platform/* resources — so tests that
// drive the emitters directly get the same $(CLANG)/$(LLD_ROOT)/$(YMAKE_PYTHON3)
// tool paths without an ambient platform.
func testToolchain() moduleToolchain {
	return resolveModuleToolchain([]resourceDecl{
		makeResourceDecl(resourcePatternClangTool, "sbr:test-clang"),
		makeResourceDecl(resourcePatternLLDRoot, "sbr:test-lld"),
		makeResourceDecl(resourcePatternYMakePython3, "sbr:test-python"),
	})
}

// addToolchainPeers injects the synthetic build/platform/* RESOURCES_LIBRARYs every
// module implicitly PEERDIRs, so a gen test's memFS yields a populated module
// toolchain (d.tc) — the source of compiler/python/objcopy/linker paths. Without
// them the closure is empty and tool-emitting nodes carry blank tool paths.
func addToolchainPeers(files map[string]string) {
	const json = `{"by_platform":{"linux-x86_64":{"uri":"sbr:test"}}}`

	files["build/platform/clang/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG16 clang16.json)\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG20 clang20.json)\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG clang16.json)\nEND()\n"
	files["build/platform/clang/clang16.json"] = json
	// CLANG binds to clang${CLANG_VER}.json (=clang20.json); same sbr here so golden
	// output is version-agnostic.
	files["build/platform/clang/clang20.json"] = json
	files["build/platform/lld/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(LLD_ROOT lld.json)\nEND()\n"
	files["build/platform/lld/lld.json"] = json
	files["build/platform/python/ymake_python3/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(YMAKE_PYTHON3 python.json)\nEND()\n"
	files["build/platform/python/ymake_python3/python.json"] = json
}

func EmitAR(
	instance ModuleInstance,
	objRefs []NodeRef,
	objPaths []VFS,
	peerArchiveRefs []NodeRef,
	hostP *Platform,
	emit Emitter,
) NodeRef {
	if len(objRefs) != len(objPaths) {
		ThrowFmt("EmitAR: objRefs/objPaths length mismatch (%d vs %d)", len(objRefs), len(objPaths))
	}

	archivePath := Build(instance.Path + "/" + ArchiveName(instance.Path))

	return emitARNode(instance, archivePath, nil, objRefs, objPaths, peerArchiveRefs, nil, testToolchain(), hostP, emit)
}

func Gen(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn)) *Graph {
	return genWithResources(fs, targetDir, hostP, targetP, onWarn, nil, false, true)
}

func GenWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool) *Graph {
	return genWithResources(fs, targetDir, hostP, targetP, onWarn, resources, testMode, true)
}
