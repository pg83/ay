package main

func vfsPtr(v VFS) *VFS {
	return &v
}

func cloneVFSs(in []VFS) []VFS {
	return append([]VFS(nil), in...)
}

func containsVFS(xs []VFS, want VFS) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}

	return false
}

func filterSourceVFS(vs []VFS) []VFS {
	n := 0

	for _, v := range vs {
		if v.isSource() {
			n++
		}
	}

	if n == len(vs) {
		return vs
	}

	out := make([]VFS, 0, n)

	for _, v := range vs {
		if v.isSource() {
			out = append(out, v)
		}
	}

	return out
}

func eachBucketVFS(chunks [][]VFS, fn func(VFS)) {
	for _, ch := range chunks {
		for _, v := range ch {
			fn(v)
		}
	}
}

func collectBucketVFS(chunks [][]VFS, keep func(VFS) bool) []VFS {
	var out []VFS

	eachBucketVFS(chunks, func(v VFS) {
		if keep(v) {
			out = append(out, v)
		}
	})

	return out
}
