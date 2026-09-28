package main

var dedupers DeDuperPool

type DeDuperPool struct {
	deduper DeDuper
	live    bool
}

func (p *DeDuperPool) get() *DeDuper {
	assert(!p.live, "deduper already borrowed")

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
	assert(p.live && d == &p.deduper, "deduper pool: invalid return")

	p.live = false
}

type IdKey interface {
	~uint32
	strID() uint32
}

type DeDuper struct {
	gen   Vec[uint16]
	epoch uint16
}

func (dd *DeDuper) reset() {
	if dd.gen.freshLen(int(vfsBound())) {
		dd.epoch = uint16(chaosEpochStart.number(1))

		return
	}

	dd.epoch++

	if dd.epoch == 0 {
		clear(dd.gen.s)

		dd.epoch = 1
	}
}

func (dd *DeDuper) add(id uint32) bool {
	if int(id) >= len(dd.gen.s) {
		dd.grow(id)
	}

	cell := unsafeAt(dd.gen.s, uint64(id))

	if *cell == dd.epoch {
		return false
	}

	*cell = dd.epoch

	return true
}

func (dd *DeDuper) addStable(id uint32) bool {
	cell := unsafeAt(dd.gen.s, uint64(id))

	if *cell == dd.epoch {
		return false
	}

	*cell = dd.epoch

	return true
}

//go:noinline
func (dd *DeDuper) grow(id uint32) {
	dd.gen.ensureLen(int(id) + 1)
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
		if deduper.addStable(x.strID()) {
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
				if deduper.addStable(x.strID()) {
					out = append(out, x)
				}
			}
		}
	})

	return out
}
