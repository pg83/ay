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
SYSINCL = r'''- source_filter: "^a/libm/.*\\.c"
  includes:
    - complex.h: a/include/complex.h

- source_filter: "^a/prefix/"
  includes:
    - pre.h: a/include/pre.h

- source_filter: "^(?!a/skip|a/also).*"
  includes:
    - neg.h: a/include/neg.h

- source_filter: "^a/p(?!q|r)"
  includes:
    - pneg.h: a/include/pneg.h

- source_filter: "^(?!a/(x.y))"
  includes:
    - bad.h: a/include/bad.h
'''


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


class YaMakeSysinclFiltersTest(unittest.TestCase):
    def test_source_filters_select_sysincl_records(self):
        code, graph, stderr = ay_make({
            "build/sysincl/misc.yml": SYSINCL,
            "a/ya.make": "LIBRARY()\n" + NO_PLATFORM + "SRCS(libm/m.c prefix/p.cpp skip/s.cpp other.cpp pz.cpp)\nEND()\n",
            "a/libm/m.c": "#include <complex.h>\n",
            "a/prefix/p.cpp": "#include <pre.h>\n#include <complex.h>\n",
            "a/skip/s.cpp": "#include <neg.h>\n",
            "a/other.cpp": "#include <neg.h>\n#include <pneg.h>\n",
            "a/pz.cpp": "#include <pneg.h>\n",
            "a/include/complex.h": "", "a/include/pre.h": "", "a/include/neg.h": "",
            "a/include/pneg.h": "", "a/include/bad.h": "",
        }, "-k", "--verbose")
        self.assertEqual(code, 0, stderr)
        self.assertEqual({
            node["outputs"][0]: node["inputs"]
            for node in graph["graph"] if node["kv"]["p"] == "CC"
        }, {
            "$(B)/a/_/libm/m.c.o": ["$(S)/a/libm/m.c", "$(S)/a/include/complex.h"],
            "$(B)/a/_/prefix/p.cpp.o": ["$(S)/a/prefix/p.cpp", "$(S)/a/include/pre.h"],
            "$(B)/a/_/skip/s.cpp.o": ["$(S)/a/skip/s.cpp"],
            "$(B)/a/other.cpp.o": ["$(S)/a/other.cpp", "$(S)/a/include/neg.h"],
            "$(B)/a/pz.cpp.o": ["$(S)/a/pz.cpp", "$(S)/a/include/pneg.h"],
        })
        self.assertIn(
            'sysincl: misc.yml:17: source_filter "^(?!a/(x.y))" unsupported (sysincl: negative-lookahead'
            ' alt "a/(x.y)" has regex metacharacters; only literal prefixes are supported) — record disabled',
            stderr.splitlines(),
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
