package main

var ansiCols = map[string]string{
	"red":           ansiESC + "[31m",
	"green":         ansiESC + "[32m",
	"yellow":        ansiESC + "[33m",
	"blue":          ansiESC + "[34m",
	"magenta":       ansiESC + "[35m",
	"cyan":          ansiESC + "[36m",
	"white":         ansiESC + "[37m",
	"light-red":     ansiESC + "[91m",
	"light-green":   ansiESC + "[92m",
	"light-yellow":  ansiESC + "[93m",
	"light-blue":    ansiESC + "[94m",
	"light-magenta": ansiESC + "[95m",
	"light-cyan":    ansiESC + "[96m",
	"light-white":   ansiESC + "[97m",
}

const (
	ansiESC = "\x1b"
	ansiRST = ansiESC + "[0m"
)

// color takes a name from ansiCols: the callers pass constants and node
// colors, which every node sets.
func color(name, s string) string {
	c, ok := ansiCols[name]

	assert(ok, "color: unknown name")

	return c + s + ansiRST
}
