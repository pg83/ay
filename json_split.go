package main

import (
	"bytes"
	"encoding/json"
)

func jsonObjectMembers(data []byte, visit func(key string, raw []byte)) bool {
	i := jsonSkipSpace(data, 0)

	if i >= len(data) || data[i] != '{' {
		return false
	}

	i = jsonSkipSpace(data, i+1)

	if i < len(data) && data[i] == '}' {
		return true
	}

	for {
		if i >= len(data) || data[i] != '"' {
			return false
		}

		keyEnd := jsonStringEnd(data, i)

		if keyEnd < 0 {
			return false
		}

		key := jsonDecodeString(data[i:keyEnd])

		i = jsonSkipSpace(data, keyEnd)

		if i >= len(data) || data[i] != ':' {
			return false
		}

		i = jsonSkipSpace(data, i+1)

		end := jsonValueEnd(data, i)

		if end < 0 {
			return false
		}

		visit(key, data[i:end])
		i = jsonSkipSpace(data, end)

		if i >= len(data) {
			return false
		}

		switch data[i] {
		case ',':
			i = jsonSkipSpace(data, i+1)
		case '}':
			return true
		default:
			return false
		}
	}
}

func jsonSkipSpace(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}

	return i
}

func jsonStringEnd(data []byte, i int) int {
	for i++; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}

	return -1
}

func jsonDecodeString(quoted []byte) string {
	if bytes.IndexByte(quoted, '\\') < 0 {
		return string(quoted[1 : len(quoted)-1])
	}

	var s string

	if json.Unmarshal(quoted, &s) != nil {
		return ""
	}

	return s
}

func jsonValueEnd(data []byte, i int) int {
	if i >= len(data) {
		return -1
	}

	switch data[i] {
	case '"':
		return jsonStringEnd(data, i)
	case '{', '[':
		depth := 0

		for ; i < len(data); i++ {
			switch data[i] {
			case '"':
				end := jsonStringEnd(data, i)

				if end < 0 {
					return -1
				}

				i = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--

				if depth == 0 {
					return i + 1
				}
			}
		}

		return -1
	}

	for ; i < len(data); i++ {
		switch data[i] {
		case ',', '}', ']', ' ', '\t', '\n', '\r':
			return i
		}
	}

	return i
}
