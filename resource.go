package main

import (
	"strings"
)

func resourceModuleTag(modName TOK) *string {
	switch modName {
	case tokPy3Library, tokPy3ProgramBin, tokPy23Library, tokPy23NativeLibrary:
		return stringPtr("PY3")
	}

	return nil
}

func resourceBinTagForData(d *ModuleData) *string {
	if d.moduleStmt.Name == tokPy3Program {
		return stringPtr("PY3_BIN")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

func resourceLibTagForData(d *ModuleData) *string {
	if d.moduleStmt.Name == tokPy3Program || d.programPairedLib {
		return stringPtr("PY3_BIN_LIB")
	}

	return resourceModuleTag(d.moduleStmt.Name)
}

type PySrcEntry struct {
	pathHash    string
	pathInput   VFS
	key         string
	kvHash      string
	kvCmd       string
	inputDep    VFS
	extraInputs []VFS
}

func resolvePySrcRel(fs FS, srcDirs []VFS, modulePath, srcRel string) string {
	for i := len(srcDirs) - 1; i >= 1; i-- {
		if fs.isFile(srcDirs[i], srcRel) {
			return srcDirs[i].rel() + "/" + srcRel
		}
	}

	if srcRel != "" && pathIsClean(srcRel) &&
		!fs.isFile(dirKey(modulePath), srcRel) && fs.isFile(srcRootVFS, srcRel) {
		return srcRel
	}

	return modulePath + "/" + srcRel
}

func buildPySrcEntriesFor(reg *CodegenRegistry, fs FS, d *ModuleData, modulePath string, srcs []string, topLevel bool, namespace *STR) []PySrcEntry {
	keyPrefix := ""

	if !topLevel {
		if namespace != nil {
			keyPrefix = strings.ReplaceAll(strings.TrimSuffix(namespace.string(), "."), ".", "/") + "/"
		} else {
			keyPrefix = modulePath + "/"
		}
	}

	fullName := make(map[string]bool, len(d.pySrcs))

	for i, s := range d.pySrcs {
		if i < len(d.pySrcsFullName) && d.pySrcsFullName[i] {
			fullName[s.string()] = true
		}
	}

	out := make([]PySrcEntry, 0, len(srcs)*2)

	for _, srcRel := range srcs {
		suffix := ".yapyc3"

		if strings.Contains(srcRel, "/") {
			suffix = "." + d.pyYapycSuffix + ".yapyc3"
		}

		resolvedRel := resolvePySrcRel(fs, d.srcDirs, modulePath, srcRel)
		genInfo := reg.lookupSplit(dirKey(modulePath), internStr(srcRel))
		generated := genInfo != nil
		pySource := source(resolvedRel)

		if generated {
			pySource = build(modulePath, "/", srcRel)
			resolvedRel = modulePath + "/" + srcRel
		}

		srcEdge := pySource
		copyStaged := generated && genInfo.SourcePath != 0 && genInfo.SourcePath.isSource()

		if copyStaged {
			srcEdge = genInfo.SourcePath
		}

		if !d.pyBuildNoPY {
			pyKey := "resfs/file/py/" + keyPrefix + srcRel
			pyKvHash := "resfs/src/" + pyKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + "\"}"
			pyKvCmd := "resfs/src/" + pyKey + "=" + resolvedRel

			var pyExtra []VFS

			if copyStaged {
				pyExtra = []VFS{srcEdge}
			}

			out = append(out, PySrcEntry{
				pathHash:    srcRel,
				pathInput:   pySource,
				key:         pyKey,
				kvHash:      pyKvHash,
				kvCmd:       pyKvCmd,
				inputDep:    pySource,
				extraInputs: pyExtra,
			})
		}

		if !d.pyBuildNoPYC {
			ypKey := "resfs/file/py/" + keyPrefix + srcRel + ".yapyc3"
			ypPathInput := build(modulePath, "/", srcRel, suffix)
			ypKvHash := "resfs/src/" + ypKey + "=${rootrel;context=TEXT;input=TEXT:\"" + srcRel + suffix + "\"}"
			ypKvCmd := "resfs/src/" + ypKey + "=" + modulePath + "/" + srcRel + suffix

			out = append(out, PySrcEntry{
				pathHash:    srcRel + suffix,
				pathInput:   ypPathInput,
				key:         ypKey,
				kvHash:      ypKvHash,
				kvCmd:       ypKvCmd,
				inputDep:    ypPathInput,
				extraInputs: []VFS{srcEdge},
			})
		}
	}

	return out
}

func pySrcYapycSuffix(modulePath string) string {
	return protoPathID("$S/" + modulePath)[:4]
}
