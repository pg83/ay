package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

func cmdCasAnalyze(_ GlobalFlags, args []string) int {
	if len(args) == 0 || args[0] != "analyze" {
		throwFmt("usage: ay dev cas analyze [--chunk=N] <cas-dir>")
	}

	chunkAvg := 8192

	var minLen int64

	casDir := ""

	for _, a := range args[1:] {
		switch {
		case strings.HasPrefix(a, "--chunk="):
			chunkAvg = throw2(strconv.Atoi(strings.TrimPrefix(a, "--chunk=")))
		case strings.HasPrefix(a, "--min-len="):
			minLen = throw2(strconv.ParseInt(strings.TrimPrefix(a, "--min-len="), 10, 64))
		case strings.HasPrefix(a, "-"):
			throwFmt("cas analyze: unknown flag %q", a)
		default:
			casDir = a
		}
	}

	if casDir == "" {
		throwFmt("cas analyze: missing <cas-dir>")
	}

	if chunkAvg < 64 {
		throwFmt("cas analyze: --chunk must be >= 64")
	}

	return casAnalyze(casDir, chunkAvg, minLen)
}

func casAnalyze(casDir string, chunkAvg int, minLen int64) int {
	type chunkInfo struct {
		hash [32]byte
		size int64
	}

	fileCh := make(chan string, 256)
	hashCh := make(chan chunkInfo, 1<<14)

	var files atomic.Int64

	go func() {
		defer close(fileCh)

		try(func() {
			throw(filepath.WalkDir(casDir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}

				if !d.Type().IsRegular() || throw2(d.Info()).Size() < minLen {
					return nil
				}

				files.Add(1)
				fileCh <- path

				return nil
			}))
		}).catch(func(e *Exception) {
			fmt.Fprintf(os.Stderr, "cas analyze: walk: %s\n", e.error())
		})
	}()

	var wg sync.WaitGroup

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for path := range fileCh {
				try(func() {
					data := throw2(os.ReadFile(path))

					cdcChunks(data, chunkAvg, func(chunk []byte) {
						hashCh <- chunkInfo{hash: sha256.Sum256(chunk), size: int64(len(chunk))}
					})
				}).catch(func(e *Exception) {
					fmt.Fprintf(os.Stderr, "cas analyze: skip %s: %s\n", path, e.error())
				})
			}
		}()
	}

	go func() {
		wg.Wait()
		close(hashCh)
	}()

	seen := make(map[[32]byte]struct{})

	var totalBytes, totalChunks, uniqueBytes int64

	for ci := range hashCh {
		totalChunks++
		totalBytes += ci.size

		if _, ok := seen[ci.hash]; !ok {
			seen[ci.hash] = struct{}{}
			uniqueBytes += ci.size
		}
	}

	if totalChunks == 0 {
		fmt.Printf("cas analyze: %s — no files >= min-len\n", casDir)

		return 0
	}

	filesN := files.Load()
	uniqueChunks := int64(len(seen))

	const refBytes = 32

	refOverhead := totalChunks * refBytes
	chunkedStore := uniqueBytes + refOverhead

	pct := func(part, whole int64) float64 {
		if whole == 0 {
			return 0
		}

		return 100 * float64(part) / float64(whole)
	}

	fmt.Printf("cas analyze: %s   (content-defined chunking, avg≈%d B, min-len %s)\n", casDir, chunkAvg, humanBytes(minLen))
	fmt.Printf("  files            %d   (>= min-len)\n", filesN)
	fmt.Printf("  total size       %s   (whole-file CAS, already content-deduped)\n", humanBytes(totalBytes))
	fmt.Printf("  chunks           %d   (avg %d B)\n", totalChunks, totalBytes/totalChunks)
	fmt.Printf("  unique chunks    %d   (%.1f%% of chunks)\n", uniqueChunks, pct(uniqueChunks, totalChunks))
	fmt.Printf("  unique data      %s   (%.1f%% of total)\n", humanBytes(uniqueBytes), pct(uniqueBytes, totalBytes))
	fmt.Printf("  + chunk refs     %s   (%d × %d B)\n", humanBytes(refOverhead), totalChunks, refBytes)
	fmt.Printf("  chunked store    %s   (%.1f%% of total)\n", humanBytes(chunkedStore), pct(chunkedStore, totalBytes))
	fmt.Printf("  saving           %s   (%.1f%%)\n", humanBytes(totalBytes-chunkedStore), pct(totalBytes-chunkedStore, totalBytes))

	return 0
}

func cdcChunks(data []byte, avg int, yield func([]byte)) {
	minSize := avg / 4

	if minSize < 64 {
		minSize = 64
	}

	maxSize := avg * 8
	mod := uint64(avg)
	start := 0

	var h uint64

	for i := 0; i < len(data); i++ {
		h = (h << 1) + gearTable[data[i]]

		size := i - start + 1

		if size < minSize {
			continue
		}

		if h%mod == 0 || size >= maxSize {
			yield(data[start : i+1])
			start = i + 1
			h = 0
		}
	}

	if start < len(data) {
		yield(data[start:])
	}
}

var gearTable = func() [256]uint64 {
	var t [256]uint64

	x := uint64(0x2545F4914F6CDD1D)

	for i := range t {
		x += 0x9E3779B97F4A7C15

		z := x

		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB

		t[i] = z ^ (z >> 31)
	}

	return t
}()

func humanBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0

	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
