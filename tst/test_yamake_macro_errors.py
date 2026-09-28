import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
ERRORS = [
    ("SET_RESOURCE_URI_FROM_JSON(x)", "gen: a: SET_RESOURCE_URI_FROM_JSON expects 2 args (var json), got 1"),
    ("LLVM_BC(a.cpp NAME n)", "LLVM_BC requires USE_LLVM_BC16/18/20 before invocation"),
    ("USE_LLVM_BC16()\nLLVM_BC(a.cpp NAME)", "LLVM_BC NAME expects a value"),
    ("USE_LLVM_BC18()\nLLVM_BC(a.cpp SUFFIX)", "LLVM_BC SUFFIX expects a value"),
    ("USE_LLVM_BC20()\nLLVM_BC(a.cpp b.cpp)", "LLVM_BC: NAME keyword is required (got args [a.cpp b.cpp])"),
    ("CHECK_CONFIG_H()", "CHECK_CONFIG_H expects exactly 1 argument, got 0"),
    ("DECIMAL_MD5_LOWER_32_BITS()", "gen: a: DECIMAL_MD5_LOWER_32_BITS expects at least 1 argument (File)"),
    ("DECIMAL_MD5_LOWER_32_BITS(f.cpp FUNCNAME)", "gen: a: DECIMAL_MD5_LOWER_32_BITS FUNCNAME requires a value"),
    ("BUILDWITH_CYTHON_CPP()", "BUILDWITH_CYTHON_CPP expects at least 1 argument"),
    ("BUILDWITH_CYTHON_C()", "BUILDWITH_CYTHON_C expects at least 1 argument"),
    ("PY_NAMESPACE()", "gen: PY_NAMESPACE expects exactly 1 argument, got 0"),
    ("YQL_LAST_ABI_VERSION(x)", "YQL_LAST_ABI_VERSION expects exactly 0 arguments, got 1"),
    ("YQL_ABI_VERSION(1 2)", "YQL_ABI_VERSION expects exactly 3 arguments, got 2"),
    ("PROTOC_FATAL_WARNINGS(x)", "PROTOC_FATAL_WARNINGS expects exactly 0 arguments, got 1"),
    ("COPY_FILE(a)", "gen: COPY_FILE at line 5 expects at least source and destination, got 1 args"),
    ("COPY_FILE(AUTO TEXT a)", "gen: COPY_FILE at line 5 expects at least source and destination, got 3 args"),
    ("COPY(AUTO WITH_CONTEXT a)", "gen: COPY at line 5 expects FROM <dir>"),
    ("COPY(FROM)", "gen: COPY at line 5 expects source directory after FROM"),
    ("PROTO_NAMESPACE()", "gen: PROTO_NAMESPACE expects at least 1 argument"),
    ("YMAPS_SPROTO(a.txt)", 'gen: a: YMAPS_SPROTO expects .proto arguments, got "a.txt"'),
    ("YA_CONF_JSON()", "YA_CONF_JSON expects exactly 1 argument, got 0"),
    ("ALLOCATOR()", "gen: ALLOCATOR expects exactly 1 argument, got 0 (line 5)"),
    ("ALLOCATOR(foo)", 'gen: unknown allocator "foo" (line 5); extend allocatorPeers in gen.go'),
    ("ARCHIVE(NAME)", "gen: ARCHIVE(NAME ...) missing value after NAME (line 5)"),
    ("ARCHIVE(a.txt)", "gen: ARCHIVE expects `NAME <output>` (line 5)"),
    ("ARCHIVE(NAME out.inc)", "gen: ARCHIVE(NAME out.inc) has no input files (line 5)"),
    ("ARCHIVE_BY_KEYS(NAME)", "gen: ARCHIVE_BY_KEYS(NAME ...) missing value after NAME (line 5)"),
    ("ARCHIVE_BY_KEYS(NAME out.inc KEYS)", "gen: ARCHIVE_BY_KEYS(KEYS ...) missing value after KEYS (line 5)"),
    ("ARCHIVE_BY_KEYS(a.txt)", "gen: ARCHIVE_BY_KEYS expects `NAME <output>` (line 5)"),
    ("ARCHIVE_BY_KEYS(NAME out.inc)", "gen: ARCHIVE_BY_KEYS(NAME out.inc) has no input files (line 5)"),
    ("ARCHIVE_ASM(NAME)", "gen: ARCHIVE_ASM(NAME ...) missing value after NAME (line 5)"),
    ("ARCHIVE_ASM(a.txt)", "gen: ARCHIVE_ASM expects `NAME <output>` (line 5)"),
    ("ARCHIVE_ASM(NAME out.inc DONTCOMPRESS)", "gen: ARCHIVE_ASM(NAME out.inc) has no input files (line 5)"),
    ("LJ_21_ARCHIVE(a.txt)", "gen: LJ_21_ARCHIVE has no .lua files (line 5)"),
    ("SRC()", "gen: SRC() requires at least 1 argument (filename); got 0 at line 5"),
    ("SRC_C_NO_LTO()", "gen: SRC_C_NO_LTO expects exactly 1 argument (filename); got 0 at line 5"),
    ("SRC_C_AVX()", "gen: SRC_C_AVX() requires at least 1 argument (filename); got 0 at line 5"),
    ("BUILD_MN(a)", "bad-macro-args: a: BUILD_MN with options is not modelled, expected (MnInfo MnName), got 1 args; skipped"),
    ("AR_PLUGIN()", "gen: AR_PLUGIN expects exactly 1 argument, got 0"),
    ("DYNAMIC_LIBRARY_FROM()", "gen: DYNAMIC_LIBRARY_FROM expects at least 1 argument"),
    ("EXPORTS_SCRIPT()", "gen: EXPORTS_SCRIPT expects exactly 1 argument, got 0"),
    ("PY_SRCS(NAMESPACE)", "PY_SRCS NAMESPACE expects a value"),
    ("PY_SRCS(CYTHON_DIRECTIVE)", "PY_SRCS CYTHON_DIRECTIVE expects a value"),
    ("PY_MAIN()", "gen: PY_MAIN expects exactly 1 argument, got 0"),
    ("PY_CONSTRUCTOR()", "gen: PY_CONSTRUCTOR expects exactly 1 argument, got 0"),
    ("CPP_PROTO_PLUGIN0(n)", "gen: CPP_PROTO_PLUGIN0 expects at least 2 arguments, got 1"),
    ("CPP_PROTO_PLUGIN(n t)", "gen: CPP_PROTO_PLUGIN expects at least 3 arguments, got 2"),
    ("CPP_PROTO_PLUGIN2(n t .a)", "gen: CPP_PROTO_PLUGIN2 expects at least 4 arguments, got 3"),
    ("CPP_PROTO_PLUGIN0(n t EXTRA_OUT_FLAG)", "gen: CPP_PROTO_PLUGIN0 EXTRA_OUT_FLAG expects exactly 1 argument"),
    ("CPP_PROTO_PLUGIN0(n t EXTRA_OUT_FLAG a EXTRA_OUT_FLAG b)", "gen: CPP_PROTO_PLUGIN0 repeated EXTRA_OUT_FLAG"),
    ("CPP_PROTO_PLUGIN0(n t bogus)", 'gen: CPP_PROTO_PLUGIN0 got unexpected tail token "bogus"; supported suffixes are DEPS and EXTRA_OUT_FLAG'),
    ("APPHOST(bogus)", 'gen: a: APPHOST: unexpected argument "bogus"'),
    ("YAFF(pos)", 'gen: YAFF got unexpected positional argument "pos"'),
    ("YAFF(NAMESPACE)", "gen: YAFF NAMESPACE expects a value"),
    ("YAFF_SCHEMA()", "gen: YAFF_SCHEMA expects SCHEMA_NAME, got 0 positional args"),
    ("YAFF_SCHEMA(a b c)", 'gen: YAFF_SCHEMA got unexpected positional argument "c"'),
    ("DECLARE_IN_DIRS(x)", "gen: a: DECLARE_IN_DIRS expects a var prefix and a pattern"),
    ("DECLARE_IN_DIRS(x *.txt bogus)", 'gen: a: DECLARE_IN_DIRS: unexpected argument "bogus"'),
    ("ALL_PY_SRCS(NAMESPACE)", "ALL_PY_SRCS NAMESPACE expects a value"),
    ("FOO_BAR_BAZ()", 'unknown-macro: a: macro "FOO_BAR_BAZ" is not in the modelled set (line 5); skipped'),
    ("STRIP()", 'unknown-macro: a: macro "STRIP" not modelled'),
    ("ALLOCATOR(NOT_AN_ALLOCATOR)", 'gen: macro ALLOCATOR received service-keyword "NOT_AN_ALLOCATOR" that no handler models'),
]


def ay_make(files, *args):
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
            path.write_text(content)
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
                *args, "p",
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )
        return result.returncode, ANSI.sub("", result.stderr).strip()


def with_library(body):
    return {
        "p/ya.make": "PROGRAM()\n" + NO_PLATFORM + "PEERDIR(a)\nSRCS(main.cpp)\nEND()\n",
        "p/main.cpp": "int main(){return 0;}\n",
        "a/ya.make": "LIBRARY()\n" + NO_PLATFORM + body + "\nEND()\n",
    }


class YaMakeMacroErrorsTest(unittest.TestCase):
    def test_invalid_macro_arguments_fail_the_build(self):
        for body, expected in ERRORS:
            with self.subTest(body=body):
                code, stderr = ay_make(with_library(body))
                self.assertEqual(code, 1)
                self.assertTrue(stderr.startswith(expected), stderr)

    def test_keep_going_reports_the_failed_peer_and_continues(self):
        for body, expected in ERRORS:
            with self.subTest(body=body):
                code, stderr = ay_make(with_library(body), "-k")
                self.assertEqual(code, 0)
                if not expected.startswith(("bad-macro-args", "unknown-macro")):
                    expected = "module-failed: a: " + expected
                self.assertTrue(stderr.startswith(expected), stderr)

    def test_acknowledged_macros_are_silently_ignored(self):
        code, stderr = ay_make(with_library("ALICE_TYPED_CALLBACK()\nOWNER(g:team)\nSUBSCRIBER(someone)"))
        self.assertEqual((code, stderr), (0, ""))


if __name__ == "__main__":
    unittest.main(verbosity=2)
