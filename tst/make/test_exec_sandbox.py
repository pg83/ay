import io
import json
import tarfile
import unittest

from tst import lib
from tst.fetch.test_exec_fetch import (
    FetchCase, MockSSHAgent, NO_TLS_MOCK, OAUTH_HOST, SANDBOX_HOST, TLS_MOCKABLE, tar_bytes,
)
from tst.make.test_exec_make import built_kinds


@unittest.skipUnless(TLS_MOCKABLE, NO_TLS_MOCK)
class ExecutorSandboxTokenTest(FetchCase):
    """The executor resolves the Sandbox token once and hands it to FETCH/SB nodes."""

    def setUp(self):
        super().setUp()
        self.agent = MockSSHAgent(self.agent_sock, [("ssh-ed25519", "k", "sig")])
        self.addCleanup(self.agent.close)
        self.net.serve(OAUTH_HOST, "/token", '{"access_token": "agent-token"}')
        lib.seed_root(self.src)
        self.write("ya.conf", '[flags]\nOPENSOURCE = "yes"\n')

    def make(self, target):
        return self.ay(
            "make", "-j", "2", "-k", "--sandboxing", "--source-root", self.src, "-B", self.bld,
            "-I", self.root / "inst", target,
            env=self.env(SSH_AUTH_SOCK=self.agent_sock, YA_USER="robot"),
            check=False,
        )

    def test_fetch_nodes_share_one_token(self):
        self.write("build/platform/sb/ya.make", (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(FIRST sbr:41 FOR LINUX sbr:99 FOR DARWIN sbr:41 FOR LINUX-AARCH64)\n"
            "DECLARE_EXTERNAL_RESOURCE(SECOND sbr:42)\n"
            "END()\n"
        ))
        self.write("build/platform/python/ymake_python3/ya.make", (
            "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:43)\nEND()\n"))
        self.write("consumer/ya.make", (
            "UNION()\nPEERDIR(build/platform/sb build/platform/python/ymake_python3)\n"
            "RUN_PYTHON3(check.py ${FIRST_RESOURCE_GLOBAL}/r41.txt ${SECOND_RESOURCE_GLOBAL}/r42.txt "
            "checked.txt OUT checked.txt)\nEND()\n"))
        self.write("consumer/check.py", "import pathlib, sys\n"
                   "assert pathlib.Path(sys.argv[1]).read_text() == '41\\n'\n"
                   "assert pathlib.Path(sys.argv[2]).read_text() == '42\\n'\n"
                   "pathlib.Path(sys.argv[3]).write_text('checked\\n')\n")
        python = b'#!/bin/sh\nexec python3 "$@"\n'
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as tar:
            entry = tarfile.TarInfo("bin/python3")
            entry.size, entry.mode = len(python), 0o755
            tar.addfile(entry, io.BytesIO(python))
        for rid in (41, 42, 43):
            self.net.serve(SANDBOX_HOST, f"/api/v1.0/resource/{rid}", json.dumps({
                "state": "READY", "http": {"proxy": f"{self.net.base}/proxy/{rid}"},
            }))
            payload = archive.getvalue() if rid == 43 else tar_bytes({f"r{rid}.txt": f"{rid}\n"})
            self.net.local(f"/proxy/{rid}", payload)
        result = self.make("consumer")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.net.requests_to(OAUTH_HOST)), 1)
        api = self.net.requests_to(SANDBOX_HOST)
        self.assertEqual(sorted(r.path for r in api), [
            "/api/v1.0/resource/41", "/api/v1.0/resource/42", "/api/v1.0/resource/43",
        ])
        self.assertEqual({r.headers["Authorization"] for r in api}, {"OAuth agent-token"})
        tasks = built_kinds(result.stderr)
        self.assertCountEqual(tasks, [
            ("FT", "$(B)/resources/FIRST"),
            ("FT", "$(B)/resources/SECOND"),
            ("FT", "$(B)/resources/YMAKE_PYTHON3"),
            ("PY", "$(B)/consumer/checked.txt"),
            ("UN", "$(B)/consumer/consumer.pkg.fake"),
        ])
        self.assertEqual(len(list(self.bld.glob("uid/*/*"))), len(tasks))
        self.assertEqual((self.root / "inst/consumer/checked.txt").read_text(), "checked\n")

    def test_from_sandbox_node_runs_fetch_sandbox(self):
        # Characterization: the SB command keeps ya's
        # "--resource-file $(RESOURCE_ROOT)/sbr/<id>/resource", which nothing
        # substitutes, so `ay fetch sandbox` never downloads and the node fails.
        self.write("sb/ya.make", (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "FROM_SANDBOX(FILE 123 OUT sb.cpp)\nSRCS(sb.cpp)\nEND()\n"
        ))
        result = self.make("sb")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            f" fetch sandbox --source-root {self.src} --ya-start-command-file"
            " --resource-file $(RESOURCE_ROOT)/sbr/123/resource --resource-id 123"
            " --copy-to-dir . -- sb.cpp --ya-end-command-file\n",
            result.stderr,
        )
        self.assertIn("open $(RESOURCE_ROOT)/sbr/123/resource: no such file or directory", result.stderr)
        self.assertEqual(len(self.net.requests_to(OAUTH_HOST)), 1)
        self.assertEqual(self.net.requests_to(SANDBOX_HOST), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
