package main

// Closure is both the stored form of an include closure and the view over it:
// a root file (self) plus the non-empty residue buckets of its transitive
// closure. Each bucket is a hash-consed []VFS shared through BucketCache; the
// per-closure bucket slice is bump-allocated, so the struct itself is thin.
type Closure struct {
	self    VFS
	buckets [][]VFS
}

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

func (cl Closure) spliceInto(cs *IdSet, block []VFS, k int) int {
	k = cs.spliceOne(cl.self, block, k)

	for _, b := range cl.buckets {
		k = cs.spliceNew(b, block, k)
	}

	return k
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
