package main

import (
	"bytes"
	"embed"
	"sync"
)

var (
	//go:embed *.go
	goSources              embed.FS
	knownServiceTokensOnce sync.Once
	knownServiceTokensVal  map[string]struct{}
)

func knownServiceTokens() map[string]struct{} {
	knownServiceTokensOnce.Do(func() {
		knownServiceTokensVal = mineServiceTokensFromSources()
	})

	return knownServiceTokensVal
}

func mineServiceTokensFromSources() map[string]struct{} {
	tokens := map[string]struct{}{}
	entries, err := goSources.ReadDir(".")

	if err != nil {
		return tokens
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		data, err := goSources.ReadFile(e.Name())

		if err != nil {
			continue
		}

		for len(data) > 0 {
			open := bytes.IndexByte(data, '"')

			if open < 0 {
				break
			}

			data = data[open+1:]
			n := 0
			hasLetter := false

			for n < len(data) {
				c := data[n]

				if c >= 'A' && c <= 'Z' {
					hasLetter = true
					n++

					continue
				}

				if (c >= '0' && c <= '9') || c == '_' {
					n++

					continue
				}

				break
			}

			if n < len(data) && data[n] == '"' {
				if n > 0 && hasLetter && looksLikeServiceWord(bytesString(data[:n])) {
					tokens[internBytes(data[:n]).string()] = struct{}{}
				}

				data = data[n+1:]

				continue
			}

			if n == len(data) {
				break
			}

			data = data[n+1:]
		}
	}

	return tokens
}
