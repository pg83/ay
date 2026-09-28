import os
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import lib


SDE = os.environ.get("AY_TEST_SDE", "")


def wide_closure():
    """One source including 300 headers: closure buckets of 16 elements and more take the SIMD path."""
    return {
        "wide/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n",
        "wide/a.cpp": "".join(f'#include "h{i}.h"\n' for i in range(300)),
        **{f"wide/h{i}.h": "" for i in range(300)},
    }


@unittest.skipUnless(SDE, "AY_TEST_SDE names no Intel SDE binary to emulate an AVX-512 CPU with")
class Avx512EmulationTest(unittest.TestCase):
    def test_bucket_hash_on_an_avx512_cpu_keeps_the_graph(self):
        files = wide_closure()
        expected = lib.make(files, "wide")
        with tempfile.TemporaryDirectory(prefix="ay-sde-") as directory:
            wrapper = Path(directory) / "ay"
            wrapper.write_text(f'#!/bin/sh\nexec "{SDE}" -spr -- "{lib.AY}" "$@"\n')
            wrapper.chmod(wrapper.stat().st_mode | stat.S_IXUSR)
            with mock.patch.object(lib, "AY", wrapper):
                emulated = lib.make(files, "wide", timeout=300)
        self.assertEqual(emulated, expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
