package main

import (
	"bufio"
	"os"
	"strconv"
)

var closureDump = newClosureDump()

type ClosureDump struct {
	cl   *bufio.Writer
	cs   *bufio.Writer
	ck   *bufio.Writer
	fcl  *os.File
	fcs  *os.File
	fck  *os.File
	buf  []byte
	next int32
	gmap map[*IncludeScanner][]int32
}

func newClosureDump() *ClosureDump {
	prefix := os.Getenv("AY_CLOSURE_DUMP")

	if prefix == "" {
		return nil
	}

	fcl := throw2(os.Create(prefix + ".closures"))
	fcs := throw2(os.Create(prefix + ".builds"))
	fck := throw2(os.Create(prefix + ".keys"))

	return &ClosureDump{
		cl:   bufio.NewWriterSize(fcl, 1<<20),
		cs:   bufio.NewWriterSize(fcs, 1<<20),
		ck:   bufio.NewWriterSize(fck, 1<<20),
		fcl:  fcl,
		fcs:  fcs,
		fck:  fck,
		gmap: map[*IncludeScanner][]int32{},
	}
}

func (d *ClosureDump) writeInt(w *bufio.Writer, x uint32, first bool) {
	if !first {
		w.WriteByte(' ')
	}
	d.buf = strconv.AppendUint(d.buf[:0], uint64(x), 10)
	w.Write(d.buf)
}

func (d *ClosureDump) recordClosure(s *IncludeScanner, ref ClosureRef, key uint32, cl []VFS, crefs []ClosureRef) {
	g := d.gmap[s]
	for len(g) <= int(ref) {
		g = append(g, -1)
	}
	g[ref] = d.next
	d.gmap[s] = g
	d.next++

	for i, v := range cl {
		d.writeInt(d.cl, v.strID(), i == 0)
	}
	d.cl.WriteByte('\n')

	for i, r := range crefs {
		d.writeInt(d.cs, uint32(g[r]), i == 0)
	}
	d.cs.WriteByte('\n')

	d.writeInt(d.ck, key, true)
	d.ck.WriteByte('\n')
}

func (d *ClosureDump) close() {
	if d == nil {
		return
	}

	d.cl.Flush()
	d.cs.Flush()
	d.ck.Flush()
	d.fcl.Close()
	d.fcs.Close()
	d.fck.Close()
}
