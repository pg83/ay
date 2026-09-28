import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


class NonCanonicalModulePathTest(unittest.TestCase):
    def test_srcs_include_and_copy_file_spellings(self):
        files = {
            "m/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "INCLUDE(../common/flags.inc)\n"
                "INCLUDE(${ARCADIA_ROOT}/common/./more.inc)\n"
                "INCLUDE(sub/local.inc)\n"
                "INCLUDE(sub/slash.inc/)\n"
                "INCLUDE(${ARCADIA_ROOT}/root.inc)\n"
                "SRCS(sub/../a.cpp sub//b.cpp sub/./c.cpp ./e.cpp)\n"
                "COPY_FILE(src.h out/../dst.h)\n"
                "END()\n"
            ),
            "common/flags.inc": "CFLAGS(-DFROM_INCLUDE)\n",
            "common/more.inc": "CFLAGS(-DFROM_MORE)\n",
            "m/sub/local.inc": "CFLAGS(-DFROM_LOCAL)\n",
            "m/sub/slash.inc": "CFLAGS(-DFROM_SLASH)\n",
            # An INCLUDE inside a root-level file resolves against the root.
            "root.inc": "INCLUDE(nested.inc)\n",
            "nested.inc": "CFLAGS(-DFROM_NESTED)\n",
            "m/a.cpp": '#include "dst.h"\n',
            "m/sub/b.cpp": "",
            "m/sub/c.cpp": "",
            "m/e.cpp": "",
            "m/src.h": "",
            "build/scripts/fs_tools.py": "",
        }
        graph = lib.make(files, "m")
        objects = {
            "$(B)/m/a.cpp.o": "$(S)/m/a.cpp",
            "$(B)/m/_/sub/b.cpp.o": "$(S)/m/sub/b.cpp",
            "$(B)/m/_/sub/c.cpp.o": "$(S)/m/sub/c.cpp",
            "$(B)/m/e.cpp.o": "$(S)/m/e.cpp",
        }
        for output, source in objects.items():
            with self.subTest(output=output):
                node = lib.node_by_output(graph, output)
                self.assertEqual(source, node["inputs"][0])
                defines = [a for a in node["cmds"][0]["cmd_args"] if a.startswith("-DFROM_")]
                self.assertEqual(
                    [
                        "-DFROM_INCLUDE", "-DFROM_MORE", "-DFROM_LOCAL",
                        "-DFROM_SLASH", "-DFROM_NESTED",
                    ],
                    defines,
                )
        self.assertIn(
            "$(B)/m/dst.h", lib.node_by_output(graph, "$(B)/m/a.cpp.o")["inputs"]
        )
        self.assertEqual(
            ["$(S)/build/scripts/fs_tools.py", "$(S)/m/src.h"],
            lib.node_by_output(graph, "$(B)/m/dst.h")["inputs"],
        )


class DedupDebugTest(unittest.TestCase):
    FILES = {
        "app/ya.make": (
            "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "PEERDIR(one two)\nSRCS(main.cpp)\nEND()\n"
        ),
        "app/main.cpp": "#include <one/one.h>\nint main(){return 0;}\n",
        "one/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "PEERDIR(two)\nADDINCL(GLOBAL one/inc)\nSRCS(one.cpp)\nEND()\n"
        ),
        "one/one.h": "#include <shared.h>\n",
        "one/one.cpp": '#include "one.h"\n',
        "one/inc/shared.h": "",
        "two/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "SRCS(two.cpp)\nEND()\n"
        ),
        "two/two.cpp": "",
    }

    def run_make(self, extra_env):
        with tempfile.TemporaryDirectory(prefix="ay-include-dedup-") as directory:
            root = Path(directory)
            (root / ".arcadia.root").touch()
            (root / "ya.conf").write_text(
                '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
            )
            for relative, content in self.FILES.items():
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
            env = {
                key: value
                for key, value in os.environ.items()
                if key not in lib.TOOLCHAIN_ENV_VARS and key != "AY_DEBUG_DEDUP"
            }
            env.update(extra_env)
            return subprocess.run(
                [
                    str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                    "--source-root", str(root),
                    "--target-platform", "default-linux-aarch64",
                    "--host-platform", "default-linux-x86_64",
                    "app",
                ],
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
                timeout=30,
                check=True,
            )

    def test_borrow_tracking_does_not_change_the_graph(self):
        plain = self.run_make({})
        tracked = self.run_make({"AY_DEBUG_DEDUP": "1"})
        self.assertEqual(json.loads(plain.stdout), json.loads(tracked.stdout))
        self.assertNotIn("live dedupers", tracked.stderr)
        self.assertIn(
            "$(S)/one/inc/shared.h",
            lib.node_by_output(json.loads(tracked.stdout), "$(B)/app/main.cpp.o")["inputs"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
