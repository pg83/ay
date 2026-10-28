package main

// yaConfFormulaNext finds the next `"formula": "<path>.json"` at or after
// pos in a ya.conf.json-like text and returns the path bounds and the
// offset past the match.
func yaConfFormulaNext(yyinput []byte, pos int) (ps, pe, end int, ok bool) {
	yycursor, yymarker := pos, pos
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
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

		"\"formula\"" ysp* ":" ysp* "\"" @ps [^"]+ ".json" @pe "\"" { return ps, pe, yycursor, true }
		* { continue }
		$ { return 0, 0, 0, false }
		*/
	}
}

// yaConfResourceNext finds the next `"resource": "<id>"` at or after pos
// and returns the id bounds and the offset past the match.
func yaConfResourceNext(yyinput []byte, pos int) (ps, pe, end int, ok bool) {
	yycursor, yymarker := pos, pos
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
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

		"\"resource\"" ysp* ":" ysp* "\"" @ps [^"]+ @pe "\"" { return ps, pe, yycursor, true }
		* { continue }
		$ { return 0, 0, 0, false }
		*/
	}
}
