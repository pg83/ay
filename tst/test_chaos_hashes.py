import os
import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def project():
    """A program over three libraries with header chains, generated headers and a tool."""
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


def wide_project(count):
    """Libraries in a peer chain, each exporting a global include directory."""
    files = {}
    for i in range(count):
        peers = " ".join(f"l{j}" for j in range(max(0, i - 3), i))
        peerdir = f"PEERDIR({peers})\n" if peers else ""
        files[f"l{i}/ya.make"] = (
            f"LIBRARY()\n{BARE}{peerdir}ADDINCL(GLOBAL l{i}/inc)\nSRCS(x.cpp)\nEND()\n"
        )
        files[f"l{i}/x.cpp"] = "int x(){return 0;}\n"
        files[f"l{i}/inc/.keep"] = ""
    files["app/ya.make"] = (
        f"PROGRAM()\n{BARE}PEERDIR({' '.join(f'l{i}' for i in range(count))})\n"
        "SRCS(main.cpp)\nEND()\n"
    )
    files["app/main.cpp"] = "int main(){return 0;}\n"
    return files


def plain(text):
    return text.replace("\x1b[31m", "").replace("\x1b[33m", "").replace("\x1b[0m", "")


class ChaosHashesTest(unittest.TestCase):
    """Hashes behind the seam: narrowed halves collide on real inputs."""

    @classmethod
    def setUpClass(cls):
        cls.files = project()
        cls.expected = lib.make(cls.files, "app")

    def assert_same_graph(self, words):
        self.assertEqual(lib.make(self.files, "app", env={"AY_CHAOS": words}), self.expected)

    def test_colliding_keys_keep_the_graph(self):
        for words in ("xxh-intern-hi=0", "xxh-intern-hi=15", "xxh-slice-hi=0",
                      "hash-bucket-h1=0", "hash-list-h1=0"):
            with self.subTest(words=words):
                self.assert_same_graph(words)

    def test_zero_verify_halves_keep_the_graph(self):
        for words in ("xxh-intern-lo=0", "xxh-slice-lo=0", "hash-bucket-h2=0", "hash-list-h2=0"):
            with self.subTest(words=words):
                self.assert_same_graph(words)

    def test_epoch_wraparound_clears_overflowed_buckets(self):
        for resets_before_wrap in (16, 64, 256):
            with self.subTest(resets_before_wrap=resets_before_wrap):
                self.assert_same_graph(f"hash-bucket-h1=0 epoch-start={2**32 - resets_before_wrap}")

    def test_bucket_overflow_is_reported_in_verbose_mode(self):
        result = lib.make_process(self.files, "app", "--verbose", env={"AY_CHAOS": "hash-bucket-h1=0"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(
            plain(result.stderr),
            r"(?m)^bucket-hash: \d+ h1 mismatches, \d+ buckets in overflow "
            r"\(pair-collision headroom shrinking\)$",
        )

    def test_pairs_colliding_in_both_halves_are_rejected(self):
        wide = wide_project(20)
        for files, words, message in (
            (self.files, "hash-bucket-h1=3 hash-bucket-h2=7",
             r"BucketCache: bucket hash pair collision \(h1=0x[0-9a-f]+ h2=0x[0-9a-f]+, \d+ elems\)"),
            (self.files, "hash-list-h1=3 hash-list-h2=3",
             r"BucketCache: bucket-list hash pair collision \(h1=0x[0-9a-f]+ h2=0x[0-9a-f]+, \d+ buckets\)"),
            (wide, "xxh-slice-hi=3 xxh-slice-lo=7",
             r"SliceCache: hash pair collision \(h1=0x[0-9a-f]+ h2=0x[0-9a-f]+, \d+ elems\)"),
        ):
            with self.subTest(words=words):
                result = lib.make_process(files, "app", env={"AY_CHAOS": words})
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertEqual(result.stdout, "")
                self.assertRegex(plain(result.stderr), rf"\A{message}\n\Z")


class ChaosPerfBucketHashTest(unittest.TestCase):
    def test_stress_reports_the_first_pair_collision(self):
        result = lib.run_process(
            "dev", "perf", "buckethash", timeout=120,
            env={**os.environ, "AY_CHAOS": "hash-bucket-h1=3 hash-bucket-h2=7"},
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertRegex(
            result.stdout.splitlines()[-1],
            r"^PAIR COLLISION after \d+ sequences \(\d+ distinct, [0-9.]+s\): "
            r"h1=0x[0-9a-f]{16} h2=0x[0-9a-f]{16}$",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
