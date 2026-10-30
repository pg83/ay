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
// The caller trims leading and trailing whitespace.
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

	"ext_"? "from" nsp* npath nsp* "import" nsp+ ntarget (nsp* "," nsp* ntarget)* (nsp+ "with" nsp+ "context")? nsp* { return yycursor == yylimit }
	"ext_"? ("import" | "nlgimport") nsp* nmodule (nsp* "," nsp* nmodule)* nsp* { return yycursor == yylimit }
	* { return false }
	$ { return false }
	*/
}
