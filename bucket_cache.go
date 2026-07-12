package main

import "github.com/zeebo/xxh3"

const (
	closureSourceBuckets = 8
	closureBuckets       = 1 + closureSourceBuckets
)

func closureBucketIndex(v VFS) int {
	if v.isBuild() {
		return 0
	}

	return 1 + int((v.strID()>>1)&(closureSourceBuckets-1))
}

type BucketVal struct {
	verify uint64
	slice  []VFS
}

type BucketListVal struct {
	verify uint64
	list   *BucketList
}

type BucketCache struct {
	chunks       *BumpAllocator[[]VFS]
	lists        *BumpAllocator[BucketList]
	pool         *BumpAllocator[VFS]
	intern       *IntValueMap[BucketVal]
	overflow     *IntValueMap[BucketVal]
	listIntern   *IntValueMap[BucketListVal]
	listOverflow *IntValueMap[BucketListVal]
	h1Mismatches int
	overflowed   int
	scratch      [closureBuckets][]VFS
}

func newBucketCache() *BucketCache {
	return &BucketCache{
		chunks:       newBumpAllocator[[]VFS](),
		lists:        newBumpAllocator[BucketList](),
		pool:         newBumpAllocator[VFS](),
		intern:       newIntValueMap[BucketVal](1 << 18),
		overflow:     newIntValueMap[BucketVal](1 << 4),
		listIntern:   newIntValueMap[BucketListVal](1 << 16),
		listOverflow: newIntValueMap[BucketListVal](1 << 4),
	}
}

func (c *BucketCache) internBucketList(buckets [][]VFS) *BucketList {
	if len(buckets) == 0 {
		return nil
	}

	sum := xxh3.Hash128(sliceBytes(buckets))
	h1, h2 := sum.Hi, sum.Lo

	if h1 == 0 {
		h1 = 1
	}

	if h2 == 0 {
		h2 = 1
	}

	cell, found := c.listIntern.cell(h1)

	if found {
		if cell.verify == h2 {
			return cell.list
		}

		cell2, found2 := c.listOverflow.cell(h2)

		if found2 {
			if cell2.verify != h1 {
				throwFmt("BucketCache: bucket-list hash pair collision (h1=%#x h2=%#x, %d buckets)", h1, h2, len(buckets))
			}

			return cell2.list
		}

		list := c.commitBucketList(buckets)

		*cell2 = BucketListVal{verify: h1, list: list}

		return list
	}

	list := c.commitBucketList(buckets)

	*cell = BucketListVal{verify: h2, list: list}

	return list
}

func (c *BucketCache) commitBucketList(buckets [][]VFS) *BucketList {
	c.chunks.commit(len(buckets))
	list := c.lists.one()

	*list = BucketList(buckets[:len(buckets):len(buckets)])

	return list
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

		*cell2 = BucketVal{verify: h1, slice: slice}

		return slice
	}

	slice := c.pool.list(elems...)

	*cell = BucketVal{verify: h2, slice: slice}

	return slice
}

func (c *BucketCache) storeBuckets(self VFS, rest []VFS) Closure {
	for r := range c.scratch {
		c.scratch[r] = c.scratch[r][:0]
	}

	for _, v := range rest {
		r := closureBucketIndex(v)

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

	list := c.internBucketList(buckets[:n])

	return Closure{self: self, buckets: list}
}
