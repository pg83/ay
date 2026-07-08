package main

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

func (cl Closure) collect(na *NodeArenas, keep func(VFS) bool) []VFS {
	out := na.vfs.alloc(cl.len())[:0]

	cl.each(func(v VFS) {
		if keep(v) {
			out = append(out, v)
		}
	})

	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func (cl Closure) spliceInto(cs *IdSet, block []VFS, k int) int {
	k = cs.spliceOne(cl.self, block, k)

	for _, b := range cl.buckets {
		k = cs.spliceNew(b, block, k)
	}

	return k
}
