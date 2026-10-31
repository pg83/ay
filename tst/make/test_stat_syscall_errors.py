import io
import os
import shutil
import subprocess
import tarfile

from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("native stat syscall fault diagnostics without graph assertions")
class StatSyscallErrorsTest(MakeTest):
    def stat_fault(self, path, binary, source_root):
        tracer = shutil.which("strace")
        self.assertIsNotNone(tracer, "stat fault diagnostics require strace")
        trace = self.work / "stat.trace"
        calls = "%stat,%fstat,statx"
        environment = os.environ.copy()
        for key in ("CC", "CXX", "CPP", "LD", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS"):
            environment.pop(key, None)
        environment.update(YA_TC="no", YA_NO_RESPAWN="yes", PYTHONDONTWRITEBYTECODE="1")
        result = subprocess.run(
            [tracer, "-qq", "-f", "-e", "trace=" + calls, "-P", str(path),
             "-e", "inject=" + calls + ":error=EIO", "-o", str(trace),
             binary, "make", "-j0", "--sandboxing", "--source-root", str(source_root), self.project_name],
            cwd=self.root, env=environment, capture_output=True, text=True, timeout=120,
        )
        self.assertIn("EIO", trace.read_text())
        self.assertIn("(INJECTED)", trace.read_text())
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("input/output error", result.stderr.lower())
        self.assertIn(path.name, result.stderr)
        return result

    def test_large_file_stat_io_errors_are_reported(self):
        self.write("ya.make", "UNION(answer)\nLARGE_FILES(value.bin)\nEXPLICIT_OUTPUTS(${BINDIR}/value.bin)\nEND()\n")
        self.write("value.bin", "42\n")
        self.stat_fault(self.project / "value.bin", self.binary, self.root)

    def test_indexed_source_fstat_io_errors_are_reported(self):
        self.write("ya.make", "UNION(answer)\nEND()\n")
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as output:
            output.add(self.project / "ya.make", arcname=self.project_name + "/ya.make")
        indexed = self.work / "source.indexed"
        with indexed.open("wb") as output:
            packed = subprocess.run([os.environ["AY_INDEX_BINARY"]], input=archive.getvalue(),
                                    stdout=output, stderr=subprocess.PIPE, timeout=30)
        self.assertEqual(packed.returncode, 0, packed.stderr.decode())
        self.stat_fault(indexed, os.environ["AY_GATE_TEST_BINARY"], indexed)
