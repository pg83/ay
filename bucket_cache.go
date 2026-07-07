package main

const closureBuckets = 16

type bucketVal struct {
	verify uint64
	slice  []VFS
}

type BucketCache struct {
	chunks       *BumpAllocator[[]VFS]
	pool         *BumpAllocator[VFS]
	intern       *IntValueMap[bucketVal]
	overflow     *IntValueMap[bucketVal]
	h1Mismatches int
	overflowed   int
	scratch      [closureBuckets][]VFS
}

func newBucketCache() *BucketCache {
	return &BucketCache{
		chunks:   newBumpAllocator[[]VFS](1 << 12),
		pool:     newBumpAllocator[VFS](1 << 19),
		intern:   newIntValueMap[bucketVal](1 << 18),
		overflow: newIntValueMap[bucketVal](1 << 4),
	}
}

func m4(a, b, c, d, e uint32) uint64 {
    return mix64((uint64(a)<<32 | uint64(b)) ^ (uint64(c)<<32 | uint64(d)) ^ uint64(e))
}

func bucketHash(elems []VFS) (uint64, uint64) {
	sm, xr, sq, cb := bucketHashPlatform(elems)

	nm := uint32(len(elems) + 1)
	h1 := m4(sm, xr, sq, cb, nm)
	h2 := m4(xr, sq, cb, nm, sm)

	if h1 == 0 {
		h1 = 1
	}

	if h2 == 0 {
		h2 = 1
	}

	return h1, h2
}

func (c *BucketCache) internBucket(elems []VFS) []VFS {
	h1, h2 := bucketHash(elems)
	cell, found := c.intern.cell(h1)

	if found {
		if cell.verify == h2 {
			return cell.slice
		}

		c.h1Mismatches++

		cell2, found2 := c.overflow.cell(h2)

		if found2 {
			if cell2.verify != h1 {
				throwFmt("BucketCache: bucket hash pair collision (h1=%#x h2=%#x, %d elems)", h1, h2, len(elems))
			}

			return cell2.slice
		}

		c.overflowed++

		slice := c.pool.list(elems...)

		*cell2 = bucketVal{verify: h1, slice: slice}

		return slice
	}

	slice := c.pool.list(elems...)

	*cell = bucketVal{verify: h2, slice: slice}

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
