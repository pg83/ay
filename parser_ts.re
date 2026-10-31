package main

/*!re2c
	ws = [\t\n\v\f\r ];
	comment = "/*" [^*]* "*/";
	sp = ws | comment;
	str = "\"" [^"]* "\"" | "'" [^']* "'";
	ident = [a-zA-Z$_] [a-zA-Z0-9$_]*;
	alias = sp+ "as" sp+ ident;
	spec = ident alias?;
	sep = sp* "," sp*;
	named = "{" sp* (spec (sep spec)* sep?)? sp* "}";
	namespace = "*" alias?;
	clause = ident (sep (namespace | named))? | namespace | named;
*/

// tsImportCallMatch matches "( <str>" with comments/whitespace at the
// start of input[pos:]; input must end with a 0 sentinel. Upstream adds the
// import on the closing quote, so the call closer is not required.
func tsImportCallMatch(yyinput []byte, pos int) (end, qs, qe int, kind TSImportMatch) {
	yycursor, yymarker := pos, pos
	yylimit := len(yyinput) - 1
	q1, q2 := -1, -1
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:eof = 0;
	re2c:tags = 1;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyinput[yycursor]";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";
	re2c:define:YYLESSTHAN = "yylimit <= yycursor";
	re2c:define:YYSTAGP = "@@{tag} = yycursor";
	re2c:define:YYSTAGN = "@@{tag} = -1";
	re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";


	sp* "(" sp* @q1 str @q2 { return yycursor, q1, q2, tsImportMatched }
	sp* "(" sp* { return yycursor, 0, 0, tsSpecifierExpected }
	* { return 0, 0, 0, tsNoImport }
	$ { return 0, 0, 0, tsNoImport }
	*/
}

// tsImportDeclMatch matches an import/export clause followed by a module
// string at the start of input[pos:]; input must end with a 0 sentinel.
func tsImportDeclMatch(yyinput []byte, pos int) (end, qs, qe int, kind TSImportMatch) {
	yycursor, yymarker := pos, pos
	yylimit := len(yyinput) - 1
	q1, q2 := -1, -1
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:eof = 0;
	re2c:tags = 1;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyinput[yycursor]";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";
	re2c:define:YYLESSTHAN = "yylimit <= yycursor";
	re2c:define:YYSTAGP = "@@{tag} = yycursor";
	re2c:define:YYSTAGN = "@@{tag} = -1";
	re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";


	sp+ (("type" sp+)? clause sp+ "from" sp+)? @q1 str @q2 { return yycursor, q1, q2, tsImportMatched }
	sp+ ("type" sp+)? clause sp+ "from" sp+ { return yycursor, 0, 0, tsSpecifierExpected }
	sp+ { return yycursor, 0, 0, tsClauseExpected }
	* { return 0, 0, 0, tsNoImport }
	$ { return 0, 0, 0, tsNoImport }
	*/
}
