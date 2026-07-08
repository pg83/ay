package main

import (
	"embed"
	"regexp"
	"sync"
)

var (
	//go:embed *.go
	goSources              embed.FS
	stringLiteralRE        = regexp.MustCompile(`"([A-Z][A-Z0-9_]*|[A-Z0-9_]*[A-Z][A-Z0-9_]*)"`)
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

		for _, match := range stringLiteralRE.FindAllSubmatch(data, -1) {
			if looksLikeServiceWord(bytesString(match[1])) {
				tokens[internBytes(match[1]).string()] = struct{}{}
			}
		}
	}

	return tokens
}
