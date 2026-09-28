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
VARIABLES = (
    "SET(SV foo)\n"
    "SET(SOFF off)\n"
    "SET(SZERO 0)\n"
    "SET(SEMPTY)\n"
    "SET(SNET net)\n"
    "SET(SONE 1)\n"
    "SET(SPATH a/b)\n"
)


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


def compiled_sources(graph):
    return {
        source.rsplit("/", 1)[-1]
        for node in graph["graph"]
        if node["kv"].get("p") == "CC"
        for source in node["inputs"]
        if source.startswith("$(S)/a/")
    }


def module(body, sources):
    files = {"a/ya.make": "LIBRARY()\n" + NO_PLATFORM + body + "END()\n"}
    for source in sources:
        files[f"a/{source}"] = "int x;\n"
    return files


class YaMakeConditionTest(unittest.TestCase):
    def evaluate(self, conditions, *args, prelude=VARIABLES):
        body = prelude
        sources = []
        for index, condition in enumerate(conditions):
            body += (
                f"IF ({condition})\nSRCS(t{index}.cpp)\n"
                f"ELSE()\nSRCS(f{index}.cpp)\nENDIF()\n"
            )
            sources += [f"t{index}.cpp", f"f{index}.cpp"]
        code, graph, stderr = ay_make(module(body, sources), *args)
        self.assertEqual(code, 0, stderr)
        compiled = compiled_sources(graph)
        result = {}
        for index, condition in enumerate(conditions):
            taken = f"t{index}.cpp" in compiled
            self.assertNotEqual(taken, f"f{index}.cpp" in compiled, condition)
            result[condition] = taken
        return result

    def test_truth_table(self):
        expected = {
            "yes": True,
            "no": False,
            "NOT no": True,
            "OPENSOURCE": True,
            "UNSET_VAR": False,
            "NOT UNSET_VAR": True,
            "CURDIR": True,
            "ARCADIA_ROOT": True,
            "SV": True,
            "SOFF": False,
            "SZERO": False,
            "SEMPTY": False,
            "SNET": False,
            "yes AND no": False,
            "yes OR no": True,
            "no OR (yes AND NOT no)": True,
            "NOT (yes AND no)": True,
            "ANDROID_API == 0": True,
            "ANDROID_API != 0": False,
            "ANDROID_API < 21": True,
            "ANDROID_API > 21": False,
            "ANDROID_API >= 0": True,
            "ANDROID_API >= 1": False,
            "ANDROID_API STARTS_WITH \"0\"": True,
            "SONE == 1": True,
            "SV == \"foo\"": True,
            "SV != \"foo\"": False,
            "yes == \"yes\"": True,
            "CURDIR == \"$(S)/a\"": True,
            "UNDEFINED_THING == \"UNDEFINED_THING\"": True,
            "MODDIR == \"a\"": True,
            "SV STARTS_WITH \"fo\"": True,
            "SV STARTS_WITH \"x\"": False,
            "SV MATCHES \"^f.o$\"": True,
            "SV MATCHES \"bar\"": False,
            "COMPILER_VERSION VERSION_GE \"18.1\"": True,
            "COMPILER_VERSION VERSION_LT \"18.1\"": False,
            "\"1.2\" VERSION_EQ \"1.2.0\"": True,
            "\"1.2\" VERSION_LE \"1.10\"": True,
            "\"1.10\" VERSION_GT \"1.9\"": True,
            "\"2\" VERSION_GT \"2.0.1\"": False,
            "DEFINED SV": True,
            "DEFINED UNSET_VAR": False,
            "NOT DEFINED UNSET_VAR": True,
            "SPATH == a/b": True,
            "BUILD_TYPE == \"DEBUG\"": True,
            "OS_LINUX AND ARCH_AARCH64": True,
            "ARCH_X86_64": False,
            "12 == 12": True,
            "\"x\" == \"x\"": True,
            "100 > 99": True,
        }
        self.assertEqual(self.evaluate(list(expected)), expected)

    def test_define_flag_binds_condition_variable(self):
        self.assertEqual(
            self.evaluate(["CUSTOM_SWITCH", "CUSTOM_VALUE == \"v1\""], "-DCUSTOM_SWITCH=yes", "-DCUSTOM_VALUE=v1"),
            {"CUSTOM_SWITCH": True, "CUSTOM_VALUE == \"v1\"": True},
        )

    def test_elseif_chain_and_nesting(self):
        body = (
            "IF (no)\nSRCS(a.cpp)\n"
            "ELSEIF (UNSET_VAR)\nSRCS(b.cpp)\n"
            "ELSEIF (OPENSOURCE)\n"
            "  IF (NOT ARCH_AARCH64)\nSRCS(c.cpp)\nELSE()\nSRCS(d.cpp)\nENDIF()\n"
            "ELSE()\nSRCS(e.cpp)\n"
            "ENDIF()\n"
            "IF (no)\nSRCS(f.cpp)\nELSEIF (no)\nSRCS(g.cpp)\nENDIF()\n"
        )
        code, graph, stderr = ay_make(module(body, [f"{name}.cpp" for name in "abcdefg"]))
        self.assertEqual(code, 0, stderr)
        self.assertEqual(compiled_sources(graph), {"d.cpp"})

    def test_evaluation_errors(self):
        cases = [
            ("ANDROID_API", 'macros: identifier "ANDROID_API" has int binding but is used in boolean position'),
            ('"foo"', 'macros: bare string "foo" cannot be evaluated as a boolean condition'),
            ("1", "macros: bare integer 1 cannot be evaluated as a boolean condition"),
            ('ANDROID_API == "x"', "macros: == operand type mismatch: left is int 0, right is string"),
            ('foo == "x"', 'macros: unknown IF identifier "foo"'),
            ("MODDIR == a", 'macros: unknown IF identifier "a"'),
            ("(yes AND no) == yes", "macros: unexpected cond kind 4 in comparator operand position"),
            ('SV MATCHES "("', "error parsing regexp: missing closing ): `(`"),
            ('SV VERSION_FOO "1"', 'macros: unknown version operator "VERSION_FOO"'),
            ('DEFINED "x"', "macros: DEFINED expects a variable name, got kind 1"),
            ('ANDROID_API < "x"', "macros: < requires int operands, got left=int right=string"),
            ("SV < 1", "macros: < requires int operands, got left=string right=int"),
        ]
        for condition, expected in cases:
            with self.subTest(condition=condition):
                body = VARIABLES + f"IF ({condition})\nSRCS(x.cpp)\nENDIF()\n"
                code, _, stderr = ay_make(module(body, ["x.cpp"]))
                self.assertEqual(code, 1)
                self.assertEqual(stderr, expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
