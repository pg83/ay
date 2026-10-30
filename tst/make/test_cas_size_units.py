import subprocess

from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("cas analyze command output; no graph assertions")
class CasSizeUnitsTest(MakeTest):
    def test_megabyte_sizes_use_binary_units(self):
        cas = self.work / "cas"
        cas.mkdir()
        (cas / "payload").write_bytes(b"x" * (1024 * 1024))
        result = subprocess.run(
            [self.binary, "dev", "cas", "analyze", "--min-len=1048576", str(cas)],
            capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("min-len 1.00 MiB", result.stdout)
        self.assertIn("total size       1.00 MiB", result.stdout)
