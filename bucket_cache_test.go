package main

import "testing"

func TestBucketHashSeparatesPowerMomentCollision(t *testing.T) {
	a := []VFS{512, 672, 832, 992}
	b := []VFS{544, 608, 896, 960}
	a1, a2 := bucketHash(a)
	b1, b2 := bucketHash(b)

	if a1 == b1 && a2 == b2 {
		t.Fatal("bucketHash collides on distinct sets with equal count/sum/xor/square/cube")
	}
}

func TestBucketCacheStoresBuildInputsInOneBucket(t *testing.T) {
	var rest []VFS

	for rel := uint32(1); rel <= closureSourceBuckets; rel++ {
		rest = append(rest, VFS(rel<<1))
	}

	for rel := uint32(1); rel <= 4; rel++ {
		rest = append(rest, VFS(rel<<1|1))
	}

	buckets := newBucketCache().storeBuckets(0, rest).bucketList()

	if len(buckets) != closureBuckets {
		t.Fatalf("got %d buckets, want %d", len(buckets), closureBuckets)
	}

	for _, v := range buckets[0] {
		if !v.isBuild() {
			t.Fatalf("build bucket contains source input %v", v)
		}
	}

	for _, bucket := range buckets[1:] {
		for _, v := range bucket {
			if !v.isSource() {
				t.Fatalf("source bucket contains build input %v", v)
			}
		}
	}
}

func TestBucketCacheVisitsInternedBucketOncePerEpoch(t *testing.T) {
	c := newBucketCache()
	bucket := c.storeBuckets(0, []VFS{2, 4}).bucketList()[0]

	if !c.firstBucketVisit(bucket) {
		t.Fatal("first bucket visit was rejected")
	}

	if c.firstBucketVisit(bucket) {
		t.Fatal("second bucket visit in the same epoch was accepted")
	}

	c.resetScratch()

	if !c.firstBucketVisit(bucket) {
		t.Fatal("bucket visit after reset was rejected")
	}
}

func TestBucketCacheClearsEpochsOnWraparound(t *testing.T) {
	c := newBucketCache()
	bucket := c.storeBuckets(0, []VFS{2, 4}).bucketList()[0]

	*bucketEpochCell(bucket) = 1
	c.bucketEpoch = ^uint32(0)
	c.resetScratch()

	if c.bucketEpoch != 1 {
		t.Fatalf("wrapped epoch = %d, want 1", c.bucketEpoch)
	}

	if !c.firstBucketVisit(bucket) {
		t.Fatal("stale wrapped epoch was not cleared")
	}
}
