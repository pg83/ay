package main

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
