package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var dedupers DeDuperPool

var dedupDebug = os.Getenv("AY_DEBUG_DEDUP") != ""

type DeDuperPool struct {
	free  []*DeDuper
	sites []string
}

func (p *DeDuperPool) get() *DeDuper {
	if dedupDebug {
		p.sites = append(p.sites, dedupSite())

		if len(p.sites) >= 2 {
			dedupReport(p.sites)
		}
	}

	if n := len(p.free); n > 0 {
		d := p.free[n-1]

		p.free = p.free[:n-1]

		d.reset()

		return d
	}

	d := &DeDuper{}

	d.reset()

	return d
}

func (p *DeDuperPool) put(d *DeDuper) {
	if dedupDebug && len(p.sites) > 0 {
		p.sites = p.sites[:len(p.sites)-1]
	}

	p.free = append(p.free, d)

	if len(p.free) > 2 {
		panic("deduper pool: more than 2 concurrently live: " + strings.Join(p.sites, " || "))
	}
}

func dedupSite() string {
	pc := make([]uintptr, 4)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])

	var parts []string

	for i := 0; i < 3; i++ {
		f, more := frames.Next()

		parts = append(parts, fmt.Sprintf("%s@%s:%d", strings.TrimPrefix(f.Function, "main."), filepath.Base(f.File), f.Line))

		if !more {
			break
		}
	}

	return strings.Join(parts, " <- ")
}

var dedupStacks = map[string]bool{}

func dedupReport(sites []string) {
	sig := strings.Join(sites, " || ")

	if dedupStacks[sig] {
		return
	}

	dedupStacks[sig] = true

	fmt.Fprintf(os.Stderr, "=== %d live dedupers ===\n", len(sites))

	for i, s := range sites {
		fmt.Fprintf(os.Stderr, "  #%d  %s\n", i+1, s)
	}
}

type IdKey interface {
	~uint32
	strID() uint32
}

type DeDuper struct {
	gen   Vec[uint32]
	epoch uint32
}

func (dd *DeDuper) reset() {
	if dd.gen.freshLen(int(vfsBound())) {
		dd.epoch = 1

		return
	}

	dd.epoch++

	if dd.epoch == 0 {
		clear(dd.gen.s)

		dd.epoch = 1
	}
}

func (dd *DeDuper) add(id uint32) bool {
	dd.gen.ensureLen(int(id) + 1)

	if dd.gen.s[id] == dd.epoch {
		return false
	}

	dd.gen.s[id] = dd.epoch

	return true
}

func (dd *DeDuper) has(id uint32) bool {
	return dd.gen.s[id] == dd.epoch
}

func dedupInPlace[T IdKey](xs []T) []T {
	deduper := dedupers.get()

	defer dedupers.put(deduper)

	out := xs[:0]

	for _, x := range xs {
		if deduper.add(x.strID()) {
			out = append(out, x)
		}
	}

	return out
}

func dedup[T IdKey](lists ...[]T) []T {
	total := 0

	for _, l := range lists {
		total += len(l)
	}

	if total == 0 {
		return nil
	}

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	out := make([]T, 0, total)

	for _, l := range lists {
		for _, x := range l {
			if deduper.add(x.strID()) {
				out = append(out, x)
			}
		}
	}

	return out
}
