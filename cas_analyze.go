package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cmdCasAnalyze handles `ay make cas <sub> …`. The only sub is `analyze`: it
// estimates how much the (already whole-file-deduplicated) CAS would shrink if its
// files were split into variable-size, content-defined chunks — the rsync rolling-
// hash idea, a.k.a. content-defined chunking — and identical chunks shared across
// files. A pure read-only analysis; it never touches the store.
func cmdCasAnalyze(args []string) int {
	if len(args) == 0 || args[0] != "analyze" {
		ThrowFmt("usage: ay make cas analyze [--chunk=N] <cas-dir>")
	}

	chunkAvg := 8192
	var minLen int64
	casDir := ""

	for _, a := range args[1:] {
		switch {
		case strings.HasPrefix(a, "--chunk="):
			chunkAvg = Throw2(strconv.Atoi(strings.TrimPrefix(a, "--chunk=")))
		case strings.HasPrefix(a, "--min-len="):
			minLen = Throw2(strconv.ParseInt(strings.TrimPrefix(a, "--min-len="), 10, 64))
		case strings.HasPrefix(a, "-"):
			ThrowFmt("cas analyze: unknown flag %q", a)
		default:
			casDir = a
		}
	}

	if casDir == "" {
		ThrowFmt("cas analyze: missing <cas-dir>")
	}

	if chunkAvg < 64 {
		ThrowFmt("cas analyze: --chunk must be >= 64")
	}

	return casAnalyze(casDir, chunkAvg, minLen)
}

// casAnalyze chunks every CAS file of at least minLen bytes; smaller files are
// skipped and excluded from every stat (the counts are over accounted files only).
func casAnalyze(casDir string, chunkAvg int, minLen int64) int {
	seen := make(map[[32]byte]int64) // chunk content hash -> chunk size (dedup set)

	var files, totalBytes, totalChunks int64

	Throw(filepath.WalkDir(casDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		if Throw2(d.Info()).Size() < minLen {
			return nil
		}

		data := Throw2(os.ReadFile(path))
		files++
		totalBytes += int64(len(data))

		cdcChunks(data, chunkAvg, func(chunk []byte) {
			totalChunks++

			h := sha256.Sum256(chunk)

			if _, ok := seen[h]; !ok {
				seen[h] = int64(len(chunk))
			}
		})

		return nil
	}))

	if totalChunks == 0 {
		fmt.Printf("cas analyze: %s — no files\n", casDir)

		return 0
	}

	var uniqueBytes int64

	for _, sz := range seen {
		uniqueBytes += sz
	}

	uniqueChunks := int64(len(seen))

	// A chunked store keeps the unique chunk bodies plus, per chunk occurrence, a
	// reference (the chunk's 32-byte hash) in the file's chunk list.
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
	fmt.Printf("  files            %d   (>= min-len)\n", files)
	fmt.Printf("  total size       %s   (whole-file CAS, already content-deduped)\n", humanBytes(totalBytes))
	fmt.Printf("  chunks           %d   (avg %d B)\n", totalChunks, totalBytes/totalChunks)
	fmt.Printf("  unique chunks    %d   (%.1f%% of chunks)\n", uniqueChunks, pct(uniqueChunks, totalChunks))
	fmt.Printf("  unique data      %s   (%.1f%% of total)\n", humanBytes(uniqueBytes), pct(uniqueBytes, totalBytes))
	fmt.Printf("  + chunk refs     %s   (%d × %d B)\n", humanBytes(refOverhead), totalChunks, refBytes)
	fmt.Printf("  chunked store    %s   (%.1f%% of total)\n", humanBytes(chunkedStore), pct(chunkedStore, totalBytes))
	fmt.Printf("  saving           %s   (%.1f%%)\n", humanBytes(totalBytes-chunkedStore), pct(totalBytes-chunkedStore, totalBytes))

	return 0
}

// cdcChunks splits data into content-defined chunks averaging ~avg bytes, calling
// yield for each. A boundary is cut where a gear rolling hash (FastCDC-style) over
// the recent bytes hits 0 mod avg, bounded by [avg/4, avg*8]. Because the boundary
// depends only on local content, identical regions cut identically regardless of
// edits elsewhere — the property that lets chunks dedup across files.
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

// gearTable maps each byte to a fixed pseudo-random 64-bit value for the gear hash,
// seeded deterministically (splitmix64) so chunk boundaries are stable across runs.
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
