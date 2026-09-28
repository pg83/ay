import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
LAST_COMMON_DEFINE = "-D__LONG_LONG_SUPPORTED"
FIRST_NO_LIBC_FLAG = "-UNDEBUG"


def ay_make(files, target="a", *args):
    with tempfile.TemporaryDirectory(prefix="ay-yamake-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            if isinstance(content, bytes):
                path.write_bytes(content)
            else:
                path.write_text(content, newline="")
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


def library(body, **extra):
    files = {
        "a/ya.make": "LIBRARY()\n" + NO_PLATFORM + body + "\nSRCS(x.cpp)\nEND()\n",
        "a/x.cpp": "int x;\n",
    }
    files.update(extra)
    return files


def user_cflags(graph, output="$(B)/a/x.cpp.o"):
    args = lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]
    start = args.index(LAST_COMMON_DEFINE) + 1
    return args[start:args.index(FIRST_NO_LIBC_FLAG, start)]


class YaMakeSyntaxTest(unittest.TestCase):
    def assert_cflags(self, files, expected):
        code, graph, stderr = ay_make(files)
        self.assertEqual(code, 0, stderr)
        self.assertEqual(user_cflags(graph), expected)

    def assert_parse_error(self, text, expected):
        code, _, stderr = ay_make({"a/ya.make": text, "a/x.cpp": ""})
        self.assertEqual(code, 1)
        self.assertEqual(stderr, expected)

    def test_macro_argument_tokens(self):
        self.assert_cflags(library(
            "CFLAGS(-DA \"-DB=c d\" '-DE' -DF=\"g h\" -DI=\\\"j\\\" X-Y 12 12ab"
            " a@b -D#x #comment\n -DZ FOO\"bar\" FOO\\\"x\\\" FOO@y\"z\""
            " -w@v'u' -DQ'x y' -DR\\\"z \"a\\\\b\" \"-DS=\\\"q\\\"\" éx"
            " -DT=\"a\\\"b\")"
        ), [
            "-DA", "-DB=c d", "-DE", "-DF=g h", "-DI=\"j\"", "X-Y", "12",
            "12ab", "a@b", "-D#x", "-DZ", "FOObar", "FOO\"x\"", "FOO@yz",
            "-w@vu", "-DQx y", "-DR\"z", "a\\\\b", "-DS=\"q\"", "éx",
            "-DT=a\"b",
        ])

    def test_comments_line_endings_and_bom(self):
        text = (
            "# leading comment\r\n"
            "LIBRARY() # trailing comment\r"
            + NO_PLATFORM.replace("\n", "\r\n")
            + "CFLAGS(# comment right after the paren\r -DONE\r\n\t-DTWO)\r"
            "SRCS(x.cpp)\r\nEND()\r\n"
        )
        self.assert_cflags({
            "a/ya.make": b"\xef\xbb\xbf" + text.encode(),
            "a/x.cpp": "int x;\n",
        }, ["-DONE", "-DTWO"])

    def test_error_positions_follow_every_line_terminator(self):
        self.assert_parse_error(
            "LIBRARY()\rNO_LIBC()\r\n\tSRCS(x.cpp);\nEND()\n",
            "a/ya.make:3:13: unexpected character ';'",
        )

    def test_lexer_errors(self):
        cases = [
            ('LIBRARY()\nCFLAGS("abc', "a/ya.make:2:8: unterminated string"),
            ('LIBRARY()\nCFLAGS("abc\n")\n', "a/ya.make:2:8: unterminated string"),
            ('LIBRARY()\nCFLAGS("a\\"b\r")\n', "a/ya.make:2:8: unterminated string"),
            ('LIBRARY()\nCFLAGS("a\\"bc', "a/ya.make:2:8: unterminated string"),
            ('LIBRARY()\nCFLAGS(-DX="abc', "a/ya.make:2:8: unterminated string"),
            ('LIBRARY()\nCFLAGS(-DX="abc\n")\n', "a/ya.make:2:8: unterminated string"),
            ('LIBRARY()\nCFLAGS(FOO="a\r")\n', "a/ya.make:2:8: unterminated string"),
            ("LIBRARY()\nCFLAGS(a;b)\n", "a/ya.make:2:9: unexpected character ';'"),
            ("LIBRARY()\n;\n", "a/ya.make:2:1: unexpected character ';'"),
        ]
        for text, expected in cases:
            with self.subTest(text=text):
                self.assert_parse_error(text, expected)

    def test_statement_structure_errors(self):
        cases = [
            ('LIBRARY()\n"x"\n', 'a/ya.make:2:1: expected macro name, got string "x"'),
            ("LIBRARY()\nfoo-bar()\n", 'a/ya.make:2:1: expected macro name, got word "foo-bar"'),
            ("LIBRARY()\n)\n", "a/ya.make:2:1: expected macro name, got ')'"),
            ("LIBRARY()\n(\n", "a/ya.make:2:1: expected macro name, got '('"),
            ("LIBRARY()\n12()\n", "a/ya.make:2:1: expected macro name, got integer 12"),
            ("LIBRARY()\n==\n", "a/ya.make:2:1: expected macro name, got '=='"),
            ("LIBRARY()#x\n", 'a/ya.make:1:10: expected macro name, got word "#x"'),
            ("LIBRARY()\nSRCS x.cpp\n", 'a/ya.make:2:6: expected \'(\' after macro name "SRCS", got word "x.cpp"'),
            ("LIBRARY()\nSRCS", 'a/ya.make:2:5: expected \'(\' after macro name "SRCS", got end of file'),
            ("LIBRARY()\nSRCS(x.cpp\n", 'a/ya.make:2:1: unterminated macro call "SRCS" (missing \')\')'),
            ("LIBRARY()\nSRCS(x.cpp (y))\n", 'a/ya.make:2:12: unexpected \'(\' inside macro call "SRCS"'),
            ("LIBRARY()\nSRCS(x.cpp == y)\n", 'a/ya.make:2:12: unexpected \'==\' inside macro call "SRCS"'),
            ("LIBRARY()\nSRCS(a < b)\n", 'a/ya.make:2:8: unexpected \'<\' inside macro call "SRCS"'),
            ("LIBRARY()\nSRCS(>)\n", 'a/ya.make:2:6: unexpected \'>\' inside macro call "SRCS"'),
            ("LIBRARY()\nSRCS(!=)\n", 'a/ya.make:2:6: unexpected \'!=\' inside macro call "SRCS"'),
            ("LIBRARY()\nSRCS(>=)\n", 'a/ya.make:2:6: unexpected \'>=\' inside macro call "SRCS"'),
        ]
        for text, expected in cases:
            with self.subTest(text=text):
                self.assert_parse_error(text, expected)

    def test_if_syntax_errors(self):
        cases = [
            ("LIBRARY()\nIF (A)\nSRCS(x.cpp)\n", "a/ya.make:4:1: unexpected end of file inside IF block (missing ENDIF)"),
            ("LIBRARY()\nIF ()\nENDIF()\n", "a/ya.make:2:1: IF requires a condition expression"),
            ("LIBRARY()\nIF A\nENDIF()\n", "a/ya.make:2:4: expected '(' after IF, got identifier \"A\""),
            ("LIBRARY()\nIF (A\n", "a/ya.make:2:1: unterminated IF condition (missing ')')"),
            ("LIBRARY()\nIF (A)\nELSE()\nELSE()\nENDIF()\n", "a/ya.make:4:1: expected ENDIF after ELSE block, got ELSE"),
            ("LIBRARY()\nIF (A)\nELSE()\nELSEIF(B)\nENDIF()\n", "a/ya.make:4:1: expected ENDIF after ELSE block, got ELSEIF"),
            ("LIBRARY()\nIF (A B)\nENDIF()\n", "a/ya.make:2:7: unexpected identifier \"B\" in IF condition"),
            ("LIBRARY()\nIF (NOT)\nENDIF()\n", "a/ya.make:2:1: unexpected end of IF condition"),
            ("LIBRARY()\nIF (A ==)\nENDIF()\n", "a/ya.make:2:1: unexpected end of IF condition"),
            ("LIBRARY()\nIF ((A B))\nENDIF()\n", "a/ya.make:2:5: missing ')' in IF condition"),
            ("LIBRARY()\nIF ((A)\nENDIF()\n", "a/ya.make:2:1: unterminated IF condition (missing ')')"),
            ("LIBRARY()\nIF (A AND OR)\nENDIF()\n", "a/ya.make:2:11: operator \"OR\" used as identifier in IF condition"),
            ("LIBRARY()\nIF (NOT AND)\nENDIF()\n", "a/ya.make:2:9: operator \"AND\" used as identifier in IF condition"),
            ("LIBRARY()\nIF (== A)\nENDIF()\n", "a/ya.make:2:5: unexpected '==' in IF condition"),
            ("LIBRARY()\nIF (A == B == C)\nENDIF()\n", "a/ya.make:2:12: chained comparison '==' after '==' is not supported"),
            ("LIBRARY()\nIF (A < B >= C)\nENDIF()\n", "a/ya.make:2:11: chained comparison '>=' after '<' is not supported"),
            ("LIBRARY()\nIF (A > B < C)\nENDIF()\n", "a/ya.make:2:11: chained comparison '<' after '>' is not supported"),
            ("LIBRARY()\nIF (A != B != C)\nENDIF()\n", "a/ya.make:2:12: chained comparison '!=' after '!=' is not supported"),
            ("LIBRARY()\nIF (A >= B > C)\nENDIF()\n", "a/ya.make:2:12: chained comparison '>' after '>=' is not supported"),
            ("LIBRARY()\nIF (A STARTS_WITH B == C)\nENDIF()\n", "a/ya.make:2:21: chained comparison '==' after identifier \"STARTS_WITH\" is not supported"),
            ("LIBRARY()\nIF (A MATCHES B == C)\nENDIF()\n", "a/ya.make:2:17: chained comparison '==' after identifier \"MATCHES\" is not supported"),
            ("LIBRARY()\nIF (A VERSION_LT B == C)\nENDIF()\n", "a/ya.make:2:20: chained comparison '==' after identifier \"VERSION_LT\" is not supported"),
        ]
        for text, expected in cases:
            with self.subTest(text=text):
                self.assert_parse_error(text, expected)

    def test_macro_arity_errors(self):
        cases = [
            ("INCLUDE_ONCE(a b)", "a/ya.make:2:1: INCLUDE_ONCE expects 0 or 1 arguments, got 2"),
            ("INCLUDE()", "a/ya.make:2:1: INCLUDE expects at least 1 argument (the path)"),
            ("INCLUDE(/etc/passwd)", "a/ya.make:2:1: INCLUDE(/etc/passwd): absolute paths escape the source root"),
            ("SET()", "a/ya.make:2:1: SET expects at least 1 argument (name), got 0"),
            ("JOIN_SRCS()", "a/ya.make:2:1: JOIN_SRCS expects at least one argument (the output name)"),
            ("SRCDIR()", "a/ya.make:2:1: SRCDIR expects at least 1 argument, got 0"),
            ("GENERATE_ENUM_SERIALIZATION(a.h b.h)", "a/ya.make:2:1: GENERATE_ENUM_SERIALIZATION expects exactly 1 argument (header path), got 2"),
            ("GENERATE_ENUM_SERIALIZATION_WITH_HEADER()", "a/ya.make:2:1: GENERATE_ENUM_SERIALIZATION_WITH_HEADER expects exactly 1 argument (header path), got 0"),
            ("GENERATE_ENUM_SERIALIZATION_NOUTF()", "a/ya.make:2:1: GENERATE_ENUM_SERIALIZATION_NOUTF expects exactly 1 argument (header path), got 0"),
            ("CONFIGURE_FILE(a.in)", "a/ya.make:2:1: CONFIGURE_FILE expects exactly 2 arguments (src dst), got 1"),
            ("CREATE_BUILDINFO_FOR()", "a/ya.make:2:1: CREATE_BUILDINFO_FOR expects exactly 1 argument, got 0"),
            ("RUN_ANTLR4_CPP()", "a/ya.make:2:1: RUN_ANTLR4_CPP expects at least 1 argument (grammar)"),
            ("RUN_ANTLR4_CPP_SPLIT(L.g4)", "a/ya.make:2:1: RUN_ANTLR4_CPP_SPLIT expects at least 2 arguments (lexer parser)"),
            ("RUN_ANTLR()", "a/ya.make:2:1: RUN_ANTLR expects at least 1 argument"),
            ("RUN_ANTLR4()", "a/ya.make:2:1: RUN_ANTLR4 expects at least 1 argument"),
            ("RUN_PROGRAM()", "a/ya.make:2:1: RUN_PROGRAM expects at least 1 argument (tool path)"),
            ("RUN_PY3_PROGRAM()", "a/ya.make:2:1: RUN_PY3_PROGRAM expects at least 1 argument (tool path)"),
            ("RUN_PYTHON3()", "a/ya.make:2:1: RUN_PYTHON3 expects at least 1 argument (script path)"),
            ("RUN_LUA()", "a/ya.make:2:1: RUN_LUA expects at least 1 argument (script path)"),
            ("SPLIT_CODEGEN(tool)", "a/ya.make:2:1: SPLIT_CODEGEN expects at least 2 arguments (tool prefix), got 1"),
            ("BASE_CODEGEN(tool)", "a/ya.make:2:1: BASE_CODEGEN expects at least 2 arguments (tool prefix), got 1"),
            ("STRUCT_CODEGEN()", "a/ya.make:2:1: STRUCT_CODEGEN expects exactly 1 argument (prefix), got 0"),
            ("FROM_SANDBOX()", "a/ya.make:2:1: FROM_SANDBOX expects at least 1 argument (resource id)"),
            ("ALL_RESOURCE_FILES()", "a/ya.make:2:1: ALL_RESOURCE_FILES expects at least 1 argument (the extension)"),
            ("RESOURCE(DONT_PARSE DONT_COMPRESS a.txt)", "RESOURCE at line 2: argument count after DONT_PARSE/DONT_COMPRESS strip must be even (got 1)"),
        ]
        for text, expected in cases:
            with self.subTest(text=text):
                self.assert_parse_error("LIBRARY()\n" + text + "\nEND()\n", expected)

    def test_unknown_lowercase_macro_is_reported(self):
        code, _, stderr = ay_make(library("foo(bar)"))
        self.assertEqual(code, 1)
        self.assertEqual(stderr, 'unknown-macro: a: macro "foo" is not in the modelled set (line 5); skipped')
        code, graph, stderr = ay_make(library("foo(bar)"), "a", "-k")
        self.assertEqual(code, 0)
        self.assertEqual(stderr, 'unknown-macro: a: macro "foo" is not in the modelled set (line 5); skipped')
        lib.node_by_output(graph, "$(B)/a/x.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
