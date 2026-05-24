package main

import "strings"

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
