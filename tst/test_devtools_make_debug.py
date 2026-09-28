import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


RED = "\x1b[31m"
RESET = "\x1b[0m"
YA_CONF = '[flags]\nOPENSOURCE = "yes"\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"

AUDIT_REPORT = """\
=== ya.make macros gen acknowledges but emits nothing for ===
  OWNER                                    × 2
  SUBSCRIBER                               × 1
=== uppercase service-keyword arguments seen per macro ===
  EXCLUDE_TAGS:
      GO_PROTO                       × 1
      ZZ_TAG                         × 1
  LICENSE:
      MIT                            × 1
      ZZZ_UNKNOWN_LIC                × 1
=== unhandled service-keyword arguments (not present as a "…" literal in *.go) ===
  EXCLUDE_TAGS:
      ZZ_TAG                         × 1
  LICENSE:
      MIT                            × 1
      ZZZ_UNKNOWN_LIC                × 1
"""

EMPTY_AUDIT_REPORT = """\
=== ya.make macros gen acknowledges but emits nothing for ===
  (none)
=== uppercase service-keyword arguments seen per macro ===
  (none)
=== unhandled service-keyword arguments (not present as a "…" literal in *.go) ===
  (none)
"""


class MakeDebugTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-make-debug-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / ".arcadia.root").touch()
        (self.root / "ya.conf").write_text(YA_CONF)

    def write(self, files):
        for relative, content in files.items():
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)

    def make(self, *args, target="lib", env=None):
        environment = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        environment.update(env or {})
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0",
                "--source-root", str(self.root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args,
                target,
            ],
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=60,
            check=False,
        )

    def library(self, *lines):
        self.write({
            "lib/ya.make": LIBRARY_HEAD + "".join(line + "\n" for line in lines)
            + "SRCS(a.c)\nEND()\n",
            "lib/a.c": "int f(void){return 0;}\n",
        })

    def test_dump_ignored_macros_reports_service_keywords(self):
        self.library(
            'LICENSE(MIT ZZZ_UNKNOWN_LIC "")',
            "OWNER(g:foo)",
            "SUBSCRIBER(g:bar)",
            "OWNER(g:baz)",
            "EXCLUDE_TAGS(ZZ_TAG GO_PROTO 123 lower)",
        )
        result = self.make("--dump-ignored-macros")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertEqual(result.stderr, AUDIT_REPORT)

    def test_dump_ignored_macros_without_findings(self):
        self.library()
        result = self.make("--dump-ignored-macros")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, EMPTY_AUDIT_REPORT)

    def test_audit_is_silent_unless_requested(self):
        self.library("OWNER(g:foo)", "LICENSE(ZZZ_UNKNOWN_LIC)")
        result = self.make()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")

    def test_unmodelled_service_keyword_is_fatal(self):
        self.library("CUDA_NVCC_FLAGS(ZZZ_BAD)")
        result = self.make()
        self.assertEqual(result.returncode, 1)
        self.assertTrue(result.stderr.startswith(
            RED + 'gen: macro CUDA_NVCC_FLAGS received service-keyword "ZZZ_BAD" '
            "that no handler models"
        ))

    def test_ownership_debug_reports_no_violations(self):
        self.library()
        plain = self.make("-G", "--sandboxing")
        checked = self.make("-G", "--sandboxing", env={"AY_DEBUG_OWNERSHIP": "1"})
        self.assertEqual(checked.returncode, 0, checked.stderr)
        self.assertEqual(checked.stderr, "ownership: 0 violating (field, site) pairs\n")
        self.assertEqual(json.loads(checked.stdout), json.loads(plain.stdout))

    def test_autoinclude_linters_make_inc(self):
        self.library()
        self.write({
            "build/conf/autoincludes.json": '["lib"]\n',
            "build/internal/conf/autoincludes.json": '["other"]\n',
            "lib/linters.make.inc": "CFLAGS(-DFROM_LINTERS)\n",
            "other/ya.make": LIBRARY_HEAD + "SRCS(b.c)\nEND()\n",
            "other/b.c": "int g(void){return 0;}\n",
        })
        for target, output, expected in (
            ("lib", "$(B)/lib/a.c.o", True),
            ("other", "$(B)/other/b.c.o", False),
        ):
            result = self.make("-G", target=target)
            self.assertEqual(result.returncode, 0, result.stderr)
            args = lib.node_by_output(json.loads(result.stdout), output)["cmds"][0]["cmd_args"]
            self.assertEqual("-DFROM_LINTERS" in args, expected, target)

    def test_malformed_autoincludes_is_fatal(self):
        self.library()
        self.write({"build/conf/autoincludes.json": '{"bad"\n'})
        result = self.make("-G")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            RED + "autoinclude: parse build/conf/autoincludes.json: "
            "unexpected end of JSON input" + RESET + "\n",
        )

    def test_uncached_node_and_build_only_inputs(self):
        self.write({
            "lib/ya.make": LIBRARY_HEAD
            + "CREATE_BUILDINFO_FOR(buildinfo_data.h)\nSRCS(a.cpp)\nEND()\n",
            "lib/a.cpp": '#include "buildinfo_data.h"\nint f(){return 0;}\n',
            "build/scripts/build_info_gen.py": "print(1)\n",
            "build/scripts/xargs.py": "print(1)\n",
            "build/scripts/yield_line.py": "print(1)\n",
        })
        result = self.make("-G")
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        info = lib.node_by_output(graph, "$(B)/lib/buildinfo_data.h")
        self.assertIs(info["cache"], False)
        self.assertEqual(info["inputs"], [])
        compile_node = lib.node_by_output(graph, "$(B)/lib/a.cpp.o")
        self.assertNotIn("cache", compile_node)
        self.assertEqual(compile_node["inputs"], ["$(B)/lib/buildinfo_data.h"])
        sandboxed = json.loads(self.make("-G", "--sandboxing").stdout)
        compile_node = lib.node_by_output(sandboxed, "$(B)/lib/a.cpp.o")
        self.assertIn("$(S)/lib/a.cpp", compile_node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
