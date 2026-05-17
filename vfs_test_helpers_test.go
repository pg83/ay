package main

// ParseVFSOrSource is a test-only shim for legacy fixtures that still
// spell VFS values either as canonical "$(S)/..." / "$(B)/..." strings
// or as bare source-relative paths.
func ParseVFSOrSource(s string) VFS {
	if v, ok := ParseVFS(s); ok {
		return v
	}

	return Source(s)
}

// VFSesFromStrings is the bulk test helper variant of ParseVFSOrSource.
func VFSesFromStrings(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}

// ToVFSSlice keeps old emitter/node test fixtures readable without
// forcing every literal to be rewritten during production refactors.
func ToVFSSlice(ss []string) []VFS {
	out := make([]VFS, len(ss))
	for i, s := range ss {
		out[i] = ParseVFSOrSource(s)
	}

	return out
}
