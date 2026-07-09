package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

func cmdMaxRSS(g GlobalFlags, args []string) int {
	sub := args

	for i, a := range args {
		if a == "--" {
			sub = args[i+1:]

			break
		}
	}

	if len(sub) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ay dev maxrss -- <command> [args...]")

		return 2
	}

	cmd := exec.Command(sub[0], sub[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "maxrss: %v\n", err)

		return 1
	}

	root := cmd.Process.Pid

	var peak atomic.Uint64

	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		sample := func() {
			if kb := subtreeRSSKB(root); kb > peak.Load() {
				peak.Store(kb)
			}
		}

		sample()

		ticker := time.NewTicker(time.Second)

		defer ticker.Stop()

		for {
			select {
			case <-done:
				return

			case <-ticker.C:
				sample()
			}
		}
	}()

	waitErr := cmd.Wait()

	close(done)
	<-stopped

	kb := peak.Load()

	fmt.Fprintf(os.Stderr, "maxrss (subtree): %d kB (%.1f MiB)\n", kb, float64(kb)/1024)

	var ee *exec.ExitError

	if errors.As(waitErr, &ee) {
		return ee.ExitCode()
	}

	if waitErr != nil {
		fmt.Fprintf(os.Stderr, "maxrss: %v\n", waitErr)

		return 1
	}

	return 0
}

func subtreeRSSKB(root int) uint64 {
	entries, err := os.ReadDir("/proc")

	if err != nil {
		return 0
	}

	children := map[int][]int{}
	rss := map[int]uint64{}

	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())

		if err != nil {
			continue
		}

		ppid, kb, ok := readProcStat(pid)

		if !ok {
			continue
		}

		children[ppid] = append(children[ppid], pid)
		rss[pid] = kb
	}

	var total uint64

	stack := []int{root}

	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		total += rss[pid]
		stack = append(stack, children[pid]...)
	}

	return total
}

func readProcStat(pid int) (ppid int, rssKB uint64, ok bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")

	if err != nil {
		return 0, 0, false
	}

	s := string(data)
	rp := strings.LastIndexByte(s, ')')

	if rp < 0 || rp+2 >= len(s) {
		return 0, 0, false
	}

	fields := strings.Fields(s[rp+2:])

	if len(fields) < 22 {
		return 0, 0, false
	}

	ppid, err = strconv.Atoi(fields[1])

	if err != nil {
		return 0, 0, false
	}

	pages, err := strconv.ParseUint(fields[21], 10, 64)

	if err != nil {
		return 0, 0, false
	}

	return ppid, pages * uint64(os.Getpagesize()) / 1024, true
}
