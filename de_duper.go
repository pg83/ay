package main

var dedupers DeDuperPool

type DeDuperPool struct {
	free []*DeDuper
}

func (p *DeDuperPool) get() *DeDuper {
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
	p.free = append(p.free, d)
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
