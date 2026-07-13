package main

import (
	"unsafe"
)

const (
	closureSourceBuckets = 8
	closureBuckets       = 1 + closureSourceBuckets
)

func closureBucketIndex(v VFS) int {
	x := uint32(v)

	if x&1 != 0 {
		return 0
	}

	return 1 + int((x>>1)&(closureSourceBuckets-1))
}

func bucketListHash(buckets [][]VFS) (uint64, uint64) {
	h1 := uint64(0x9e3779b97f4a7c15) ^ uint64(len(buckets))
	h2 := uint64(0xd1b54a32d192ed03) + uint64(len(buckets))

	for _, bucket := range buckets {
		p := uint64(uintptr(unsafe.Pointer(unsafe.SliceData(bucket))))
		x := p ^ uint64(len(bucket))*0x94d049bb133111eb

		h1 = mix64(h1 ^ x)
		h2 = mix64(h2 + x + 0x9e3779b97f4a7c15)
	}

	if h1 == 0 {
		h1 = 1
	}

	if h2 == 0 {
		h2 = 1
	}

	return h1, h2
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
	bucketEpoch  uint32
}

func newBucketCache() *BucketCache {
	c := &BucketCache{
		chunks:       newBumpAllocator[[]VFS](),
		lists:        newBumpAllocator[BucketList](),
		pool:         newBumpAllocator[VFS](),
		intern:       newIntValueMap[BucketVal](1 << 18),
		overflow:     newIntValueMap[BucketVal](1 << 4),
		listIntern:   newIntValueMap[BucketListVal](1 << 16),
		listOverflow: newIntValueMap[BucketListVal](1 << 4),
		bucketEpoch:  1,
	}

	return c
}

func (c *BucketCache) internBucketList(buckets [][]VFS) *BucketList {
	if len(buckets) == 0 {
		return nil
	}

	h1, h2 := bucketListHash(buckets)

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

		slice := c.storeBucket(elems)

		*cell2 = BucketVal{verify: h1, slice: slice}

		return slice
	}

	slice := c.storeBucket(elems)

	*cell = BucketVal{verify: h2, slice: slice}

	return slice
}

func (c *BucketCache) storeBucket(elems []VFS) []VFS {
	n := len(elems) + 1
	block := c.pool.alloc(n)[:n]

	// Keep the mutable visit epoch outside the immutable slice: it must not
	// affect bucket hashing, equality, capacity, or downstream input chunks.
	block[0] = 0
	copy(block[1:], elems)
	c.pool.commit(n)

	return block[1:n:n]
}

func bucketEpochCell(bucket []VFS) *uint32 {
	// Every interned bucket is returned by storeBucket with this arena-backed
	// header immediately before its first element.
	return (*uint32)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(bucket)), -int(unsafe.Sizeof(VFS(0)))))
}

func (c *BucketCache) firstBucketVisit(bucket []VFS) bool {
	epoch := bucketEpochCell(bucket)

	if *epoch == c.bucketEpoch {
		return false
	}

	*epoch = c.bucketEpoch

	return true
}

func (c *BucketCache) clearBucketEpochs() {
	for i := range c.intern.vals.s {
		*bucketEpochCell(c.intern.vals.s[i].slice) = 0
	}

	for i := range c.overflow.vals.s {
		*bucketEpochCell(c.overflow.vals.s[i].slice) = 0
	}
}

func (c *BucketCache) resetScratch() {
	for r := range c.scratch {
		c.scratch[r] = c.scratch[r][:0]
	}

	c.bucketEpoch++

	if c.bucketEpoch == 0 {
		c.clearBucketEpochs()
		c.bucketEpoch = 1
	}
}

func (c *BucketCache) spliceOne(seen *IdSet, v VFS) {
	id := v.strID()

	if seen.gen.s[id] == seen.epoch {
		return
	}

	seen.gen.s[id] = seen.epoch
	r := closureBucketIndex(v)
	c.scratch[r] = append(c.scratch[r], v)
}

func (c *BucketCache) spliceBucket(seen *IdSet, bucket []VFS) {
	if len(bucket) == 0 {
		return
	}

	if !c.firstBucketVisit(bucket) {
		return
	}

	gen := unsafe.SliceData(seen.gen.s)
	epoch := seen.epoch
	r := closureBucketIndex(bucket[0])
	dst := c.scratch[r]

	for _, v := range bucket {
		id := v.strID()
		cell := (*uint16)(unsafe.Add(unsafe.Pointer(gen), uintptr(id)*unsafe.Sizeof(epoch)))

		if *cell == epoch {
			continue
		}

		*cell = epoch
		dst = append(dst, v)
	}

	c.scratch[r] = dst
}

func (c *BucketCache) spliceClosure(seen *IdSet, cl Closure) {
	c.spliceOne(seen, cl.self)

	for _, bucket := range cl.bucketList() {
		c.spliceBucket(seen, bucket)
	}
}

func (c *BucketCache) spliceLeaves(seen *IdSet, leaves []VFS) {
	for _, v := range leaves {
		c.spliceOne(seen, v)
	}
}

func (c *BucketCache) buildScratch() []VFS {
	return c.scratch[0]
}

func (c *BucketCache) storeScratch(self VFS) Closure {
	buckets := c.chunks.alloc(closureBuckets)[:0]

	for r := 0; r < closureBuckets; r++ {
		if len(c.scratch[r]) == 0 {
			continue
		}

		buckets = append(buckets, c.internBucket(c.scratch[r]))
	}

	list := c.internBucketList(buckets)

	return Closure{self: self, buckets: list}
}

func (c *BucketCache) storeBuckets(self VFS, rest []VFS) Closure {
	c.resetScratch()

	for _, v := range rest {
		r := closureBucketIndex(v)

		c.scratch[r] = append(c.scratch[r], v)
	}

	return c.storeScratch(self)
}
