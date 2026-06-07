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

func statsUIDPreimage(n *Node, c *canonBuf) string {
	return string(appendStatsPreimage(c.strBuf[:0], c, n))
}

func pythonStringListRepr(c *canonBuf, items []string) string {
	return string(appendPythonListRepr(c.strBuf[:0], items))
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
	return inclArgMemo{m: &DenseMap[VFS, ANY]{}}
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

	return emitARNode(instance, archivePath, nil, objRefs, objPaths, peerArchiveRefs, nil, hostP, emit)
}

func Gen(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn)) *Graph {
	return genWithResources(fs, targetDir, hostP, targetP, onWarn, nil, false, true)
}

func GenWithResources(fs FS, targetDir string, hostP, targetP *Platform, onWarn func(Warn), resources *resourceFetchPlan, testMode bool) *Graph {
	return genWithResources(fs, targetDir, hostP, targetP, onWarn, resources, testMode, true)
}
