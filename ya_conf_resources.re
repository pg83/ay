package main

import "bytes"

var (
	yaConfFormulaKey  = []byte(`"formula"`)
	yaConfResourceKey = []byte(`"resource"`)
)

// yaConfFormulaNext finds the next `"formula": "<path>.json"` at or after
// pos in a ya.conf.json-like text and returns the path bounds and the
// offset past the match. Candidates start at the literal key, found with
// bytes.Index; the scanner only checks the rest.
func yaConfFormulaNext(yyinput []byte, pos int) (ps, pe, end int, ok bool) {
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
		i := bytes.Index(yyinput[pos:], yaConfFormulaKey)

		if i < 0 {
			return 0, 0, 0, false
		}

		yycursor := pos + i + len(yaConfFormulaKey)
		yymarker := yycursor
		/*!re2c
		re2c:yyfill:enable = 0;
		re2c:eof = 0;
		re2c:tags = 1;
		re2c:define:YYCTYPE = byte;
		re2c:define:YYPEEK = "yyPeek(yyinput, yycursor)";
		re2c:define:YYSKIP = "yycursor += 1";
		re2c:define:YYBACKUP = "yymarker = yycursor";
		re2c:define:YYRESTORE = "yycursor = yymarker";
		re2c:define:YYLESSTHAN = "yylimit <= yycursor";
		re2c:define:YYSTAGP = "@@{tag} = yycursor";
		re2c:define:YYSTAGN = "@@{tag} = -1";
		re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

		ysp = [\t\n\f\r ];

		ysp* ":" ysp* "\"" @ps [^"]+ ".json" @pe "\"" { return ps, pe, yycursor, true }
		* { pos += i + 1; continue }
		$ { pos += i + 1; continue }
		*/
	}
}

// yaConfResourceNext finds the next `"resource": "<id>"` at or after pos
// and returns the id bounds and the offset past the match.
func yaConfResourceNext(yyinput []byte, pos int) (ps, pe, end int, ok bool) {
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
		i := bytes.Index(yyinput[pos:], yaConfResourceKey)

		if i < 0 {
			return 0, 0, 0, false
		}

		yycursor := pos + i + len(yaConfResourceKey)
		yymarker := yycursor
		/*!re2c
		re2c:yyfill:enable = 0;
		re2c:eof = 0;
		re2c:tags = 1;
		re2c:define:YYCTYPE = byte;
		re2c:define:YYPEEK = "yyPeek(yyinput, yycursor)";
		re2c:define:YYSKIP = "yycursor += 1";
		re2c:define:YYBACKUP = "yymarker = yycursor";
		re2c:define:YYRESTORE = "yycursor = yymarker";
		re2c:define:YYLESSTHAN = "yylimit <= yycursor";
		re2c:define:YYSTAGP = "@@{tag} = yycursor";
		re2c:define:YYSTAGN = "@@{tag} = -1";
		re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

		ysp* ":" ysp* "\"" @ps [^"]+ @pe "\"" { return ps, pe, yycursor, true }
		* { pos += i + 1; continue }
		$ { pos += i + 1; continue }
		*/
	}
}
