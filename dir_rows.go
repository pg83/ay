package main

var dirRows DirRows

type DirRows struct {
	idx   DenseIndex
	count uint32
}

func (d *DirRows) rowOf(dir STR) uint32 {
	return d.idx.get(uint32(dir))
}

func (d *DirRows) rowFor(dir STR) uint32 {
	if row := d.idx.get(uint32(dir)); row != 0 {
		return row
	}

	d.count++
	d.idx.put(uint32(dir), d.count)

	return d.count
}
