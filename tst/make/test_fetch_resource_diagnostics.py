import json
import subprocess

from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("fetch resource descriptor diagnostics; no graph assertions")
class FetchResourceDiagnosticsTest(MakeTest):
    def test_missing_host_resource_is_reported_before_creating_outputs(self):
        descriptor = {"pattern": "E2E_TOOL", "resources": [
            {"platform": "darwin-arm64", "resource": "sbr:1"},
        ]}
        for host in ("linux-x86_64", "default-linux-x86_64"):
            with self.subTest(host=host):
                output = self.work / "output"
                result = subprocess.run(
                    [self.binary, "fetch", "--resource-json", json.dumps(descriptor),
                     "--host-platform", host, str(self.work / "build"), str(self.root), str(output)],
                    capture_output=True, text=True, timeout=30,
                )
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(f'resource "E2E_TOOL" has no entry for host platform "{host}"', result.stderr)
                self.assertEqual(result.stdout, "")
                self.assertFalse(output.exists())
