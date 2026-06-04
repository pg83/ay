package main

import "strings"

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

func ArchiveName(moduleDir string) string {
	return archiveNameWithPrefix(moduleDir, "lib")
}

func globalArchiveNameWithPrefixOrName(moduleDir, prefix, name string) string {
	base := archiveNameWithPrefixOrName(moduleDir, prefix, name)
	return base[:len(base)-2] + ".global.a"
}
