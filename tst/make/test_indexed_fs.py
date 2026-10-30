import io
import os
import struct
import subprocess
import tarfile

from tst.backends import ay_only
from tst.make.lib import MakeTest, PROJECT


class IndexedMakeTest(MakeTest):
    def unreadable_file(self, relative):
        if self.backend == "ya":
            target = self.project / relative
            target.chmod(0)
            self.addCleanup(target.chmod, 0o600)
        else:
            # Encode permissions in the archive while its packer can still
            # read the fixture's contents from the host filesystem.
            self.unreadable_entries = {*getattr(self, "unreadable_entries", ()), f"{PROJECT}/{relative}"}

    def indexed_entry(self, entry):
        if entry.name in getattr(self, "unreadable_entries", ()):
            entry.mode &= ~0o444
        return entry

    def make(self, *flags, **options):
        if self.backend == "ya":
            return super().make(*flags, **options)
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w", dereference=True) as output:
            output.add(self.root / "build", arcname="build")
            for name in (".arcadia.root", "linters.make.inc", "ya.conf", "ya.make"):
                if (self.root / name).is_file():
                    output.add(self.root / name, arcname=name)
            output.dereference = False
            output.add(self.project, arcname=PROJECT, filter=self.indexed_entry)
        indexed = self.work / "source.indexed"
        with indexed.open("wb") as output:
            packed = subprocess.run([os.environ["AY_INDEX_BINARY"]], input=archive.getvalue(),
                                    stdout=output, stderr=subprocess.PIPE, timeout=30)
        self.assertEqual(packed.returncode, 0, packed.stderr.decode())
        previous = self.binary
        self.binary = os.environ["AY_GATE_TEST_BINARY"]
        try:
            return super().make("--source-root", str(indexed), *flags, **options)
        finally:
            self.binary = previous


class IndexedGraphTest(IndexedMakeTest):
    def test_missing_frontend_resource_configuration_is_reported(self):
        self.write("ya.make", "TS_PACKAGE()\nEND()\n")
        self.write("package.json", '{"name":"@e2e/missing-resources","version":"1.0.0","engines":{"node":"22.13.0"}}\n')
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        # The indexed fixture contains build/ and this project; mirror its
        # missing frontend metadata in ya's private source overlay.
        if self.backend == "ya":
            resources = self.root / "devtools/frontend_build_platform/erm/erm-packages.json"
            contents = resources.read_bytes()
            resources.unlink()
            self.addCleanup(resources.write_bytes, contents)
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no such file or directory", result.stderr.lower())

    def test_large_file_below_a_regular_file_is_rejected(self):
        self.write("ya.make", "UNION(answer)\nLARGE_FILES(not_directory/child/value.bin)\nEND()\n")
        self.write("not_directory", "not a directory\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not_directory", result.stderr)

    def test_large_file_outside_the_source_root_is_rejected(self):
        self.write("ya.make", "UNION(answer)\nLARGE_FILES(../../__ay_e2e_outside__.bin)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("__ay_e2e_outside__", result.stderr)

    def test_missing_sysincl_configuration_is_reported(self):
        self.copy_project()
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + f"\nSYSINCL={PROJECT}/missing_sysincl.yml\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing_sysincl.yml", result.stderr)

    def test_empty_test_data_directory_keeps_the_suite(self):
        self.write("ya.make", f"EXECTEST()\nDATA(arcadia/{PROJECT}/empty)\nRUN(echo answer)\nEND()\n")
        (self.project / "empty").mkdir()
        self.node(self.graph("-t", "--cache-tests"), "test-results/exectest/meta.json")

    def test_include_below_a_regular_file_is_missing(self):
        self.write("ya.make", "UNION(answer)\nINCLUDE(not_directory.inc/subdirectory/missing.inc)\nEND()\n")
        self.write("not_directory.inc", "not a directory\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not_directory.inc", result.stderr)

    def test_include_outside_the_source_root_is_rejected(self):
        self.write("ya.make", "UNION(answer)\nINCLUDE(${ARCADIA_ROOT}/../ay_e2e_outside.inc)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ay_e2e_outside.inc", result.stderr)

    def test_unreadable_include_is_rejected(self):
        self.write("ya.make", "UNION(answer)\nINCLUDE(settings.inc)\nEND()\n")
        self.write("settings.inc", "SET(E2E_VALUE 42)\n")
        self.unreadable_file("settings.inc")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("settings.inc", result.stderr)

    def test_cached_test_data_reports_unreadable_files(self):
        self.write("ya.make", f"EXECTEST()\nDATA(arcadia/{PROJECT}/data)\nRUN(echo answer)\nEND()\n")
        self.write("data/a_unreadable.txt", "42\n")
        self.write("data/z_after.txt", "17\n")
        self.unreadable_file("data/a_unreadable.txt")
        result = self.make("-t", "--cache-tests")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("a_unreadable.txt", result.stderr)

    def test_cached_exectest_reads_file_and_directory_data(self):
        self.write("ya.make", f"EXECTEST()\nDATA(arcadia/{PROJECT}/data arcadia/{PROJECT}/standalone.txt)\nRUN(echo answer)\nEND()\n")
        for name in ("data/value.txt", "data/nested/value.txt", "standalone.txt", "data/ignored.pyc",
                     "data/.cache/ignored.txt", "data/.svn/ignored.txt", "data/.idea/ignored.txt"):
            self.write(name, "42\n")
        graph = self.graph("-t", "--cache-tests")
        self.node(graph, "test-results/exectest/meta.json")

    def test_local_large_files_are_copied_from_the_indexed_source(self):
        self.write("ya.make", "UNION(answer)\nLARGE_FILES(value.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/value.txt)\nEND()\n")
        self.write("value.txt", "local large file\n")
        copied = [node for node in self.graph()["graph"] if node["outputs"] == [f"$(B)/{PROJECT}/value.txt"]]
        self.assertEqual(len(copied), 1)
        self.assertIn(f"$(S)/{PROJECT}/value.txt", copied[0]["inputs"])

    def copy_project(self, prefix=""):
        self.write("ya.make", prefix + "UNION(answer)\nCOPY_FILE(value.txt result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("value.txt", "indexed source\n")

    def test_copy_graph_from_indexed_source(self):
        self.copy_project()
        node = next(node for node in self.graph()["graph"]
                    if node["outputs"] == [f"$(B)/{PROJECT}/result.txt"])
        self.assertIn(f"$(S)/{PROJECT}/value.txt", node["inputs"])

    def test_makefile_larger_than_a_read_chunk(self):
        self.copy_project("# " + "x" * 1048600 + "\n")
        graph = self.graph()
        self.assertIn(f"$(B)/{PROJECT}/result.txt", {path for node in graph["graph"] for path in node["outputs"]})

    def test_symlink_is_not_a_source_file(self):
        self.copy_project()
        self.write("ya.make", "UNION(answer)\nCOPY_FILE(alias.txt result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        (self.project / "alias.txt").symlink_to("value.txt")
        (self.project / "empty").mkdir()
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("alias.txt", result.stderr)

    def test_missing_and_directory_include_files_report_errors(self):
        (self.project / "directory.inc").mkdir()
        for name in ("missing.inc", "directory.inc"):
            with self.subTest(include=name):
                self.write("ya.make", f"UNION(answer)\nINCLUDE({name})\nEND()\n")
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(name, result.stderr)


@ay_only("indexed-source reader errors in the aygate executable")
class IndexedSourceErrorsTest(MakeTest):
    def test_missing_truncated_and_corrupt_archives(self):
        magic = b"AYIDX\0\0\1"
        samples = (
            (None, "no such file"),
            (b"short", "not an indexed archive"),
            (b"?" * 64, "not an indexed archive"),
            (magic + bytes(56), "corrupt indexed archive header"),
            (magic + struct.pack("<QQQQQ", 1, 96, 0, 96, 100) + bytes(48), "corrupt indexed archive header"),
        )
        for index, (contents, message) in enumerate(samples):
            with self.subTest(index=index):
                indexed = self.work / f"invalid{index}.indexed"
                if contents is not None:
                    indexed.write_bytes(contents)
                result = subprocess.run([os.environ["AY_GATE_TEST_BINARY"], "make", "-j0", "-G",
                                         "--source-root", str(indexed), PROJECT],
                                        cwd=self.root, text=True, capture_output=True, timeout=30)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr.lower())

    def test_directory_cannot_be_memory_mapped_as_an_indexed_archive(self):
        indexed = self.work / "directory.indexed"
        indexed.mkdir()
        for index in range(16):
            (indexed / f"entry_{index:02d}").write_text("")
        self.assertGreaterEqual(indexed.stat().st_size, 64)
        result = subprocess.run([os.environ["AY_GATE_TEST_BINARY"], "make", "-j0", "-G",
                                 "--source-root", str(indexed), PROJECT],
                                cwd=self.root, text=True, capture_output=True, timeout=30)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("mmap " + str(indexed), result.stderr)
