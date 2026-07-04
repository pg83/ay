package main

const closureBuckets = 16

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
