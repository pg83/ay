import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def run_make(files, *args, extra_env=None):
    with tempfile.TemporaryDirectory(prefix="ay-link-stream-") as directory:
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
            if key not in lib.TOOLCHAIN_ENV_VARS and key != "AY_DEBUG_PENDING"
        }
        env.update(extra_env or {})
        return subprocess.run(
            [
                str(lib.AY), "make", "-j0", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )


def fixture():
    return {
        "dup/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}"
            "COPY_FILE(a.o x.o)\nCOPY_FILE(b.o x.o)\nSRCS(y.cpp)\nEND()\n"
        ),
        "dup/a.o": "a\n",
        "dup/b.o": "b\n",
        "dup/y.cpp": "int y;\n",
        "one/ya.make": f"LIBRARY()\n{NO_PLATFORM}SRCS(one.cpp)\nEND()\n",
        "one/one.cpp": "int one;\n",
        "two/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(two.cpp)\nPEERDIR(one)\nEND()\n",
        "two/two.cpp": "int main(){return 0;}\n",
        "globalonly/ya.make": f"LIBRARY()\n{NO_PLATFORM}SRCS(GLOBAL g.cpp)\nEND()\n",
        "globalonly/g.cpp": "int g;\n",
    }


class StreamingEmitterTest(unittest.TestCase):
    def test_unfilled_reserved_member_is_reported_as_cycle(self):
        for args in (("-G", "dup"), ("dup",)):
            with self.subTest(args=args):
                result = run_make(fixture(), *args)
                self.assertEqual(result.returncode, 1)
                self.assertIn(
                    "finish: 1 pending node(s) form a dependency cycle",
                    result.stderr,
                )
                self.assertNotIn("pending node ", result.stderr)
                self.assertEqual(result.stdout, "")

    def test_debug_pending_lists_unresolved_slots(self):
        result = run_make(fixture(), "-G", "dup", extra_env={"AY_DEBUG_PENDING": "1"})
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "pending node 4 out=$(B)/dup/libdup.a unresolved deps: 2(nil-slot)\n",
            result.stderr,
        )

    def test_stream_without_dump_generates_every_target_silently(self):
        result = run_make(fixture(), "one", "two")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertEqual(result.stderr, "")

    def test_dump_writes_one_graph_per_target(self):
        result = run_make(fixture(), "-G", "one", "two")
        self.assertEqual(result.returncode, 0, result.stderr)
        decoder = json.JSONDecoder()
        text = result.stdout
        graphs = []
        position = 0
        while position < len(text):
            while position < len(text) and text[position].isspace():
                position += 1
            if position == len(text):
                break
            graph, position = decoder.raw_decode(text, position)
            graphs.append(graph)
        self.assertEqual(len(graphs), 2)
        results = [
            [node["outputs"][0] for node in graph["graph"] if node["uid"] in graph["result"]]
            for graph in graphs
        ]
        self.assertEqual(results, [["$(B)/one/libone.a"], ["$(B)/two/two"]])

    def test_repeated_target_without_archive_streams_cleanly(self):
        result = run_make(fixture(), "globalonly", "globalonly")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((result.stdout, result.stderr), ("", ""))
        graph = json.loads(run_make(fixture(), "-G", "globalonly").stdout)
        self.assertIn(
            "$(B)/globalonly/libglobalonly.global.a",
            [node["outputs"][0] for node in graph["graph"] if node["uid"] in graph["result"]],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
