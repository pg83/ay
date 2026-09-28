import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


def library(sources, *lines):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        + "".join(f"{line}\n" for line in lines)
        + f"SRCS({sources})\nEND()\n"
    )


def make_process(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-include-resolve-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
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
        return subprocess.run(
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


def inputs_of(graph, output):
    return set(lib.node_by_output(graph, output)["inputs"])


def closure(files, target, output):
    result = make_process(files, target, "-k")
    if result.returncode != 0:
        raise AssertionError(f"make failed:\n{result.stderr}")
    graph = json.loads(result.stdout)
    return set(lib.node_by_output(graph, output)["inputs"]), result.stderr


class IncludeResolutionOrderTest(unittest.TestCase):
    def test_quoted_include_prefers_includer_directory(self):
        files = {
            "m/ya.make": library("a.cpp", "ADDINCL(inc)"),
            "m/a.cpp": '#include "h.h"\n#include <angle.h>\n',
            "m/h.h": "",
            "m/angle.h": "",
            "inc/h.h": "",
            "inc/angle.h": "",
        }
        inputs, _ = closure(files, "m", "$(B)/m/a.cpp.o")
        self.assertIn("$(S)/m/h.h", inputs)
        self.assertNotIn("$(S)/inc/h.h", inputs)
        # Angle includes never look into the includer's directory.
        self.assertIn("$(S)/inc/angle.h", inputs)
        self.assertNotIn("$(S)/m/angle.h", inputs)

    def test_source_root_wins_over_addincl_and_first_addincl_wins(self):
        files = {
            "m/ya.make": library("a.cpp", "ADDINCL(inc1 inc2)"),
            "m/a.cpp": "#include <both.h>\n#include <second.h>\n#include <rooted.h>\n",
            "inc1/both.h": "",
            "inc2/both.h": "",
            "inc2/second.h": "",
            "inc1/rooted.h": "",
            "rooted.h": "",
        }
        inputs, _ = closure(files, "m", "$(B)/m/a.cpp.o")
        self.assertTrue(
            {"$(S)/inc1/both.h", "$(S)/inc2/second.h", "$(S)/rooted.h"} <= inputs
        )
        self.assertFalse({"$(S)/inc2/both.h", "$(S)/inc1/rooted.h"} & inputs)

    def test_peer_global_and_one_level_addincl(self):
        files = {
            "p/ya.make": library("p.cpp", "ADDINCL(GLOBAL p/include ONE_LEVEL p/one)"),
            "p/p.cpp": "",
            "p/include/global.h": "",
            "p/one/one_level.h": "",
            "m/ya.make": library("m.cpp", "PEERDIR(p)"),
            "m/m.cpp": "#include <global.h>\n#include <one_level.h>\n",
            "r/ya.make": library("r.cpp", "PEERDIR(m)"),
            "r/r.cpp": "#include <global.h>\n#include <one_level.h>\n",
        }
        direct, _ = closure(files, "m", "$(B)/m/m.cpp.o")
        self.assertTrue({"$(S)/p/include/global.h", "$(S)/p/one/one_level.h"} <= direct)
        transitive, warnings = closure(files, "r", "$(B)/r/r.cpp.o")
        self.assertIn("$(S)/p/include/global.h", transitive)
        self.assertNotIn("$(S)/p/one/one_level.h", transitive)
        self.assertIn("$(S)/r/r.cpp: unresolved include <one_level.h>", warnings)

    def test_build_root_addincl_ranks_against_source_addincl(self):
        def tree(order):
            return {
                "m/ya.make": library(
                    "a.cpp",
                    f"ADDINCL({order})",
                    "CONFIGURE_FILE(gen.h.in gen/shared.h)",
                    "CONFIGURE_FILE(gen.h.in gen/only_gen.h)",
                ),
                "m/a.cpp": "#include <shared.h>\n#include <only_gen.h>\n#include <only_src.h>\n",
                "m/gen.h.in": "",
                "src/shared.h": "",
                "src/only_src.h": "",
                "build/scripts/configure_file.py": "",
            }

        source_first, _ = closure(
            tree("src ${ARCADIA_BUILD_ROOT}/m/gen"), "m", "$(B)/m/a.cpp.o"
        )
        self.assertTrue(
            {"$(S)/src/shared.h", "$(B)/m/gen/only_gen.h", "$(S)/src/only_src.h"}
            <= source_first
        )
        self.assertNotIn("$(B)/m/gen/shared.h", source_first)
        build_first, _ = closure(
            tree("${ARCADIA_BUILD_ROOT}/m/gen src"), "m", "$(B)/m/a.cpp.o"
        )
        self.assertTrue(
            {"$(B)/m/gen/shared.h", "$(B)/m/gen/only_gen.h", "$(S)/src/only_src.h"}
            <= build_first
        )
        self.assertNotIn("$(S)/src/shared.h", build_first)

    def test_source_root_addincl_uses_linear_search(self):
        files = {
            "m/ya.make": library(
                "a.cpp",
                "ADDINCL(${ARCADIA_ROOT}/ inc ${ARCADIA_BUILD_ROOT}/m/gen)",
                "CONFIGURE_FILE(gen.h.in gen/generated.h)",
                "PEERDIR(p)",
            ),
            "m/a.cpp": (
                "#include <own.h>\n"
                "#include <generated.h>\n"
                "#include <global.h>\n"
                "#include <linux/base.h>\n"
                "#include <sub/../gone/missing.h>\n"
            ),
            "m/gen.h.in": "",
            "inc/own.h": "",
            "p/ya.make": library("p.cpp", "ADDINCL(GLOBAL p/include)"),
            "p/p.cpp": "",
            "p/include/global.h": "",
            "q/ya.make": library("q.cpp", "ADDINCL(GLOBAL ${ARCADIA_ROOT}/ GLOBAL q/include)"),
            "q/q.cpp": "",
            "q/include/peer.h": "",
            "n/ya.make": library("n.cpp", "PEERDIR(q)"),
            "n/n.cpp": "#include <peer.h>\n",
            "contrib/libs/linux-headers/linux/base.h": "",
            "build/scripts/configure_file.py": "",
        }
        inputs, warnings = closure(files, "m", "$(B)/m/a.cpp.o")
        self.assertEqual(
            {
                "$(S)/m/a.cpp",
                "$(S)/inc/own.h",
                "$(B)/m/gen/generated.h",
                "$(S)/m/gen.h.in",
                "$(S)/build/scripts/configure_file.py",
                "$(S)/p/include/global.h",
                "$(S)/contrib/libs/linux-headers/linux/base.h",
            },
            inputs,
        )
        self.assertIn("unresolved include <sub/../gone/missing.h>", warnings)
        peer_inputs, _ = closure(files, "n", "$(B)/n/n.cpp.o")
        self.assertEqual({"$(S)/n/n.cpp", "$(S)/q/include/peer.h"}, peer_inputs)

    def test_non_canonical_targets(self):
        files = {
            "m/ya.make": library("a.cpp", "ADDINCL(inc)"),
            "m/a.cpp": (
                '#include "../top.h"\n'
                '#include "./sibling.h"\n'
                "#include <sub//double.h>\n"
                "#include <sub/../up.h>\n"
                "#include <sub/./dot.h>\n"
                "#include <deep/file.h/.>\n"
                "#include <x/../y/../inc_only.h>\n"
            ),
            "m/sibling.h": "",
            "top.h": "",
            "sub/double.h": "",
            "up.h": "",
            "sub/dot.h": "",
            "inc/deep/file.h": "",
            "inc/inc_only.h": "",
        }
        inputs, warnings = closure(files, "m", "$(B)/m/a.cpp.o")
        self.assertTrue(
            {
                "$(S)/top.h", "$(S)/m/sibling.h", "$(S)/sub/double.h",
                "$(S)/up.h", "$(S)/sub/dot.h", "$(S)/inc/inc_only.h",
            }
            <= inputs
        )
        self.assertNotIn("$(S)/inc/deep/file.h", inputs)
        # A trailing "/." names a directory, which a regular file is not.
        self.assertEqual(
            ["unresolved include <deep/file.h/.>"],
            [
                line[line.index("unresolved"):line.index(" \u2014")]
                for line in warnings.splitlines()
                if "missing-include" in line
            ],
        )

    def test_linux_headers_base_search_path(self):
        files = {
            "m/ya.make": library("a.cpp", "ADDINCL(inc)"),
            "m/a.cpp": "#include <linux/types.h>\n#include <asm/nf.h>\n",
            "inc/unrelated.h": "",
            "contrib/libs/linux-headers/linux/types.h": "",
            "contrib/libs/linux-headers/_nf/asm/nf.h": "",
        }
        inputs, _ = closure(files, "m", "$(B)/m/a.cpp.o")
        self.assertTrue(
            {
                "$(S)/contrib/libs/linux-headers/linux/types.h",
                "$(S)/contrib/libs/linux-headers/_nf/asm/nf.h",
            }
            <= inputs
        )

    def test_generated_headers_resolve_from_source_and_generated_includers(self):
        files = {
            "m/ya.make": library(
                "a.cpp",
                "CONFIGURE_FILE(first.h.in first.h)",
                "CONFIGURE_FILE(second.h.in second.h)",
            ),
            "m/a.cpp": '#include "first.h"\n',
            "m/first.h.in": "#include <m/second.h>\n",
            "m/second.h.in": "",
            "build/scripts/configure_file.py": "",
        }
        inputs, _ = closure(files, "m", "$(B)/m/a.cpp.o")
        self.assertTrue({"$(B)/m/first.h", "$(B)/m/second.h"} <= inputs)

    def test_same_target_under_different_scan_configs(self):
        files = {
            "app/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PEERDIR(one two)\nSRCS(main.cpp)\nEND()\n"
            ),
            "app/main.cpp": "#include <common.h>\nint main(){return 0;}\n",
            "one/ya.make": library("one.cpp", "ADDINCL(inc1)"),
            "one/one.cpp": "#include <common.h>\n",
            "two/ya.make": library("two.cpp", "ADDINCL(inc2)"),
            "two/two.cpp": "#include <common.h>\n",
            "inc1/common.h": "",
            "inc2/common.h": "",
        }
        result = make_process(files, "app", "-k")
        graph = json.loads(result.stdout)
        self.assertIn("$(S)/inc1/common.h", inputs_of(graph, "$(B)/one/one.cpp.o"))
        self.assertIn("$(S)/inc2/common.h", inputs_of(graph, "$(B)/two/two.cpp.o"))
        self.assertIn(
            "app/main.cpp: unresolved include <common.h>", result.stderr
        )

    def test_wide_fanout_is_resolved_identically_for_every_includer(self):
        headers = [f"h{i:04d}.h" for i in range(1500)]
        source = "".join(f"#include <{header}>\n" for header in headers)
        files = {
            "m/ya.make": library("a.cpp b.cpp", "ADDINCL(inc)"),
            "m/a.cpp": source,
            "m/b.cpp": source,
        }
        for header in headers:
            files[f"inc/{header}"] = ""
        inputs, warnings = closure(files, "m", "$(B)/m/a.cpp.o")
        expected = {f"$(S)/inc/{header}" for header in headers}
        self.assertEqual(expected | {"$(S)/m/a.cpp"}, inputs)
        b_inputs, _ = closure(files, "m", "$(B)/m/b.cpp.o")
        self.assertEqual(expected | {"$(S)/m/b.cpp"}, b_inputs)
        self.assertEqual("", warnings)


class UnresolvedIncludeTest(unittest.TestCase):
    FILES = {
        "m/ya.make": library("a.cpp b.cpp"),
        "m/a.cpp": '#include "missing.h"\n#include <missing_angle.h>\n',
        "m/b.cpp": '#include "missing.h"\n',
    }

    def test_unresolved_include_is_fatal_without_keep_going(self):
        result = make_process(self.FILES, "m")
        self.assertEqual(1, result.returncode)
        self.assertIn(
            'missing-include: $(S)/m/a.cpp: unresolved include "missing.h"',
            result.stderr,
        )
        self.assertEqual("", result.stdout)

    def test_keep_going_reports_each_unresolved_include(self):
        result = make_process(self.FILES, "m", "-k")
        self.assertEqual(0, result.returncode)
        lines = [line for line in result.stderr.splitlines() if "missing-include" in line]
        self.assertEqual(3, len(lines))
        self.assertIn("$(S)/m/a.cpp: unresolved include <missing_angle.h>", result.stderr)
        self.assertIn('$(S)/m/b.cpp: unresolved include "missing.h"', result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
