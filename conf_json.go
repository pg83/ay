package main

import (
	"slices"
	"strconv"
	"unicode/utf8"
)

var rapidJSONErrors = [...]string{
	"No error.",
	"The document is empty.",
	"The document root must not be followed by other values.",
	"Invalid value.",
	"Missing a name for object member.",
	"Missing a colon after a name of object member.",
	"Missing a comma or '}' after an object member.",
	"Missing a comma or ']' after an array element.",
	"Incorrect hex digit after \\u escape in string.",
	"The surrogate pair in string is invalid.",
	"Invalid escape character in string.",
	"Missing a closing quotation mark in string.",
	"Invalid encoding in string.",
	"Number too big to be stored in double.",
	"Miss fraction part in number.",
	"Miss exponent in number.",
	"Terminate parsing due to Handler error.",
	"Unspecific syntax error.",
}

const (
	confJSONNull   = 'n'
	confJSONBool   = 'b'
	confJSONNumber = 'd'
	confJSONString = 's'
	confJSONArray  = 'a'
	confJSONObject = 'o'
)

const (
	rjDocumentEmpty = iota + 1
	rjRootNotSingular
	rjValueInvalid
	rjObjectMissName
	rjObjectMissColon
	rjObjectMissCommaOrCurlyBracket
	rjArrayMissCommaOrSquareBracket
	rjUnicodeEscapeInvalidHex
	rjUnicodeSurrogateInvalid
	rjEscapeInvalid
	rjMissQuotationMark
	rjInvalidEncoding
	_
	rjMissFraction
	rjMissExponent
	_
	rjUnspecificSyntaxError
)

type confJSON struct {
	kind  byte
	str   string
	keys  []string
	items []*confJSON
}

type rapidJSONReader struct {
	data   []byte
	pos    int
	code   int
	offset int
}

func (r *rapidJSONReader) peek() byte {
	if r.pos < len(r.data) {
		return r.data[r.pos]
	}

	return 0
}

func (r *rapidJSONReader) consume(c byte) bool {
	if r.peek() == c && c != 0 {
		r.pos++

		return true
	}

	return false
}

func (r *rapidJSONReader) fail(code, offset int) {
	if r.code == 0 {
		r.code, r.offset = code, offset
	}
}

func (r *rapidJSONReader) skipWhitespace() {
	for {
		switch r.peek() {
		case ' ', '\n', '\r', '\t':
			r.pos++
		default:
			return
		}
	}
}

func (r *rapidJSONReader) skipWhitespaceAndComments() {
	r.skipWhitespace()

	for r.code == 0 && r.consume('/') {
		switch {
		case r.consume('*'):
			for {
				if r.peek() == 0 {
					r.fail(rjUnspecificSyntaxError, r.pos)

					return
				}

				if r.consume('*') {
					if r.consume('/') {
						break
					}
				} else {
					r.pos++
				}
			}
		case r.consume('/'):
			for r.peek() != 0 {
				r.pos++

				if r.data[r.pos-1] == '\n' {
					break
				}
			}
		default:
			r.fail(rjUnspecificSyntaxError, r.pos)

			return
		}

		r.skipWhitespace()
	}
}

func (r *rapidJSONReader) literal(word string, v *confJSON) *confJSON {
	r.pos++

	for i := 1; i < len(word); i++ {
		if !r.consume(word[i]) {
			r.fail(rjValueInvalid, r.pos)

			return nil
		}
	}

	return v
}

func (r *rapidJSONReader) value() *confJSON {
	switch r.peek() {
	case 'n':
		return r.literal("null", &confJSON{kind: confJSONNull})
	case 't':
		return r.literal("true", &confJSON{kind: confJSONBool, str: "true"})
	case 'f':
		return r.literal("false", &confJSON{kind: confJSONBool, str: "false"})
	case '"':
		if s, ok := r.string(); ok {
			return &confJSON{kind: confJSONString, str: s}
		}

		return nil
	case '{':
		return r.object()
	case '[':
		return r.array()
	}

	return r.number()
}

func hexDigit(c byte) (rune, bool) {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0'), true
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return rune(c-'A') + 10, true
	}

	return 0, false
}

func jsonEscape(c byte) (byte, bool) {
	switch c {
	case '"', '\\', '/':
		return c, true
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	}

	return 0, false
}

func (r *rapidJSONReader) hex4(escape int) (rune, bool) {
	var v rune

	for range 4 {
		d, ok := hexDigit(r.peek())

		if !ok {
			r.fail(rjUnicodeEscapeInvalidHex, escape)

			return 0, false
		}

		v = v<<4 | d
		r.pos++
	}

	return v, true
}

func (r *rapidJSONReader) string() (string, bool) {
	r.pos++

	var out []byte

	for {
		c := r.peek()

		switch {
		case c == '\\':
			escape := r.pos

			r.pos++

			if e, ok := jsonEscape(r.peek()); ok {
				r.pos++
				out = append(out, e)

				continue
			}

			if !r.consume('u') {
				r.fail(rjEscapeInvalid, escape)

				return "", false
			}

			code, ok := r.hex4(escape)

			if !ok {
				return "", false
			}

			if code >= 0xD800 && code <= 0xDBFF {
				if !r.consume('\\') || !r.consume('u') {
					r.fail(rjUnicodeSurrogateInvalid, escape)

					return "", false
				}

				low, ok := r.hex4(escape)

				if !ok {
					return "", false
				}

				if low < 0xDC00 || low > 0xDFFF {
					r.fail(rjUnicodeSurrogateInvalid, escape)

					return "", false
				}

				code = (code-0xD800)<<10 | (low - 0xDC00) + 0x10000
			}

			out = utf8.AppendRune(out, code)
		case c == '"':
			r.pos++

			return string(out), true
		case c < 0x20:
			if c == 0 {
				r.fail(rjMissQuotationMark, r.pos)
			} else {
				r.fail(rjInvalidEncoding, r.pos)
			}

			return "", false
		case c < 0x80:
			out = append(out, c)
			r.pos++
		default:
			if ch, size := utf8.DecodeRune(r.data[r.pos:]); ch == utf8.RuneError && size <= 1 {
				r.fail(rjInvalidEncoding, r.pos)

				return "", false
			} else {
				out = append(out, r.data[r.pos:r.pos+size]...)
				r.pos += size
			}
		}
	}
}

func (r *rapidJSONReader) digit() bool {
	c := r.peek()

	return c >= '0' && c <= '9'
}

func (r *rapidJSONReader) number() *confJSON {
	start := r.pos

	r.consume('-')

	switch {
	case r.consume('0'):
	case r.digit():
		for r.digit() {
			r.pos++
		}
	default:
		r.fail(rjValueInvalid, r.pos)

		return nil
	}

	if r.consume('.') {
		if !r.digit() {
			r.fail(rjMissFraction, r.pos)

			return nil
		}

		for r.digit() {
			r.pos++
		}
	}

	if r.consume('e') || r.consume('E') {
		if !r.consume('+') {
			r.consume('-')
		}

		if !r.digit() {
			r.fail(rjMissExponent, r.pos)

			return nil
		}

		for r.digit() {
			r.pos++
		}
	}

	return &confJSON{kind: confJSONNumber, str: string(r.data[start:r.pos])}
}

func (r *rapidJSONReader) object() *confJSON {
	r.pos++
	r.skipWhitespaceAndComments()

	v := &confJSON{kind: confJSONObject}

	if r.code != 0 || r.consume('}') {
		return v
	}

	for {
		if r.peek() != '"' {
			r.fail(rjObjectMissName, r.pos)

			return nil
		}

		key, ok := r.string()

		if !ok {
			return nil
		}

		if r.skipWhitespaceAndComments(); r.code != 0 {
			return nil
		}

		if !r.consume(':') {
			r.fail(rjObjectMissColon, r.pos)

			return nil
		}

		if r.skipWhitespaceAndComments(); r.code != 0 {
			return nil
		}

		item := r.value()

		if r.code != 0 {
			return nil
		}

		if i := slices.Index(v.keys, key); i >= 0 {
			v.items[i] = item
		} else {
			v.keys, v.items = append(v.keys, key), append(v.items, item)
		}

		if r.skipWhitespaceAndComments(); r.code != 0 {
			return nil
		}

		switch {
		case r.consume(','):
			if r.skipWhitespaceAndComments(); r.code != 0 {
				return nil
			}
		case r.consume('}'):
			return v
		default:
			r.fail(rjObjectMissCommaOrCurlyBracket, r.pos)

			return nil
		}
	}
}

func (r *rapidJSONReader) array() *confJSON {
	r.pos++
	r.skipWhitespaceAndComments()

	v := &confJSON{kind: confJSONArray}

	if r.code != 0 || r.consume(']') {
		return v
	}

	for {
		item := r.value()

		if r.code != 0 {
			return nil
		}

		v.items = append(v.items, item)

		if r.skipWhitespaceAndComments(); r.code != 0 {
			return nil
		}

		switch {
		case r.consume(','):
			if r.skipWhitespaceAndComments(); r.code != 0 {
				return nil
			}
		case r.consume(']'):
			return v
		default:
			r.fail(rjArrayMissCommaOrSquareBracket, r.pos)

			return nil
		}
	}
}

func readConfJSON(data []byte) (*confJSON, string) {
	r := &rapidJSONReader{data: data}

	r.skipWhitespaceAndComments()

	var root *confJSON

	switch {
	case r.code != 0:
	case r.peek() == 0:
		r.fail(rjDocumentEmpty, r.pos)
	default:
		if root = r.value(); r.code == 0 {
			if r.skipWhitespaceAndComments(); r.code == 0 && r.peek() != 0 {
				r.fail(rjRootNotSingular, r.pos)
			}
		}
	}

	if r.code != 0 {
		return nil, "library/cpp/json/json_reader.cpp:427: Offset: " + strconv.Itoa(r.offset) + ", Code: " + strconv.Itoa(r.code) + ", Error: " + rapidJSONErrors[r.code]
	}

	return root, ""
}
