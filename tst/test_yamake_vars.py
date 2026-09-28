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
LONG_VALUE = "a" * 200


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
                *args, "a",
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


class YaMakeVariablesTest(unittest.TestCase):
    def user_cflags(self, files, *args):
        code, graph, stderr = ay_make(files, *args)
        self.assertEqual(code, 0, stderr)
        cmd = lib.node_by_output(graph, "$(B)/a/x.cpp.o")["cmds"][0]["cmd_args"]
        start = cmd.index(LAST_COMMON_DEFINE) + 1
        return cmd[start:cmd.index(FIRST_NO_LIBC_FLAG, start)]

    def assert_parse_error(self, files, expected):
        code, _, stderr = ay_make(files)
        self.assertEqual(code, 1)
        self.assertEqual(stderr, expected)

    def test_expansion_forms(self):
        self.assertEqual(self.user_cflags(library(
            "SET(X foo)\n"
            "SET(Y ${X} bar)\n"
            f"SET(LONG {LONG_VALUE})\n"
            "CFLAGS(-D${X} $X -DE=$X.suf -DY=${Y} ${Y} \\$X $S/p $B/q $S $B \\$S"
            " ${UNKNOWN} $UNKNOWN ${lower} ${X-Y} $ -Dm=$X$X -Dn=${X ${X}}"
            " -Do=$-a ${ -D$LONG$LONG)"
        )), [
            "-Dfoo", "foo", "-DE=foo.suf", "-DY=foo", "bar", "foo", "bar",
            "foo", "$S/p", "$B/q", "$(S)", "$(B)", "$(S)", "${UNKNOWN}",
            "$UNKNOWN", "${lower}", "${X-Y}", "$", "-Dm=foofoo", "-Dn=${X",
            "foo}", "-Do=$-a", "${", "-D" + LONG_VALUE * 2,
        ])

    def test_deferred_and_chained_references(self):
        self.assertEqual(self.user_cflags(library(
            "SET(lower low)\nSET(A ${B})\nSET(B final)\n"
            "SET(C1 ${C2})\nSET(C2 ${C3})\nSET(C3 end)\nSET(P ${C1})\n"
            "CFLAGS(${lower} ${A} $A ${P})"
        )), ["low", "final", "final", "end"])

    def test_builtin_and_boolean_bindings(self):
        self.assertEqual(self.user_cflags(library(
            "SET(X yes)\nSET(Y no)\nSET(Z ${ARCADIA_ROOT}/dir)\n"
            "CFLAGS(-DX=${X} -DY=${Y} -DZ=${Z} ${CURDIR} ${BINDIR}"
            " ${ARCADIA_BUILD_ROOT} ${MODDIR} ${CUSTOM})"
        ), "-DCUSTOM=from-cli"), [
            "-DX=yes", "-DY=no", "-DZ=$(S)/dir", "$(S)/a", "$(B)/a", "$(B)",
            "a", "from-cli",
        ])

    def test_set_append_default_enable_disable(self):
        self.assertEqual(self.user_cflags(library(
            "SET_APPEND(V a)\nSET_APPEND(V b c)\n"
            "SET(W)\nSET_APPEND(W w)\n"
            "SET_APPEND()\n"
            "DEFAULT(D one)\nDEFAULT(D two)\n"
            "SET(S0 zero)\nDEFAULT(S0 one)\n"
            "DEFAULT(E)\n"
            "DEFAULT()\n"
            "ENABLE(E1)\nDISABLE(E2)\n"
            "IF (E1 AND NOT E2)\nCFLAGS(-DENABLED)\nENDIF()\n"
            "CFLAGS(${V} -DW=${W} -DD=${D} -DS0=${S0} -DE=${E}x -DE2=${E2})"
        )), [
            "-DENABLED", "a", "b", "c", "-DW=w", "-DD=one", "-DS0=zero",
            "-DE=x", "-DE2=no",
        ])

    def test_include_paths(self):
        self.assertEqual(self.user_cflags(library(
            "INCLUDE(common.inc)\n"
            "INCLUDE(${ARCADIA_ROOT}/shared/root.inc)\n"
            "INCLUDE(missing.inc)\n"
            "SET(INC sub/x.inc)\nINCLUDE(${INC})\n"
            "DEFAULT(DEF sub/y.inc)\nINCLUDE(${DEF})\n"
            "INCLUDE(${MODDIR}.inc)\n"
            "INCLUDE(cond.inc)",
            **{
                "a/common.inc": "CFLAGS(-DCOMMON)\nINCLUDE(../shared/nested.inc)\n",
                "shared/root.inc": "CFLAGS(-DROOT)\n",
                "shared/nested.inc": "CFLAGS(-DNESTED)\n",
                "a/sub/x.inc": "CFLAGS(-DX)\n",
                "a/sub/y.inc": "CFLAGS(-DY)\n",
                "a/a.inc": "CFLAGS(-DMODDIR)\n",
                "a/cond.inc": "IF (OPENSOURCE)\nCFLAGS(-DOS)\nELSE()\nCFLAGS(-DNOS)\nENDIF()\n",
            },
        )), ["-DCOMMON", "-DNESTED", "-DROOT", "-DX", "-DY", "-DMODDIR", "-DOS"])

    def test_include_once(self):
        self.assertEqual(self.user_cflags(library(
            "INCLUDE(once.inc)\nINCLUDE(once.inc)\n"
            "INCLUDE(yes.inc)\nINCLUDE(yes.inc)\n"
            "INCLUDE(twice.inc)\nINCLUDE(twice.inc)\n"
            "INCLUDE(no.inc)\nINCLUDE(no.inc)",
            **{
                "a/once.inc": "INCLUDE_ONCE()\nCFLAGS(-DONCE)\n",
                "a/yes.inc": "INCLUDE_ONCE(yes)\nCFLAGS(-DYES)\n",
                "a/twice.inc": "CFLAGS(-DTWICE)\n",
                "a/no.inc": "INCLUDE_ONCE(no)\nCFLAGS(-DNO)\n",
            },
        )), ["-DONCE", "-DYES", "-DTWICE", "-DTWICE", "-DNO", "-DNO"])

    def test_include_errors_carry_the_included_file_position(self):
        self.assert_parse_error(
            library("INCLUDE(cyc1.inc)", **{
                "a/cyc1.inc": "INCLUDE(cyc2.inc)\n",
                "a/cyc2.inc": "\n  INCLUDE(cyc1.inc)\n",
            }),
            "a/cyc2.inc:2:3: INCLUDE cycle: a/ya.make -> a/cyc1.inc -> a/cyc2.inc -> a/cyc1.inc",
        )
        self.assert_parse_error(
            library("INCLUDE(ya.make)"),
            "a/ya.make:5:1: INCLUDE cycle: a/ya.make -> a/ya.make",
        )
        self.assert_parse_error(
            library("INCLUDE(bad.inc)", **{"a/bad.inc": "CFLAGS(\n"}),
            "a/bad.inc:1:1: unterminated macro call \"CFLAGS\" (missing ')')",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
