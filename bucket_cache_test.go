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
