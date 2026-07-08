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

func filterSourceVFS(na *NodeArenas, vs []VFS) []VFS {
	n := 0

	for _, v := range vs {
		if v.isSource() {
			n++
		}
	}

	if n == len(vs) {
		return vs
	}

	out := na.vfs.alloc(n)[:0]

	for _, v := range vs {
		if v.isSource() {
			out = append(out, v)
		}
	}

	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func eachBucketVFS(chunks [][]VFS, fn func(VFS)) {
	for _, ch := range chunks {
		for _, v := range ch {
			fn(v)
		}
	}
}

func collectBucketVFS(na *NodeArenas, chunks [][]VFS, keep func(VFS) bool) []VFS {
	total := 0

	for _, b := range chunks {
		total += len(b)
	}

	out := na.vfs.alloc(total)[:0]

	eachBucketVFS(chunks, func(v VFS) {
		if keep(v) {
			out = append(out, v)
		}
	})

	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}
