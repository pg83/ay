import base64
import http.server
import io
import json
import os
import socket
import socketserver
import ssl
import struct
import subprocess
import sys
import tarfile
import tempfile
import threading
import unittest
import urllib.parse
import zipfile
from pathlib import Path

import lib


SANDBOX_HOST = "sandbox.yandex-team.ru"
OAUTH_HOST = "oauth.yandex-team.ru"
MDS_HOST = "storage-int.mds.yandex.net"

# Go verifies TLS through the system trust store on macOS and ignores
# SSL_CERT_FILE, so the mock's test CA can only be trusted elsewhere.
TLS_MOCKABLE = sys.platform != "darwin"
NO_TLS_MOCK = "Go on macOS ignores SSL_CERT_FILE, so the mock CA cannot be trusted"


def tar_bytes(members):
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as archive:
        for name, content in members.items():
            data = content.encode()
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = 0o644
            archive.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


def zip_bytes(members):
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w") as archive:
        for name, content in members.items():
            archive.writestr(name, content)
    return buffer.getvalue()


def make_certificate(directory):
    key = directory / "mock.key"
    cert = directory / "mock.pem"
    subprocess.run(
        [
            "openssl", "req", "-x509", "-newkey", "ec",
            "-pkeyopt", "ec_paramgen_curve:prime256v1", "-nodes",
            "-keyout", key, "-out", cert, "-days", "2",
            "-subj", "/CN=ay-test-mock",
            "-addext", f"subjectAltName=DNS:{SANDBOX_HOST},DNS:{OAUTH_HOST}",
            "-addext", "basicConstraints=critical,CA:TRUE",
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return cert, key


POLL_INTERVAL = 0.02


class QuietHTTPServer(http.server.ThreadingHTTPServer):
    daemon_threads = True

    def handle_error(self, request, client_address):
        pass


class Request:
    def __init__(self, method, host, path, headers, body):
        self.method = method
        self.host = host
        self.path = path
        self.headers = headers
        self.body = body


class MockInternet:
    """Plain HTTP origin, forward HTTP proxy and TLS-terminating CONNECT proxy.

    Routes map (host, path-without-query) to (status, body bytes) or to a
    callable returning that pair.
    """

    def __init__(self, directory):
        self.routes = {}
        self.requests = []
        self.cert = None
        self.tls = None
        if TLS_MOCKABLE:
            self.cert, key = make_certificate(directory)
            self.tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            self.tls.load_cert_chain(self.cert, key)
        mock = self

        class Handler(http.server.BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"
            tls_host = None

            def do_CONNECT(self):
                host = self.path.split(":", 1)[0]
                self.send_response(200, "Connection established")
                self.end_headers()
                with mock.tls.wrap_socket(self.connection, server_side=True) as tls_socket:
                    inner = type("TLSHandler", (Handler,), {"tls_host": host})
                    inner(tls_socket, self.client_address, self.server)
                self.close_connection = True

            def route(self, method):
                parsed = urllib.parse.urlsplit(self.path)
                host = self.tls_host or parsed.hostname or self.headers["Host"].split(":")[0]
                length = int(self.headers.get("Content-Length") or 0)
                body = self.rfile.read(length) if length else b""
                mock.requests.append(Request(method, host, self.path, dict(self.headers), body))
                answer = mock.routes.get((host, parsed.path), (404, b"missing"))
                status, payload = answer() if callable(answer) else answer
                self.send_response(status)
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def do_GET(self):
                self.route("GET")

            def do_POST(self):
                self.route("POST")

            def log_message(self, *args):
                pass

        self.httpd = QuietHTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=self.httpd.serve_forever, args=(POLL_INTERVAL,), daemon=True).start()
        self.port = self.httpd.server_address[1]
        self.base = f"http://127.0.0.1:{self.port}"

    def serve(self, host, path, body, status=200):
        if isinstance(body, str):
            body = body.encode()
        self.routes[(host, path)] = (status, body)

    def local(self, path, body, status=200):
        self.serve("127.0.0.1", path, body, status)
        return self.base + path

    def requests_to(self, host):
        return [r for r in self.requests if r.host == host]

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def ssh_string(data):
    if isinstance(data, str):
        data = data.encode()
    return struct.pack(">I", len(data)) + data


def read_ssh_string(data, offset):
    (length,) = struct.unpack_from(">I", data, offset)
    start = offset + 4
    return data[start:start + length], start + length


class MockSSHAgent:
    """ssh-agent protocol subset: REQUEST_IDENTITIES (11) and SIGN_REQUEST (13).

    keys: list of (format, blob_tail, signature) where signature None makes the
    agent answer SSH_AGENT_FAILURE for that key. list_fails answers FAILURE to
    the identities request.
    """

    def __init__(self, path, keys, list_fails=False):
        self.keys = keys
        self.list_fails = list_fails
        self.signed = []
        agent = self

        class Handler(socketserver.StreamRequestHandler):
            def handle(self):
                while True:
                    header = self.rfile.read(4)
                    if len(header) < 4:
                        return
                    (length,) = struct.unpack(">I", header)
                    message = self.rfile.read(length)
                    reply = agent.answer(message)
                    self.wfile.write(struct.pack(">I", len(reply)) + reply)

        self.server = socketserver.ThreadingUnixStreamServer(str(path), Handler)
        self.server.daemon_threads = True
        threading.Thread(target=self.server.serve_forever, args=(POLL_INTERVAL,), daemon=True).start()

    @staticmethod
    def blob(key_format, tail):
        return ssh_string(key_format) + ssh_string(tail)

    def answer(self, message):
        failure = bytes([5])
        if message[0] == 11:
            if self.list_fails:
                return failure
            body = struct.pack(">I", len(self.keys))
            for key_format, tail, _ in self.keys:
                body += ssh_string(self.blob(key_format, tail)) + ssh_string("comment")
            return bytes([12]) + body
        if message[0] == 13:
            blob, offset = read_ssh_string(message, 1)
            data, _ = read_ssh_string(message, offset)
            for key_format, tail, signature in self.keys:
                if self.blob(key_format, tail) == blob:
                    self.signed.append((key_format, data))
                    if signature is None:
                        return failure
                    return bytes([14]) + ssh_string(ssh_string(key_format) + ssh_string(signature))
        return failure

    def close(self):
        self.server.shutdown()
        self.server.server_close()


class FetchCase(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory(prefix="ay-fetch-test-")
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.src = self.root / "src"
        self.bld = self.root / "bld"
        self.home = self.root / "home"
        for directory in (self.src, self.bld, self.home):
            directory.mkdir()
        self.net = MockInternet(self.root)
        self.addCleanup(self.net.close)

    def env(self, **extra):
        env = {
            key: value
            for key, value in os.environ.items()
            if key.upper() not in (
                "YA_TOKEN", "YA_USER", "SSH_AUTH_SOCK", "HTTP_PROXY",
                "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "SSL_CERT_FILE",
                "SSL_CERT_DIR",
            )
        }
        env.update({
            "HOME": str(self.home),
            "HTTP_PROXY": self.net.base,
            "HTTPS_PROXY": self.net.base,
        })
        if self.net.cert is not None:
            env["SSL_CERT_FILE"] = str(self.net.cert)
        env.update(extra)
        return env

    def ay(self, *args, env=None, check=True):
        result = subprocess.run(
            [str(lib.AY), *map(str, args)],
            env=env if env is not None else self.env(),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )
        if check and result.returncode != 0:
            raise AssertionError(
                f"exit {result.returncode}: {args!r}\n"
                f"--- stdout ---\n{result.stdout}--- stderr ---\n{result.stderr}"
            )
        return result

    def write(self, relative, content):
        path = self.src / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path


class FetchBase64Test(FetchCase):
    def test_decodes_into_nested_output(self):
        out = self.bld / "deep" / "vcs.json"
        payload = base64.b64encode(b'{"k": 1}\n').decode()
        self.ay("fetch", "base64", payload, out)
        self.assertEqual(out.read_bytes(), b'{"k": 1}\n')

    def test_rejects_wrong_arity_and_bad_payload(self):
        result = self.ay("fetch", "base64", "only-one", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("usage: ay fetch base64 <data> <out>", result.stderr)
        result = self.ay("fetch", "base64", "!!!", self.bld / "x", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("illegal base64 data", result.stderr)


class FetchURLTest(FetchCase):
    def test_default_output_name_unpacks_tar(self):
        url = self.net.local("/pkg/tool.tar", tar_bytes({"bin/tool": "t\n"}))
        self.ay("fetch", self.bld, self.src, url)
        name = url.replace("/", "_").replace(":", "_")
        self.assertEqual(
            (self.bld / "resources" / name / "bin" / "tool").read_text(), "t\n"
        )

    def test_zip_archive_falls_back_to_unzip(self):
        url = self.net.local("/tool.zip", zip_bytes({"z/file.txt": "zipped\n"}))
        out = self.bld / "zipped"
        self.ay("fetch", self.bld, self.src, url, out)
        self.assertEqual((out / "z" / "file.txt").read_text(), "zipped\n")

    def test_plain_file_is_copied_as_resource(self):
        url = self.net.local("/raw.bin", "not an archive\n")
        out = self.bld / "raw"
        self.ay("fetch", self.bld, self.src, url, out)
        self.assertEqual(sorted(p.name for p in out.iterdir()), ["resource"])
        self.assertEqual((out / "resource").read_text(), "not an archive\n")

    def test_existing_read_only_output_is_replaced(self):
        url = self.net.local("/new.tar", tar_bytes({"fresh.txt": "new\n"}))
        out = self.bld / "stale"
        locked = out / "locked"
        locked.mkdir(parents=True)
        (locked / "old.txt").write_text("old\n")
        locked.chmod(0o555)
        self.addCleanup(lambda: locked.exists() and locked.chmod(0o755))
        self.ay("fetch", self.bld, self.src, url, out)
        self.assertEqual(sorted(p.name for p in out.iterdir()), ["fresh.txt"])

    def test_http_error_status_fails(self):
        url = self.net.local("/gone.tar", "gone", status=404)
        result = self.ay("fetch", self.bld, self.src, url, self.bld / "gone", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"fetch: {url} returned 404 Not Found", result.stderr)

    def test_empty_url_fails(self):
        result = self.ay("fetch", self.bld, self.src, "", self.bld / "none", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("fetch: empty URL", result.stderr)

    def test_usage_error(self):
        result = self.ay("fetch", self.bld, self.src, check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("usage: ay fetch <build-root> <source-root> <uri> [output-dir]", result.stderr)


class FetchMappedAndScriptedTest(FetchCase):
    def test_mapping_conf_redirects_sandbox_id(self):
        self.net.local("/mirror/77.tgz", tar_bytes({"mapped.txt": "m\n"}))
        self.write("build/mapping.conf.json", json.dumps({
            "extensions": {"mirror": f"{self.net.base}/mirror"},
            "resources": {"77": "{mirror}/77.tgz"},
        }))
        self.ay("fetch", self.bld, self.src, "sbr:77")
        self.assertEqual((self.bld / "resources" / "77" / "mapped.txt").read_text(), "m\n")

    def test_sandbox_id_without_token_runs_fetch_from_sandbox_script(self):
        self.write("build/scripts/fetch_from_sandbox.py", (
            "import io, sys, tarfile\n"
            "args = sys.argv[1:]\n"
            "rid = args[args.index('--resource-id') + 1]\n"
            "dst = args[args.index('--copy-to') + 1]\n"
            "data = ('script ' + rid + '\\n').encode()\n"
            "with tarfile.open(dst, 'w') as t:\n"
            "    info = tarfile.TarInfo('from_script.txt')\n"
            "    info.size = len(data)\n"
            "    t.addfile(info, io.BytesIO(data))\n"
        ))
        self.ay("fetch", self.bld, self.src, "sbr:88", env=self.env(SSH_AUTH_SOCK=""))
        self.assertEqual(
            (self.bld / "resources" / "88" / "from_script.txt").read_text(),
            "script 88\n",
        )
        self.assertEqual(self.net.requests, [])

    def test_mds_keys_run_fetch_from_mds_script(self):
        self.write("build/scripts/fetch_from_mds.py", (
            "import sys\n"
            "args = sys.argv[1:]\n"
            "open(args[args.index('--copy-to') + 1], 'w').write('mds ' + args[args.index('--key') + 1])\n"
        ))
        self.ay("fetch", self.bld, self.src, "https://mds.example/1/2/3", self.bld / "u")
        self.assertEqual((self.bld / "u" / "resource").read_text(), "mds 1/2/3")
        self.ay("fetch", self.bld, self.src, "4/5/6", self.bld / "k")
        self.assertEqual((self.bld / "k" / "resource").read_text(), "mds 4/5/6")
        self.assertEqual(self.net.requests, [])


@unittest.skipUnless(TLS_MOCKABLE, NO_TLS_MOCK)
class FetchFromSandboxAPITest(FetchCase):
    def resource(self, rid, state="READY", mds="", multifile=False):
        info = {
            "state": state,
            "multifile": multifile,
            "http": {"proxy": f"{self.net.base}/proxy/{rid}"},
            "attributes": {"mds": mds} if mds else {},
        }
        self.net.serve(SANDBOX_HOST, f"/api/v1.0/resource/{rid}", json.dumps(info))

    def test_mds_copy_is_preferred(self):
        self.resource(10, mds="10/archive")
        self.net.serve(MDS_HOST, "/get-sandbox/10/archive", tar_bytes({"via_mds.txt": "mds\n"}))
        self.ay("fetch", self.bld, self.src, "sbr:10", env=self.env(YA_TOKEN=" env-token \n"))
        self.assertEqual((self.bld / "resources" / "10" / "via_mds.txt").read_text(), "mds\n")
        api = self.net.requests_to(SANDBOX_HOST)
        self.assertEqual([r.path for r in api], ["/api/v1.0/resource/10"])
        self.assertEqual(api[0].headers["Authorization"], "OAuth env-token")
        mds = self.net.requests_to(MDS_HOST)
        self.assertEqual(len(mds), 1)
        self.assertNotIn("Authorization", mds[0].headers)

    def test_failed_mds_falls_back_to_authenticated_proxy(self):
        self.resource(11, mds="11/archive", multifile=True)
        self.net.serve(MDS_HOST, "/get-sandbox/11/archive", "down", status=503)
        self.net.local("/proxy/11", tar_bytes({"via_proxy.txt": "proxy\n"}))
        (self.home / ".ya_token").write_text("file-token\n")
        self.ay("fetch", self.bld, self.src, "sbr:11")
        self.assertEqual((self.bld / "resources" / "11" / "via_proxy.txt").read_text(), "proxy\n")
        proxied = [r for r in self.net.requests if r.path.startswith("/proxy/11")]
        self.assertEqual(len(proxied), 1)
        self.assertEqual(proxied[0].path, "/proxy/11?origin=fetch-from-sandbox&stream=tgz")
        self.assertEqual(proxied[0].headers["Authorization"], "OAuth file-token")

    def test_every_source_failing_reports_last_error(self):
        self.resource(12)
        self.net.local("/proxy/12", "nope", status=500)
        result = self.ay("fetch", self.bld, self.src, "sbr:12", env=self.env(YA_TOKEN="t"), check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("/proxy/12?origin=fetch-from-sandbox returned 500 Internal Server Error", result.stderr)

    def test_resource_not_ready(self):
        self.resource(13, state="BROKEN")
        result = self.ay("fetch", self.bld, self.src, "sbr:13", env=self.env(YA_TOKEN="t"), check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("sandbox resource 13 is not READY (state=BROKEN)", result.stderr)

    def test_api_error_status(self):
        result = self.ay("fetch", self.bld, self.src, "sbr:14", env=self.env(YA_TOKEN="t"), check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("sandbox resource 14 API returned 404 Not Found", result.stderr)


@unittest.skipUnless(TLS_MOCKABLE, NO_TLS_MOCK)
class FetchTokenFromSSHAgentTest(FetchCase):
    def start_agent(self, keys, list_fails=False):
        agent = MockSSHAgent(self.root / "agent.sock", keys, list_fails)
        self.addCleanup(agent.close)
        return agent

    def oauth_forms(self):
        return [
            urllib.parse.parse_qs(r.body.decode())
            for r in self.net.requests_to(OAUTH_HOST)
        ]

    def test_signs_with_each_key_until_oauth_grants_a_token(self):
        agent = self.start_agent([
            ("ssh-rsa", "refused", None),
            ("ssh-ed25519", "rejected", "sig-rejected"),
            ("ssh-ed25519-cert-v01@openssh.com", "granted", "sig-granted"),
        ])
        self.resource_ready(20)
        answers = iter([(403, b"{}"), (200, b'{"access_token": "agent-token"}')])
        self.net.routes[(OAUTH_HOST, "/token")] = lambda: next(answers)
        self.ay("fetch", self.bld, self.src, "sbr:20", env=self.env(
            SSH_AUTH_SOCK=str(self.root / "agent.sock"), YA_USER="robot",
        ))
        self.assertEqual((self.bld / "resources" / "20" / "ok.txt").read_text(), "ok\n")
        self.assertEqual([f for f, _ in agent.signed], [
            "ssh-rsa", "ssh-ed25519", "ssh-ed25519-cert-v01@openssh.com",
        ])
        signed_data = agent.signed[0][1].decode()
        self.assertTrue(signed_data.endswith("f4d36b7671004ed9850148fa645acac6robot"))
        forms = self.oauth_forms()
        self.assertEqual(len(forms), 2)
        self.assertEqual(forms[0]["grant_type"], ["ssh_key"])
        self.assertEqual(forms[0]["login"], ["robot"])
        self.assertEqual(forms[0]["ts"][0] + "f4d36b7671004ed9850148fa645acac6robot", signed_data)
        self.assertEqual(
            forms[0]["ssh_sign"],
            [base64.urlsafe_b64encode(b"sig-rejected").decode().rstrip("=")],
        )
        self.assertNotIn("public_cert", forms[0])
        self.assertEqual(
            forms[1]["public_cert"],
            [base64.urlsafe_b64encode(MockSSHAgent.blob(
                "ssh-ed25519-cert-v01@openssh.com", "granted",
            )).decode().rstrip("=")],
        )
        api = self.net.requests_to(SANDBOX_HOST)
        self.assertEqual(api[0].headers["Authorization"], "OAuth agent-token")

    def resource_ready(self, rid):
        self.net.serve(SANDBOX_HOST, f"/api/v1.0/resource/{rid}", json.dumps({
            "state": "READY", "http": {"proxy": f"{self.net.base}/proxy/{rid}"},
        }))
        self.net.local(f"/proxy/{rid}", tar_bytes({"ok.txt": "ok\n"}))

    def assert_falls_back_to_script(self, env):
        self.write("build/scripts/fetch_from_sandbox.py", (
            "import sys\n"
            "args = sys.argv[1:]\n"
            "open(args[args.index('--copy-to') + 1], 'w').write('fallback')\n"
        ))
        self.ay("fetch", self.bld, self.src, "sbr:30", env=env)
        self.assertEqual(
            (self.bld / "resources" / "30" / "resource").read_text(), "fallback"
        )
        self.assertEqual(self.net.requests_to(SANDBOX_HOST), [])

    def test_undecodable_oauth_answer_yields_no_token(self):
        agent = self.start_agent([("ssh-ed25519", "k", "sig")])
        self.net.serve(OAUTH_HOST, "/token", "not json")
        self.assert_falls_back_to_script(self.env(
            SSH_AUTH_SOCK=str(self.root / "agent.sock"), YA_USER="robot",
        ))
        self.assertEqual(len(agent.signed), 1)
        self.assertEqual(len(self.oauth_forms()), 1)

    def test_unreachable_oauth_yields_no_token(self):
        self.start_agent([("ssh-ed25519", "k", "sig")])
        env = self.env(SSH_AUTH_SOCK=str(self.root / "agent.sock"), YA_USER="robot")
        env["HTTPS_PROXY"] = "http://127.0.0.1:1"
        self.assert_falls_back_to_script(env)

    def test_agent_refusing_to_list_yields_no_token(self):
        self.start_agent([], list_fails=True)
        self.assert_falls_back_to_script(self.env(
            SSH_AUTH_SOCK=str(self.root / "agent.sock"), YA_USER="robot",
        ))

    def test_unknown_user_without_login_name_yields_no_token(self):
        # A uid without a passwd entry and an empty USER leave no login name.
        unshare = ["unshare", "-U", "--map-user=3999999"]
        probe = subprocess.run([*unshare, "true"], stdout=subprocess.DEVNULL,
                               stderr=subprocess.DEVNULL, check=False)
        if probe.returncode != 0:
            self.skipTest("unprivileged user namespaces are unavailable")
        agent = self.start_agent([("ssh-ed25519", "k", "sig")])
        self.write("build/scripts/fetch_from_sandbox.py", (
            "import sys\n"
            "args = sys.argv[1:]\n"
            "open(args[args.index('--copy-to') + 1], 'w').write('anonymous')\n"
        ))
        result = subprocess.run(
            [*unshare, str(lib.AY), "fetch", str(self.bld), str(self.src), "sbr:31"],
            env=self.env(SSH_AUTH_SOCK=str(self.root / "agent.sock"), USER=""),
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30, check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.bld / "resources" / "31" / "resource").read_text(), "anonymous")
        self.assertEqual(agent.signed, [])

    def test_dead_agent_socket_yields_no_token(self):
        self.assert_falls_back_to_script(self.env(
            SSH_AUTH_SOCK=str(self.root / "missing.sock"),
        ))


class FetchSandboxCommandTest(FetchCase):
    def test_untar_rename_copy_and_mark_executable(self):
        archive = self.root / "resource.tar"
        archive.write_bytes(tar_bytes({"pkg/tool": "tool\n", "pkg/other": "other\n"}))
        untar = self.bld / "unpacked"
        tool = self.bld / "out" / "tool"
        raw = self.bld / "out" / "raw.tar"
        self.ay(
            "fetch", "sandbox", "--ya-start-command-file",
            "--resource-file", archive, "--resource-id", "5",
            "--untar-to", untar, "--rename", "pkg/tool", "--rename", "RESOURCE",
            "--executable", "--",
            "--ya-start-command-file", tool, raw, "--ya-end-command-file",
        )
        self.assertEqual(tool.read_text(), "tool\n")
        self.assertEqual(raw.read_bytes(), archive.read_bytes())
        self.assertEqual(tool.stat().st_mode & 0o777, 0o755)
        self.assertEqual(raw.stat().st_mode & 0o777, 0o755)
        self.assertFalse((untar / "pkg" / "tool").exists())
        self.assertEqual((untar / "pkg" / "other").read_text(), "other\n")

    def test_downloads_and_copies_single_output(self):
        self.net.local("/mirror/6", "payload\n")
        self.write("build/mapping.conf.json", json.dumps({
            "resources": {"6": f"{self.net.base}/mirror/6"},
        }))
        target = self.bld / "copy"
        self.ay(
            "fetch", "sandbox", "--source-root", self.src, "--resource-id", "6",
            "--copy-to-dir", target, "--", "named.bin",
        )
        self.assertEqual((target / "named.bin").read_text(), "payload\n")

    def test_copy_without_outputs_and_plain_rename(self):
        fetched = self.root / "fetched.bin"
        fetched.write_text("blob\n")
        target = self.bld / "copy"
        self.ay(
            "fetch", "sandbox", "--resource-file", fetched, "--resource-id", "7",
            "--copy-to-dir", target,
        )
        self.assertEqual((target / "resource").read_text(), "blob\n")
        moved = self.root / "moved-from"
        moved.write_text("moved\n")
        dst = self.bld / "renamed" / "file"
        self.ay(
            "fetch", "sandbox", "--resource-file", fetched, "--resource-id", "7",
            "--rename", moved, "--", dst,
        )
        self.assertEqual(dst.read_text(), "moved\n")
        self.assertFalse(moved.exists())

    def test_copy_errors(self):
        missing = self.root / "missing"
        result = self.ay(
            "fetch", "sandbox", "--resource-file", missing, "--resource-id", "9",
            "--rename", "RESOURCE", "--", self.bld / "out", check=False,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"open {missing}: no such file or directory", result.stderr)

        fetched = self.root / "fetched.bin"
        fetched.write_text("blob\n")
        occupied = self.bld / "copy" / "taken"
        occupied.mkdir(parents=True)
        result = self.ay(
            "fetch", "sandbox", "--resource-file", fetched, "--resource-id", "9",
            "--copy-to-dir", self.bld / "copy", "--", "taken", check=False,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"open {occupied}: is a directory", result.stderr)

        result = self.ay(
            "fetch", "sandbox", "--resource-file", self.bld, "--resource-id", "9",
            "--copy-to-dir", self.root / "dir-copy", check=False,
        )
        self.assertEqual(result.returncode, 1)
        # The copy_file_range fast path reports the directory source on the
        # write side; a plain read reports it on the read side.
        self.assertRegex(result.stderr, r"(read \S+|write \S+/dir-copy/resource: copy_file_range): is a directory")

    def test_argument_errors(self):
        result = self.ay("fetch", "sandbox", "--copy-to-dir", self.bld, check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("fetch sandbox: missing --resource-id", result.stderr)
        result = self.ay(
            "fetch", "sandbox", "--resource-file", self.root / "x", "--resource-id", "8",
            "--rename", "a", "--rename", "b", "--", "only", check=False,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("fetch sandbox: 2 renames exceed 1 outputs", result.stderr)


@unittest.skipUnless(TLS_MOCKABLE, NO_TLS_MOCK)
class ExecutorSandboxTokenTest(FetchCase):
    """The executor resolves the Sandbox token once and hands it to FETCH/SB nodes."""

    def setUp(self):
        super().setUp()
        self.agent = MockSSHAgent(self.root / "agent.sock", [("ssh-ed25519", "k", "sig")])
        self.addCleanup(self.agent.close)
        self.net.serve(OAUTH_HOST, "/token", '{"access_token": "agent-token"}')
        self.write(".arcadia.root", "")
        self.write("ya.conf", '[flags]\nOPENSOURCE = "yes"\n')

    def make(self, target):
        return self.ay(
            "make", "-j", "2", "-k", "--source-root", self.src, "-B", self.bld,
            "-I", self.root / "inst", target,
            env=self.env(SSH_AUTH_SOCK=str(self.root / "agent.sock"), YA_USER="robot"),
            check=False,
        )

    def test_fetch_nodes_share_one_token(self):
        self.write("build/platform/sb/ya.make", (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_RESOURCE(FIRST sbr:41)\n"
            "DECLARE_EXTERNAL_RESOURCE(SECOND sbr:42)\n"
            "END()\n"
        ))
        for rid in (41, 42):
            self.net.serve(SANDBOX_HOST, f"/api/v1.0/resource/{rid}", json.dumps({
                "state": "READY", "http": {"proxy": f"{self.net.base}/proxy/{rid}"},
            }))
            self.net.local(f"/proxy/{rid}", tar_bytes({f"r{rid}.txt": f"{rid}\n"}))
        result = self.make("build/platform/sb")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.net.requests_to(OAUTH_HOST)), 1)
        api = self.net.requests_to(SANDBOX_HOST)
        self.assertEqual(sorted(r.path for r in api), [
            "/api/v1.0/resource/41", "/api/v1.0/resource/42",
        ])
        self.assertEqual({r.headers["Authorization"] for r in api}, {"OAuth agent-token"})
        self.assertEqual(len(list(self.bld.glob("uid/*/*"))), 3)

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
