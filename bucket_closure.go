package main

const closureBuckets = 16

type BucketClosure struct {
	self    VFS
	buckets [closureBuckets]uint32
}

type BucketCache struct {
	list   [][]VFS
	pool   *BumpAllocator[VFS]
	intern *IntMap[uint32]
}

func newBucketCache() *BucketCache {
	return &BucketCache{
		list:   make([][]VFS, 1, 4096),
		pool:   newBumpAllocator[VFS](1 << 19),
		intern: newIntMap[uint32](1 << 16),
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

func (s *IncludeScanner) internBucket(elems []VFS) uint32 {
	if len(elems) == 0 {
		return 0
	}

	cell, found := s.buckets.intern.cell(bucketHash(elems))
	if found {
		return *cell
	}

	ref := uint32(len(s.buckets.list))
	s.buckets.list = append(s.buckets.list, s.buckets.pool.list(elems...))
	*cell = ref

	return ref
}

func (s *IncludeScanner) storeBuckets(self VFS, rest []VFS) BucketClosure {
	for r := range s.bktScratch {
		s.bktScratch[r] = s.bktScratch[r][:0]
	}

	for _, v := range rest {
		r := v.strID() & (closureBuckets - 1)
		s.bktScratch[r] = append(s.bktScratch[r], v)
	}

	bc := BucketClosure{self: self}

	for r := 0; r < closureBuckets; r++ {
		bc.buckets[r] = s.internBucket(s.bktScratch[r])
	}

	return bc
}

func (s *IncludeScanner) spliceClosure(cref ClosureRef, block []VFS, k int) int {
	bc := s.subgraphClosures[cref]
	k = s.tjc.closure.spliceOne(bc.self, block, k)

	for r := 0; r < closureBuckets; r++ {
		k = s.tjc.closure.spliceNew(s.buckets.list[bc.buckets[r]], block, k)
	}

	return k
}

func (s *IncludeScanner) reconstruct(bc BucketClosure, buf []VFS) []VFS {
	buf = append(buf[:0], bc.self)

	for r := 0; r < closureBuckets; r++ {
		buf = append(buf, s.buckets.list[bc.buckets[r]]...)
	}

	return buf
}
