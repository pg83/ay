package main

const closureBuckets = 16

// Closure is both the stored form of an include closure and the view over it:
// a root file (self) plus the non-empty residue buckets of its transitive
// closure. Each bucket is a hash-consed []VFS shared through BucketCache; the
// per-closure bucket slice is bump-allocated, so the struct itself is thin.
type Closure struct {
	self    VFS
	buckets [][]VFS
}

// BucketCache is the shared content pool: hash-consed bucket contents plus the
// bump arenas backing them. It is shared across the target and host scanners
// because bucket content is content-addressed and immutable. The per-file
// closure index lives on IncludeScanner instead, because the same file resolves
// to a different closure per platform.
type BucketCache struct {
	chunks  *BumpAllocator[[]VFS]
	pool    *BumpAllocator[VFS]
	intern  *IntMap[[]VFS]
	scratch [closureBuckets][]VFS
}

func newBucketCache() *BucketCache {
	return &BucketCache{
		chunks: newBumpAllocator[[]VFS](1 << 12),
		pool:   newBumpAllocator[VFS](1 << 19),
		intern: newIntMap[[]VFS](1 << 16),
	}
}

func bucketHash(elems []VFS) uint64 {
	h := mix64(uint64(len(elems)))

	for _, v := range elems {
		h += mix64(uint64(v))
	}

	if h == 0 {
		h = 1
	}

	return h
}

// internBucket hash-conses a bucket's contents into the shared pool, returning
// the shared slice so identical buckets across closures share one backing.
func (c *BucketCache) internBucket(elems []VFS) []VFS {
	cell, found := c.intern.cell(bucketHash(elems))
	if found {
		return *cell
	}

	slice := c.pool.list(elems...)
	*cell = slice

	return slice
}

func (c *BucketCache) storeBuckets(self VFS, rest []VFS) Closure {
	for r := range c.scratch {
		c.scratch[r] = c.scratch[r][:0]
	}

	for _, v := range rest {
		r := v.strID() & (closureBuckets - 1)
		c.scratch[r] = append(c.scratch[r], v)
	}

	n := 0

	for r := 0; r < closureBuckets; r++ {
		if len(c.scratch[r]) > 0 {
			n++
		}
	}

	buckets := c.chunks.alloc(n)
	k := 0

	for r := 0; r < closureBuckets; r++ {
		if len(c.scratch[r]) == 0 {
			continue
		}

		buckets[k] = c.internBucket(c.scratch[r])
		k++
	}

	c.chunks.commit(n)

	return Closure{self: self, buckets: buckets[:n:n]}
}

func (cl Closure) spliceInto(cs *IdSet, block []VFS, k int) int {
	k = cs.spliceOne(cl.self, block, k)

	for _, b := range cl.buckets {
		k = cs.spliceNew(b, block, k)
	}

	return k
}
