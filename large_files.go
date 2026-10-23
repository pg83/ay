package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path"
	"strings"
)

const largeFileDescriptor = `import json, sys
res_desc = json.load(sys.stdin)
storage = res_desc["storage"]
res_id = res_desc["resource_id"]
print(json.dumps([storage.lower(), '{}'.format(res_id)]))
`

func readLargeFileDescriptor(fs FS, placeholder VFS, external string) (string, string) {
	var values [2]string

	if err := try(func() {
		cmd := exec.Command("python3", "-c", largeFileDescriptor)

		cmd.Stdin = bytes.NewReader(concatChunks(fs.read(placeholder.relString())))

		data, err := cmd.CombinedOutput()

		if err != nil {
			throwFmt("%s", strings.TrimSpace(string(data)))
		}

		throw(json.Unmarshal(data, &values))
	}); err != nil {
		throwFmt("LARGE_FILES: error processing placeholder file \"%s.\": %s", external, err)
	}

	return values[0], values[1]
}

func resolveLargeFileSource(fs FS, codegen *CodegenRegistry, modulePath, name string, srcDirs []VFS) VFS {
	if rooted, ok := moduleRootedVFS(modulePath, name); ok {
		return rooted
	}

	dirs := append([]VFS{source(modulePath)}, srcDirs...)

	dirs = append(dirs, srcRootDirVFS)

	for _, dir := range dirs {
		if dir.isBuild() {
			candidate := buildClean(path.Join(dir.relString(), name))

			if codegen.lookup(candidate) != nil {
				return candidate
			}

			continue
		}

		candidate := source(path.Join(dir.relString(), name))

		if fs.sourcePathExists(candidate.relString()) {
			return candidate
		}
	}

	return 0
}

func applyLargeFiles(fs FS, codegen *CodegenRegistry, modulePath string, stmt UnknownStmt, d *ModuleData, onWarn func(Warn)) {
	defer d.recordCollectionDeclarations()

	args := stmt.Args

	if len(args) > 0 && args[0].string() == "AUTOUPDATED" {
		args = args[1:]
	}

	for _, value := range args {
		d.recordCollectionDeclarations()

		arg := value.string()

		if arg == "AUTOUPDATED" {
			onWarn(Warn{Kind: WarnPlugin, Message: "Please set AUTOUPDATED argument before other file names"})

			continue
		}

		if src := resolveLargeFileSource(fs, codegen, modulePath, arg, d.srcDirs); src != 0 && src.isSource() {
			onWarn(Warn{Kind: WarnPlugin, Message: "Used local large file " + src.string() + ". Don't forget to run 'ya upload --update-external' and commit " + src.string() + ".external"})

			d.copyFiles = append(d.copyFiles, CopyFileEntry{Src: src.string(), Dst: arg})

			continue
		}

		external := arg + ".external"
		placeholder := resolveLargeFileSource(fs, codegen, modulePath, external, d.srcDirs)

		if placeholder == 0 || !placeholder.isSource() {
			throwFmt("LARGE_FILES: neither actual data nor placeholder is found for \"%s\"", arg)
		}

		storage, resource := readLargeFileDescriptor(fs, placeholder, external)
		fromArgs := []string{"FILE", resource, "OUT_NOAUTO", arg}

		if slash := strings.LastIndexByte(arg, '/'); slash >= 0 {
			fromArgs = append(fromArgs, "RENAME", arg[slash+1:])
		}

		switch storage {
		case "sandbox":
			from := parseFromSandbox(internAnys(fromArgs), stmt.Line)

			from.DeclSeq = d.nextDeclSeq()
			d.fromSandboxes = append(d.fromSandboxes, from)
		default:
			throwFmt("LARGE_FILES: FROM_%s lowering is not modelled (placeholder \"%s\")", strings.ToUpper(storage), external)
		}
	}
}
