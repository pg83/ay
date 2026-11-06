package main

import (
	"bytes"
	"encoding/json"
)

func jsonStringView(quoted []byte) string {
	if bytes.IndexByte(quoted, '\\') < 0 {
		return bytesString(quoted[1 : len(quoted)-1])
	}

	var s string

	throw(json.Unmarshal(quoted, &s))

	return s
}

type canonicalResourceScan struct {
	data      []byte
	resources []string
}

func (c *canonicalResourceScan) value(i int) (int, bool) {
	switch i = jsonSkipSpace(c.data, i); c.data[i] {
	case '{':
		return c.object(i)
	case '[':
		return c.array(i)
	}

	return jsonValueEnd(c.data, i), false
}

func (c *canonicalResourceScan) array(i int) (int, bool) {
	bad := false

	if i = jsonSkipSpace(c.data, i+1); c.data[i] == ']' {
		return i + 1, false
	}

	for {
		var nested bool

		i, nested = c.value(i)
		bad = bad || nested

		if i = jsonSkipSpace(c.data, i); c.data[i] == ',' {
			i++

			continue
		}

		return i + 1, bad
	}
}

func (c *canonicalResourceScan) object(i int) (int, bool) {
	mark := len(c.resources)
	bad, uriSeen, checksumSeen, uriString := false, false, false, false
	uri := ""

	if i = jsonSkipSpace(c.data, i+1); c.data[i] == '}' {
		return i + 1, false
	}

	for {
		i = jsonSkipSpace(c.data, i)

		keyEnd := jsonStringEnd(c.data, i)
		key := jsonStringView(c.data[i:keyEnd])

		i = jsonSkipSpace(c.data, jsonSkipSpace(c.data, keyEnd)+1)

		var nested bool

		switch key {
		case "uri":
			uriSeen = true

			if uriString = c.data[i] == '"'; uriString {
				end := jsonStringEnd(c.data, i)

				uri, i = jsonStringView(c.data[i:end]), end
			} else {
				i, nested = c.value(i)
			}
		case "checksum":
			checksumSeen = true
			i, nested = c.value(i)
		default:
			i, nested = c.value(i)
		}

		bad = bad || nested

		if i = jsonSkipSpace(c.data, i); c.data[i] == ',' {
			i++

			continue
		}

		i++

		break
	}

	if !uriSeen || !checksumSeen {
		return i, bad
	}

	c.resources = c.resources[:mark]

	if !uriString {
		return i, true
	}

	if resource := canonicalTestResource(uri); resource != "" {
		c.resources = append(c.resources, resource)
	}

	return i, false
}

type usrvDepsIndex struct {
	data  []byte
	spans map[string][2]int32
}

func newUsrvDepsIndex(data []byte) *usrvDepsIndex {
	data = append([]byte(nil), data...)

	index := &usrvDepsIndex{data: data, spans: map[string][2]int32{}}
	i := jsonSkipSpace(data, jsonSkipSpace(data, 0)+1)

	if data[i] == '}' {
		return index
	}

	for {
		keyEnd := jsonStringEnd(data, i)
		key := jsonStringView(data[i:keyEnd])
		start := jsonSkipSpace(data, jsonSkipSpace(data, keyEnd)+1)
		end := jsonValueEnd(data, start)

		index.spans[key] = [2]int32{int32(start), int32(end)}

		if i = jsonSkipSpace(data, end); data[i] != ',' {
			return index
		}

		i = jsonSkipSpace(data, i+1)
	}
}

func (x *usrvDepsIndex) deps(file string) ([]IncludeDirective, bool) {
	span, ok := x.spans[file]

	if !ok || x.data[span[0]] != '[' {
		return nil, false
	}

	var out []IncludeDirective

	if i := jsonSkipSpace(x.data, int(span[0])+1); x.data[i] == ']' {
		return out, true
	}

	for i := int(span[0]) + 1; ; {
		i = jsonSkipSpace(x.data, i)

		if x.data[i] != '"' {
			throwFmt("USRV_BUILD: dependency of %s is not a string", file)
		}

		end := jsonStringEnd(x.data, i)

		var dep STR

		if bytes.IndexByte(x.data[i:end], '\\') >= 0 {
			dep = internStr(jsonStringView(x.data[i:end]))
		} else {
			dep = internBytes(x.data[i+1 : end-1])
		}

		out = append(out, IncludeDirective{kind: includeQuoted, target: includeTarget(dep.any())})

		if i = jsonSkipSpace(x.data, end); x.data[i] != ',' {
			return out, true
		}

		i++
	}
}
