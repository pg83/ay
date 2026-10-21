package main

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
