package main

import "strconv"

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

func trueColor(r, g, b uint8, s string) string {
	return ansiESC + "[38;2;" +
		strconv.Itoa(int(r)) + ";" +
		strconv.Itoa(int(g)) + ";" +
		strconv.Itoa(int(b)) + "m" +
		s + ansiRST
}

func color(name, s string) string {
	c, ok := ansiCols[name]

	if !ok {
		return s
	}

	return c + s + ansiRST
}
