package main

func (cl Closure) len() int {
	n := 0

	if cl.self != 0 {
		n++
	}

	for _, b := range cl.buckets {
		n += len(b)
	}

	return n
}

func (cl Closure) each(fn func(VFS)) {
	if cl.self != 0 {
		fn(cl.self)
	}

	eachBucketVFS(cl.buckets, fn)
}

func (cl Closure) collect(keep func(VFS) bool) []VFS {
	var out []VFS

	cl.each(func(v VFS) {
		if keep(v) {
			out = append(out, v)
		}
	})

	return out
}

// eachBucketVFS / collectBucketVFS operate on a closure's bucket chunks
// ([][]VFS) — i.e. the closure without self. Consumers that do not need self
// pass cl.buckets and use these instead of the Closure methods.
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
