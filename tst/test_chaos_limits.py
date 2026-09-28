import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def project():
    """A program over three libraries with header chains, a generated header and a tool."""
    files = {}
    for name, peers in (("a", ""), ("b", "a"), ("c", "a b")):
        headers = [f"{name}/h{i}.h" for i in range(6)]
        for i, header in enumerate(headers):
            nested = f'#include "{headers[i + 1]}"\n' if i + 1 < len(headers) else ""
            files[header] = f"#pragma once\n{nested}"
        sources = []
        for i in range(4):
            source = f"s{i}.cpp"
            sources.append(source)
            includes = "".join(f'#include "{p}/h0.h"\n' for p in peers.split())
            files[f"{name}/{source}"] = f'{includes}#include "{headers[i]}"\nint {name}{i}(){{return 0;}}\n'
        peerdir = f"PEERDIR({peers})\n" if peers else ""
        files[f"{name}/ya.make"] = (
            f"LIBRARY()\n{BARE}{peerdir}"
            f"RUN_PROGRAM(tools/gen OUT {name}_gen.h)\n"
            f"SRCS({' '.join(sources)})\nEND()\n"
        )
        files[f"{name}/s0.cpp"] = f'#include "{name}_gen.h"\n' + files[f"{name}/s0.cpp"]
    files["app/ya.make"] = (
        f"PROGRAM()\n{BARE}PEERDIR(a b c)\n"
        "RUN_PROGRAM(tools/gen OUT gen.h)\n"
        "SRCS(main.cpp)\nEND()\n"
    )
    files["app/main.cpp"] = '#include "gen.h"\n#include "c/h0.h"\nint main(){return 0;}\n'
    lib.tool_program(files, "tools/gen", "gen")
    return files


class ChaosLimitsTest(unittest.TestCase):
    """Internal limits that production inputs reach only at scale must not change the graph."""

    @classmethod
    def setUpClass(cls):
        cls.files = project()
        cls.expected = lib.make(cls.files, "app")

    def assert_same_graph(self, words):
        self.assertEqual(lib.make(self.files, "app", env={"AY_CHAOS": words}), self.expected)

    def test_minimal_tables_grow_without_losing_entries(self):
        self.assert_same_graph("table-hint=0")

    def test_epochs_wrap_around(self):
        for resets_before_wrap in (2, 16, 64, 256):
            with self.subTest(resets_before_wrap=resets_before_wrap):
                self.assert_same_graph(f"epoch-start={2**32 - resets_before_wrap}")

    def test_small_chunks_reach_the_chunk_cap(self):
        self.assert_same_graph("bump-chunk-bytes=64")

    def test_all_limits_at_once(self):
        self.assert_same_graph("table-hint=0 epoch-start=0xfffffffe bump-chunk-bytes=64")


if __name__ == "__main__":
    unittest.main(verbosity=2)
