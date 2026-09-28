import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


MAPS = """\
package main

type Table map[string]int

func maps(t Table, k string) int {
	m := map[string]int{}
	m["a"] = 1
	m["b"]++
	x := m["a"]
	delete(m, "b")
	s := []int{1}
	s[0] = 2
	t[k] += x
	delete(t, k)
	return x + s[0] + missing["z"]
}
"""

MAPS_INSTRUMENTED = """\
package main

type Table map[string]int

func maps(t Table, k string) int {
	m := map[string]int{}
	m[mapKW("a", "maps.go:7")] = 1
	m[mapKW("b", "maps.go:8")]++
	x := m[mapKR("a", "maps.go:9")]
	delete(m, mapKW("b", "maps.go:10"))
	s := []int{1}
	s[0] = 2
	t[mapKW(k, "maps.go:13")] += x
	delete(t, mapKW(k, "maps.go:14"))
	return x + s[0] + missing["z"]
}
"""

GLOBALS = """\
package main

func stub()

var global = map[int]int{1: 2}

func (t Table) size() int { return len(t) + global[1] }
"""

GLOBALS_INSTRUMENTED = """\
package main

func stub()

var global = map[int]int{1: 2}

func (t Table) size() int { return len(t) + global[mapKR(1, "globals.go:7")] }
"""

PROBE_SELF = """\
package main

func probeHelper(m map[int]int) int { return m[1] }
"""

CALLS = """\
package main

type T struct{}

func stub()

func (T) method() int { return 1 }

func plain() {
	stub()
}
"""

CALLS_INSTRUMENTED = """\
package main

type T struct{}

func stub()

func (T) method() int {
	recordCall("calls.go:7"); return 1 }

func plain() {
	recordCall("calls.go:9");
	stub()
}
"""

BROKEN = "package main\nfunc (\n"


def run_ay(*args, cwd):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=60,
        check=False,
    )


class ProbeTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-probe-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def write(self, name, content):
        path = self.root / name
        path.write_text(content)
        return path

    def test_mapinstr_wraps_map_keys(self):
        self.write("maps.go", MAPS)
        self.write("globals.go", GLOBALS)
        self.write("probe.go", PROBE_SELF)
        result = run_ay(
            "dev", "probe", "mapinstr", "maps.go", "globals.go", "probe.go",
            cwd=self.root,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "mapinstr: wrapped 2 reads + 5 writes across 2 files\n",
        )
        self.assertEqual((self.root / "maps.go").read_text(), MAPS_INSTRUMENTED)
        self.assertEqual((self.root / "globals.go").read_text(), GLOBALS_INSTRUMENTED)
        self.assertEqual((self.root / "probe.go").read_text(), PROBE_SELF)

    def test_mapinstr_defaults_to_package_files(self):
        self.write("maps.go", MAPS)
        self.write("maps_test.go", MAPS)
        result = run_ay("dev", "probe", "mapinstr", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "mapinstr: wrapped 1 reads + 5 writes across 1 files\n",
        )
        self.assertEqual((self.root / "maps_test.go").read_text(), MAPS)

    def test_mapinstr_errors(self):
        self.write("broken.go", BROKEN)
        cases = {
            "missing.go": (
                "mapinstr: read missing.go: open missing.go: "
                "no such file or directory\n"
            ),
            "broken.go": (
                "mapinstr: parse broken.go: broken.go:2:8: "
                "expected ')', found 'EOF'\n"
            ),
        }
        for name, message in cases.items():
            result = run_ay("dev", "probe", "mapinstr", name, cwd=self.root)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, message)

    @unittest.skipIf(os.geteuid() == 0, "root ignores file permissions")
    def test_mapinstr_reports_write_failure(self):
        path = self.write("globals.go", GLOBALS)
        path.chmod(0o444)
        result = run_ay("dev", "probe", "mapinstr", "globals.go", cwd=self.root)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "mapinstr: write globals.go: open globals.go: permission denied\n",
        )
        self.assertEqual(path.read_text(), GLOBALS)

    def test_callsite_injects_record_calls(self):
        self.write("calls.go", CALLS)
        self.write("probe_callsite.go", PROBE_SELF)
        result = run_ay(
            "dev", "probe", "callsite", "out/sites.txt",
            "calls.go", "probe_callsite.go",
            cwd=self.root,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "callsite: injected 2 sites across 2 files\n",
        )
        self.assertEqual((self.root / "calls.go").read_text(), CALLS_INSTRUMENTED)
        self.assertEqual((self.root / "probe_callsite.go").read_text(), PROBE_SELF)
        self.assertEqual(
            (self.root / "out/sites.txt").read_text(),
            "calls.go:7\ncalls.go:9\n",
        )

    def test_callsite_defaults_to_package_files(self):
        self.write("calls.go", CALLS)
        self.write("globals.go", GLOBALS)
        result = run_ay("dev", "probe", "callsite", "sites.txt", cwd=self.root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "callsite: injected 3 sites across 2 files\n",
        )
        self.assertEqual(
            (self.root / "sites.txt").read_text(),
            "calls.go:7\ncalls.go:9\nglobals.go:7\n",
        )

    def test_callsite_errors(self):
        result = run_ay("dev", "probe", "callsite", cwd=self.root)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(
            result.stderr,
            "usage: ay probe callsite <all-sites-out> [files...]\n",
        )
        self.write("broken.go", BROKEN)
        self.write("plainfile", "")
        (self.root / "outdir").mkdir()
        self.write("calls.go", CALLS)
        cases = {
            ("sites", "missing.go"): (
                "callsite: read missing.go: open missing.go: "
                "no such file or directory\n"
            ),
            ("sites", "broken.go"): (
                "callsite: parse broken.go: broken.go:2:8: "
                "expected ')', found 'EOF'\n"
            ),
            ("plainfile/sub/sites", "calls.go"): (
                "callsite: mkdir plainfile/sub: mkdir plainfile: not a directory\n"
            ),
            ("outdir", "calls.go"): (
                "callsite: write all-sites: open outdir: is a directory\n"
            ),
        }
        for args, message in cases.items():
            result = run_ay("dev", "probe", "callsite", *args, cwd=self.root)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, message)

    @unittest.skipIf(os.geteuid() == 0, "root ignores file permissions")
    def test_callsite_reports_write_failure(self):
        path = self.write("calls.go", CALLS)
        path.chmod(0o444)
        result = run_ay("dev", "probe", "callsite", "sites", "calls.go", cwd=self.root)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "callsite: write calls.go: open calls.go: permission denied\n",
        )
        self.assertEqual(path.read_text(), CALLS)


if __name__ == "__main__":
    unittest.main(verbosity=2)
