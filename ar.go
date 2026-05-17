package main

import "strings"

// ar.go — emitter for AR (archive) nodes.
//
// cmd_args preserves declaration (SRCS) order; inputs sorts the .o set
// alphabetically. Output path uses ArchiveName(instance.Path) regardless of
// PIC: the host/target axis is captured by host_platform + tags=["tool"],
// not by an archive-name suffix (e.g. build/cow/on's host AR still emits
// libbuild-cow-on.a, not libbuild-cow-on.pic.a).

// isBuildRootCodegenProduct reports whether a member-input path is a
// BUILD_ROOT-rooted codegen artifact that must not appear in AR `inputs`.
// Reference constrains BUILD_ROOT entries in AR `inputs` to .o files;
// generated sources/headers are wired through the constituent CC only.
func isBuildRootCodegenProduct(p string) bool {
	if !strings.HasPrefix(p, "$(B)/") {
		return false
	}
	// .o is the only BUILD_ROOT extension carried in AR `inputs`. The
	// HasSuffix(".o") test covers .cpp.o, .pic.o, and .S.o.
	return !strings.HasSuffix(p, ".o")
}

// isBuildRootCodegenProductRel is the VFS-internal form of
// isBuildRootCodegenProduct. The caller has already verified the path is
// BUILD_ROOT-anchored (VFS.IsBuild()); this checks only the suffix rule.
func isBuildRootCodegenProductRel(rel string) bool {
	return !strings.HasSuffix(rel, ".o")
}

// archiveNameWithPrefix returns the archive base name using the given
// prefix (e.g. "lib", "libpy3", "libpy3c"). Single special case: "util" →
// "<prefix>yutil.a"; "util" is never a Python module in practice.
func archiveNameWithPrefix(moduleDir, prefix string) string {
	if moduleDir == "util" {
		// The "y" infix is baked into the util special-case; preserve
		// it relative to whatever prefix the caller supplies.
		base := "libyutil.a"
		return prefix + base[len("lib"):]
	}

	parts := strings.Split(moduleDir, "/")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}

	return prefix + strings.Join(parts, "-") + ".a"
}

func archiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	if name != "" {
		return prefix + name + ".a"
	}

	return archiveNameWithPrefix(moduleDir, prefix)
}

// ArchiveName returns the on-disk archive base name for a module dir.
//
// Rule (from upstream devtools/ymake/module_confs.cpp:48-57,
// SetDefaultRealprjnameImpl(mod, depth=2) as used by ThreeDirNames):
// join the last min(3, depth) path components with "-", prefix "lib",
// suffix ".a". Single special case: "util" → "libyutil.a".
func ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "lib")
}

// Py3ArchiveName returns the archive base name for a PY3_LIBRARY
// module (prefix "libpy3"). Used by Python library types whose
// reference graph uses the "libpy3<name>.a" naming convention.
func Py3ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "libpy3")
}

// Py3cArchiveName returns the archive base name for a PY23_NATIVE_LIBRARY
// module (prefix "libpy3c"). Used by native Python C-extension library
// types whose reference graph uses the "libpy3c<name>.a" convention.
func Py3cArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "libpy3c")
}

// globalArchiveName returns the archive base name for a module's
// GLOBAL_SRCS archive. The name follows the same prefix-truncation
// rules as ArchiveName, but the ".a" suffix is replaced with
// ".global.a".
func globalArchiveName(moduleDir string) string {
	base := ArchiveName(moduleDir)

	return base[:len(base)-2] + ".global.a"
}

// globalArchiveNameWithPrefix is like globalArchiveName but uses an
// explicit prefix (e.g. "libpy3") instead of "lib".
func globalArchiveNameWithPrefix(moduleDir, prefix string) string {
	base := archiveNameWithPrefix(moduleDir, prefix)

	return base[:len(base)-2] + ".global.a"
}

func globalArchiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	base := archiveNameWithPrefixOrName(moduleDir, prefix, name)

	return base[:len(base)-2] + ".global.a"
}
