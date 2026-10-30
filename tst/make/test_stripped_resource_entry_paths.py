import http.server
import io
import json
import subprocess
import tarfile
import threading

from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("native resource archive extraction through the fetch CLI")
class StrippedResourceEntryPathsTest(MakeTest):
    def test_stripped_entries_skip_empty_paths_and_reject_parent_traversal(self):
        payloads = {}

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                body = payloads[self.path]
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        outside = self.work / "outside.txt"
        outside.write_text("kept\n")
        for index, name in enumerate(("prefix", "prefix/../outside.txt")):
            with self.subTest(name=name):
                buffer = io.BytesIO()
                with tarfile.open(fileobj=buffer, mode="w") as archive:
                    entry = tarfile.TarInfo(name)
                    entry.size = 4
                    archive.addfile(entry, io.BytesIO(b"bad\n"))
                payloads[f"/{index}.tar"] = buffer.getvalue()
                output = self.work / f"output-{index}"
                descriptor = {"pattern": "E2E_ARCHIVE", "resource": f"http://127.0.0.1:{server.server_port}/{index}.tar", "strip_prefix": 1}
                result = subprocess.run([self.binary, "fetch", "--resource-json", json.dumps(descriptor), "--host-platform", "linux-x86_64", str(self.work / "build"), str(self.root), str(output)], capture_output=True, text=True, timeout=30)
                if index == 0:
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertFalse((output / "prefix").exists())
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("escapes resource output", result.stderr)
                self.assertEqual(outside.read_text(), "kept\n")
