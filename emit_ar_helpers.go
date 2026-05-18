package main

import "strings"

// isBuildRootCodegenProduct reports whether a member-input path is a
// BUILD_ROOT-rooted codegen artifact that must not appear in AR `inputs`.
// Reference constrains BUILD_ROOT entries in AR `inputs` to .o files;
// generated sources/headers are wired through the constituent CC only.
func isBuildRootCodegenProduct(p string) bool {
	if !strings.HasPrefix(p, "$(B)/") {
		return false
	}
	return !strings.HasSuffix(p, ".o")
}

// isBuildRootCodegenProductRel is the VFS-internal form of
// isBuildRootCodegenProduct. The caller has already verified the path is
// BUILD_ROOT-anchored (VFS.IsBuild()); this checks only the suffix rule.
func isBuildRootCodegenProductRel(rel string) bool {
	return !strings.HasSuffix(rel, ".o")
}

// archiveNameWithPrefix returns the archive base name using the given
// prefix (e.g. "lib", "libpy3", "libpy3c"). Single special case: "util" ->
// "<prefix>yutil.a"; "util" is never a Python module in practice.
func archiveNameWithPrefix(moduleDir, prefix string) string {
	if moduleDir == "util" {
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
func ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "lib")
}

// Py3ArchiveName returns the archive base name for a PY3_LIBRARY module.
func Py3ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "libpy3")
}

// Py3cArchiveName returns the archive base name for a PY23_NATIVE_LIBRARY module.
func Py3cArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "libpy3c")
}

// globalArchiveName returns the archive base name for a module's GLOBAL_SRCS archive.
func globalArchiveName(moduleDir string) string {
	base := ArchiveName(moduleDir)
	return base[:len(base)-2] + ".global.a"
}

// globalArchiveNameWithPrefix is like globalArchiveName but uses an explicit prefix.
func globalArchiveNameWithPrefix(moduleDir, prefix string) string {
	base := archiveNameWithPrefix(moduleDir, prefix)
	return base[:len(base)-2] + ".global.a"
}

func globalArchiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	base := archiveNameWithPrefixOrName(moduleDir, prefix, name)
	return base[:len(base)-2] + ".global.a"
}
