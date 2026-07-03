package main

const closureBuckets = 16

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
	if len(elems) == 0 {
		return nil
	}

	cell, found := c.intern.cell(bucketHash(elems))
	if found {
		return *cell
	}

	slice := c.pool.list(elems...)
	*cell = slice

	return slice
}

func (c *BucketCache) resetScratch() {
	for r := range c.scratch {
		c.scratch[r] = c.scratch[r][:0]
	}
}

// internScratch hash-conses the scratch buckets into a Closure. Buckets are
// positional: slot r holds the residue with strID&(closureBuckets-1)==r (nil if
// empty), so a stored closure's buckets[r] merges index-to-index into another
// closure's accumulator slot r without recomputing the bucket. The scratch is
// the closure accumulator: dfs fills it directly, storeBuckets from a flat list.
func (c *BucketCache) internScratch(self VFS) Closure {
	buckets := c.chunks.alloc(closureBuckets)

	for r := 0; r < closureBuckets; r++ {
		buckets[r] = c.internBucket(c.scratch[r])
	}

	c.chunks.commit(closureBuckets)

	return Closure{self: self, buckets: buckets[:closureBuckets:closureBuckets]}
}

func (c *BucketCache) storeBuckets(self VFS, rest []VFS) Closure {
	c.resetScratch()

	for _, v := range rest {
		r := v.strID() & (closureBuckets - 1)
		c.scratch[r] = append(c.scratch[r], v)
	}

	return c.internScratch(self)
}
