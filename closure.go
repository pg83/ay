package main

type BucketList [][]VFS

type Closure struct {
	self    VFS
	buckets *BucketList
}

func (cl Closure) bucketList() [][]VFS {
	if cl.buckets == nil {
		return nil
	}

	return *cl.buckets
}

func (cl Closure) len() int {
	n := 0

	if cl.self != 0 {
		n++
	}

	for _, b := range cl.bucketList() {
		n += len(b)
	}

	return n
}

func (cl Closure) each(fn func(VFS)) {
	if cl.self != 0 {
		fn(cl.self)
	}

	eachBucketVFS(cl.bucketList(), fn)
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

	for _, b := range cl.bucketList() {
		k = cs.spliceNew(b, block, k)
	}

	return k
}
