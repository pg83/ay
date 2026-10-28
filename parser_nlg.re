package main

/*!re2c
	nsp = [\t\n\v\f\r ];
	npath = "\"" [a-zA-Z0-9_/]+ ".nlg" "\"" | "'" [a-zA-Z0-9_/]+ ".nlg" "'";
	ntarget = [a-zA-Z0-9_]+;
	nmodule = npath (nsp* "as" nsp+ ntarget)? (nsp+ "with" nsp+ "context")?;
*/

// nlgImportStatement reports whether the whole command is a
// `[ext_]from "x.nlg" import a, b [with context]` or an
// `[ext_](import|nlgimport) "x.nlg" [as a] [with context], …` statement.
func nlgImportStatement(yyinput string) bool {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:eof = 0;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";
	re2c:define:YYLESSTHAN = "yylimit <= yycursor";

	nsp* "ext_"? "from" nsp* npath nsp* "import" nsp+ ntarget (nsp* "," nsp* ntarget)* (nsp+ "with" nsp+ "context")? nsp* { return yycursor == yylimit }
	nsp* "ext_"? ("import" | "nlgimport") nsp* nmodule (nsp* "," nsp* nmodule)* nsp* { return yycursor == yylimit }
	* { return false }
	$ { return false }
	*/
}

// nlgImportPathNext finds the next quoted "<path>.nlg" at or after pos and
// returns the path bounds and the offset past the closing quote.
func nlgImportPathNext(yyinput string, pos int) (ps, pe, end int, ok bool) {
	yycursor, yymarker := pos, pos
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
		/*!re2c
		re2c:yyfill:enable = 0;
		re2c:eof = 0;
		re2c:tags = 1;
		re2c:define:YYCTYPE = byte;
		re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
		re2c:define:YYSKIP = "yycursor += 1";
		re2c:define:YYBACKUP = "yymarker = yycursor";
		re2c:define:YYRESTORE = "yycursor = yymarker";
		re2c:define:YYLESSTHAN = "yylimit <= yycursor";
		re2c:define:YYSTAGP = "@@{tag} = yycursor";
		re2c:define:YYSTAGN = "@@{tag} = -1";
		re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

		["'] @ps [a-zA-Z0-9_/]+ ".nlg" @pe ["'] { return ps, pe, yycursor, true }
		* { continue }
		$ { return 0, 0, 0, false }
		*/
	}
}
