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
	deduper DeDuper
	live    bool
	sites   []string
}

func (p *DeDuperPool) get() *DeDuper {
	if dedupDebug {
		p.sites = append(p.sites, dedupSite())

		if len(p.sites) >= 2 {
			dedupReport(p.sites)
		}
	}

	if p.live {
		sites := strings.Join(p.sites, " || ")

		if dedupDebug {
			p.sites = p.sites[:len(p.sites)-1]
		}

		panic("deduper already borrowed: " + sites)
	}

	p.live = true
	p.deduper.reset()

	return &p.deduper
}

func (p *DeDuperPool) with(f func(*DeDuper)) {
	deduper := p.get()

	defer p.put(deduper)

	f(deduper)
}

func (p *DeDuperPool) put(d *DeDuper) {
	if !p.live || d != &p.deduper {
		panic("deduper pool: invalid return")
	}

	if dedupDebug && len(p.sites) > 0 {
		p.sites = p.sites[:len(p.sites)-1]
	}

	p.live = false
}

func dedupSite() string {
	pc := make([]uintptr, 4)
	n := runtime.Callers(4, pc)
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
	if int(id) >= dd.gen.len() {
		dd.gen.ensureLen(int(id) + 1)
	}

	gen := dd.gen.s

	if gen[id] == dd.epoch {
		return false
	}

	gen[id] = dd.epoch

	return true
}

func (dd *DeDuper) has(id uint32) bool {
	return dd.gen.s[id] == dd.epoch
}

func dedupInPlace[T IdKey](xs []T) []T {
	var out []T

	dedupers.with(func(deduper *DeDuper) {
		out = dedupInPlaceWith(deduper, xs)
	})

	return out
}

func dedupInPlaceWith[T IdKey](deduper *DeDuper, xs []T) []T {
	deduper.reset()

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

	var out []T

	dedupers.with(func(deduper *DeDuper) {
		out = make([]T, 0, total)

		for _, l := range lists {
			for _, x := range l {
				if deduper.add(x.strID()) {
					out = append(out, x)
				}
			}
		}
	})

	return out
}
