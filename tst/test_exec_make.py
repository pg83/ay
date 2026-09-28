import http.server
import io
import json
import os
import re
import subprocess
import tarfile
import tempfile
import threading
import unittest
from pathlib import Path

import lib


# The fake compiler embeds the first line of every source into the object so
# archives and linked tools reveal what was compiled.
FAKE_CLANG = r'''#!/usr/bin/env python3
import os
import sys
import time

args = sys.argv[1:]
out = args[args.index("-o") + 1]
parts = []
for source in [a for a in args if a.endswith((".c", ".cpp"))]:
    text = open(source).read()
    if "FAIL_COMPILE" in text:
        sys.exit("clang: error: refusing " + os.path.basename(source))
    if "WARN_COMPILE" in text:
        print("clang: warning: " + os.path.basename(source), file=sys.stderr)
    if os.environ.get("SLOW_COMPILE") == os.path.basename(source):
        time.sleep(0.5)
    parts.append(os.path.basename(source) + "=" + text.splitlines()[0])
with open(out, "w") as f:
    f.write("obj " + " ".join(parts) + "\n")
'''

FAKE_PYTHON3 = '#!/bin/sh\nexec python3 "$@"\n'

VCS_INFO_PY = '''import sys
with open(sys.argv[2], "w") as f:
    f.write("const char* vcs = 0;\\n")
'''

LINK_LIB_PY = '''import sys
args = sys.argv[sys.argv.index("--") + 2:]
with open(args[0], "w") as f:
    for obj in args[1:]:
        f.write(open(obj).read())
'''

LINK_EXE_PY = '''import os
import sys
args = sys.argv[1:]
out = args[args.index("-o") + 1]
seen = []
for a in args:
    if a.startswith("@"):
        seen.append("cmdfile=" + open(a[1:]).read().replace("\\n", ";"))
    elif a.endswith((".o", ".a")):
        seen.append(os.path.basename(a) + ("" if os.path.exists(a) else "!missing"))
with open(out, "w") as f:
    f.write("#!/bin/sh\\n# " + " ".join(seen) + "\\necho tool-output \\"$@\\"\\n")
os.chmod(out, 0o755)
'''

# Expands @command-files recursively: nested files appear as [..] groups.
RUN_PY = '''import sys


def expand(arg):
    if not arg.startswith("@"):
        return arg
    lines = open(arg[1:]).read().splitlines()
    return "[" + " ".join(expand(line) for line in lines) + "]"


print("// " + " ".join(expand(a) for a in sys.argv[1:]))
print("int generated() { return 0; }")
'''


def tar_bytes(members):
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as archive:
        for name, content in members.items():
            info = tarfile.TarInfo(name)
            if isinstance(content, tuple):
                info.type = tarfile.SYMTYPE
                info.linkname = content[1]
                archive.addfile(info)
                continue
            data = content.encode()
            info.size = len(data)
            info.mode = 0o755
            archive.addfile(info, io.BytesIO(data))
    return buffer.getvalue()


TOOLCHAIN = {
    "/clang.tar": tar_bytes({
        "bin/clang++": FAKE_CLANG,
        "bin/clang": ("symlink", "clang++"),
        "bin/llvm-ar": FAKE_CLANG,
    }),
    "/lld.tar": tar_bytes({"bin/ld.lld": FAKE_CLANG}),
    "/py.tar": tar_bytes({"bin/python3": FAKE_PYTHON3}),
}

ANSI = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")


def plain(text):
    return ANSI.sub("", text)


class ResourceServer:
    def __init__(self, blobs):
        self.blobs = blobs
        self.requests = []
        server = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                server.requests.append(self.path)
                body = server.blobs.get(self.path)
                if body is None:
                    self.send_error(404)
                    return
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        self.httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.httpd.daemon_threads = True
        threading.Thread(target=self.httpd.serve_forever, args=(0.02,), daemon=True).start()
        self.base = f"http://127.0.0.1:{self.httpd.server_address[1]}"

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


def library(sources, extra=""):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"{extra}SRCS({' '.join(sources)})\nEND()\n"
    )


def program(name, sources, extra=""):
    return (
        f"PROGRAM({name})\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"{extra}SRCS({' '.join(sources)})\nEND()\n"
    )


class Workspace:
    def __init__(self, test, files, extra_resources=""):
        self.tmp = tempfile.TemporaryDirectory(prefix="ay-exec-test-")
        test.addCleanup(self.tmp.cleanup)
        self.server = ResourceServer(TOOLCHAIN)
        test.addCleanup(self.server.close)
        self.root = Path(self.tmp.name).resolve()
        self.src = self.root / "src"
        self.bld = self.root / "bld"
        self.inst = self.root / "inst"
        self.home = self.root / "home"
        self.home.mkdir()
        base = self.server.base
        tree = {
            ".arcadia.root": "",
            "ya.conf": '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n',
            "build/platform/clang/ya.make": (
                "RESOURCES_LIBRARY()\n"
                f"DECLARE_EXTERNAL_RESOURCE(CLANG20 {base}/clang.tar)\n"
                f"{extra_resources.format(base=base)}END()\n"
            ),
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
            "build/platform/lld/ya.make": (
                f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(LLD_ROOT {base}/lld.tar)\nEND()\n"
            ),
            "build/platform/python/ymake_python3/ya.make": (
                f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 {base}/py.tar)\nEND()\n"
            ),
            "build/scripts/vcs_info.py": VCS_INFO_PY,
            "build/scripts/link_lib.py": LINK_LIB_PY,
            "build/scripts/link_exe.py": LINK_EXE_PY,
            "build/scripts/fs_tools.py": "",
            **files,
        }
        for relative, content in tree.items():
            self.write(relative, content)

    def write(self, relative, content):
        path = self.src / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def env(self, **extra):
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
            and key not in ("YA_TOKEN", "SSH_AUTH_SOCK", "YA_USER")
        }
        env["HOME"] = str(self.home)
        env.update(extra)
        return env

    def make(self, *args, targets=("tool",), layout=True, sandboxing=True,
             check=True, cwd=None, env=None):
        command = [
            str(lib.AY), "make",
            "--host-platform", "default-linux-x86_64",
            *(["--source-root", str(self.src), "-B", str(self.bld), "-I", str(self.inst)] if layout else []),
            *(["--sandboxing"] if sandboxing else []),
            *args,
            *targets,
        ]
        result = subprocess.run(
            command,
            env=env if env is not None else self.env(),
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=60,
            check=False,
        )
        result.stdout = result.stdout.decode()
        result.stderr = result.stderr.decode()
        if check and result.returncode != 0:
            raise AssertionError(
                f"exit {result.returncode}: {command!r}\n"
                f"--- stdout ---\n{result.stdout}--- stderr ---\n{result.stderr}"
            )
        return result

    def uid_files(self, bld=None):
        return sorted((bld or self.bld).glob("uid/*/*"))

    def meta_for(self, output):
        for path in self.uid_files():
            meta = json.loads(path.read_text())
            if any(key == output or key.startswith(output + "/") for key in meta):
                return path
        raise AssertionError(f"no cached node produced {output}")


BASIC = {
    "lib/ya.make": library(["a.cpp", "b.c"]),
    "lib/a.cpp": "int a(){return 0;}\n",
    "lib/b.c": "int b(){return 0;}\n",
    "tool/ya.make": program("tool", ["main.cpp"], "PEERDIR(lib)\n"),
    "tool/main.cpp": "int main(){return 0;}\n",
}


def built_kinds(stderr):
    return re.findall(r"\[(\w+)\] \{\d+/\d+\} (\S+)", plain(stderr))


class ExecBuildTest(unittest.TestCase):
    def test_builds_program_from_fetched_toolchain_and_installs_it(self):
        ws = Workspace(self, BASIC)
        result = ws.make("-j", "4", "-T", "--stat")
        installed = ws.inst / "tool" / "tool"
        self.assertTrue(installed.is_symlink())
        self.assertTrue(os.readlink(installed).startswith(str(ws.bld / "cas") + "/"))
        text = installed.read_text()
        self.assertIn(
            "# cmdfile= __vcs_version__.c.o main.cpp.o liblib.a",
            text,
        )
        run = subprocess.run([str(installed), "x"], stdout=subprocess.PIPE, text=True, check=True)
        self.assertEqual(run.stdout, "tool-output x\n")
        self.assertEqual(sorted(ws.server.requests), ["/clang.tar", "/lld.tar", "/py.tar"])
        self.assertEqual(sorted(built_kinds(result.stderr)), [
            ("AR", "$(B)/lib/liblib.a"),
            ("CC", "$(B)/lib/a.cpp.o"),
            ("CC", "$(B)/lib/b.c.o"),
            ("CC", "$(B)/tool/main.cpp.o"),
            ("CP", "$(B)/vcs.json"),
            ("FT", "$(B)/resources/CLANG20"),
            ("FT", "$(B)/resources/LLD_ROOT"),
            ("FT", "$(B)/resources/YMAKE_PYTHON3"),
            ("LD", "$(B)/tool/tool"),
        ])
        stats = plain(result.stderr)
        self.assertIn("\n10 longest tasks:\n", stats)
        self.assertIn("\nper-kind total:\n", stats)
        critical = stats.split("\ncritical path (")[1].splitlines()[1:]
        self.assertEqual([line.split()[-1] for line in critical], [
            "$(B)/tool/tool",
            "$(B)/lib/liblib.a",
            critical[2].split()[-1],
            "$(B)/resources/CLANG20",
        ])
        self.assertIn(critical[2].split()[-1], ("$(B)/lib/a.cpp.o", "$(B)/lib/b.c.o"))

        clang_meta = json.loads(ws.meta_for("$(B)/resources/CLANG20").read_text())
        self.assertEqual(clang_meta["$(B)/resources/CLANG20/bin/clang"], {"link": "clang++"})
        self.assertEqual(
            clang_meta["$(B)/resources/CLANG20/bin/clang++"],
            clang_meta["$(B)/resources/CLANG20/bin/llvm-ar"],
        )

    def test_rebuild_reuses_cache_and_reruns_only_changed_nodes(self):
        ws = Workspace(self, BASIC)
        ws.make("-j", "2")
        uids = ws.uid_files()
        (ws.bld / "uid" / "stray-file").write_text("not a directory\n")

        again = ws.make("-j", "2", "--stat")
        self.assertEqual(again.stderr, "")
        self.assertEqual(ws.uid_files(), uids)
        self.assertEqual(len(ws.server.requests), 3)

        ws.write("tool/main.cpp", "int main(){return 1;} // WARN_COMPILE\n")
        changed = ws.make("-j", "2")
        self.assertEqual(sorted(built_kinds(changed.stderr)), [
            ("CC", "$(B)/tool/main.cpp.o"),
            ("LD", "$(B)/tool/tool"),
        ])
        self.assertIn("\x1b[2K\rclang: warning: main.cpp\n", changed.stderr)
        self.assertEqual(len(ws.server.requests), 3)
        self.assertEqual(len(ws.uid_files()), len(uids) + 2)

    def test_clear_discards_cache_and_refetches(self):
        ws = Workspace(self, BASIC)
        ws.make("-j", "2")
        before = ws.uid_files()
        result = ws.make("-j", "2", "--clear", "-T")
        self.assertEqual(len(built_kinds(result.stderr)), 9)
        self.assertEqual(len(ws.server.requests), 6)
        self.assertEqual(
            [p.name for p in ws.uid_files()],
            [p.name for p in before],
        )

    def test_without_sandboxing_and_default_layout(self):
        ws = Workspace(self, BASIC)
        wrapper = ws.root / "wrap.sh"
        log = ws.root / "wrap.log"
        wrapper.write_text(f'#!/bin/sh\necho "$1" >> {log}\nexec "$@"\n')
        wrapper.chmod(0o755)
        ws.make(
            "-j", "3", "--cmd-prefix", f"clang++={wrapper}",
            "--source-root", str(ws.src),
            layout=False, sandboxing=False,
        )
        default_build = ws.home / ".ya" / "ay"
        self.assertEqual(len(ws.uid_files(default_build)), 9)
        installed = ws.src / "tool" / "tool"
        self.assertTrue(installed.is_symlink())
        self.assertTrue(os.readlink(installed).startswith(str(default_build / "cas") + "/"))
        wrapped = log.read_text().splitlines()
        self.assertEqual(len(wrapped), 2)
        self.assertTrue(all(line.endswith("/resources/CLANG20/bin/clang++") for line in wrapped))
        self.assertIn("/tmp/", wrapped[0])

    def test_target_from_working_directory(self):
        ws = Workspace(self, BASIC)
        ws.make("-j", "2", targets=(), cwd=ws.src / "lib")
        self.assertIn("obj a.cpp=int a(){return 0;}", (ws.inst / "lib" / "liblib.a").read_text())

    def test_stdout_capture_and_command_files(self):
        ws = Workspace(self, {
            "gen/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "RUN_PYTHON3(\n"
                "    run.py plain --ya-start-command-file a --ya-start-command-file b"
                " --ya-end-command-file c --ya-end-command-file tail"
                " --ya-start-command-file open\n"
                "    STDOUT gen.cpp\n"
                ")\nEND()\n"
            ),
            "gen/run.py": RUN_PY,
        })
        ws.make("-j", "2", targets=("gen",))
        self.assertEqual(
            (ws.inst / "gen" / "libgen.a").read_text(),
            "obj gen.cpp=// plain [a [b] c] tail [open]\n",
        )

    def test_stale_scratch_and_garbage_directories_are_removed(self):
        ws = Workspace(self, BASIC)
        ws.make("-j", "2")
        names = [p.name for p in ws.uid_files()]
        for path in ws.uid_files():
            path.unlink()
        for name in names:
            stale = ws.bld / "tmp" / name
            stale.mkdir(parents=True, exist_ok=True)
            (stale / "stale.txt").write_text("stale\n")
        locked = ws.bld / "grb" / "leftover" / "locked"
        locked.mkdir(parents=True)
        (locked / "file").write_text("garbage\n")
        locked.chmod(0o555)
        result = ws.make("-j", "4", "-T", env=ws.env(SLOW_COMPILE="a.cpp"))
        self.assertEqual(len(built_kinds(result.stderr)), 9)
        self.assertEqual(list(ws.bld.glob("tmp/*/stale.txt")), [])
        self.assertFalse((ws.bld / "grb" / "leftover").exists())


class ExecFailureTest(unittest.TestCase):
    def test_failing_command_aborts_build(self):
        ws = Workspace(self, {**BASIC, "lib/a.cpp": "FAIL_COMPILE\n"})
        result = ws.make("-j", "2", check=False)
        self.assertEqual(result.returncode, 1)
        stderr = plain(result.stderr)
        self.assertRegex(stderr, r"cmd failed \(ref=\d+\): exit status 1: \S+/resources/CLANG20/bin/clang\+\+ ")
        self.assertIn("\nclang: error: refusing a.cpp\n", stderr)
        self.assertFalse((ws.inst / "tool").exists())

    def test_keep_going_builds_independent_targets_and_reports_failed_roots(self):
        ws = Workspace(self, {
            **BASIC,
            "lib/a.cpp": "FAIL_COMPILE\n",
            "ok/ya.make": program("ok", ["ok.cpp"]),
            "ok/ok.cpp": "int main(){return 0;}\n",
        })
        result = ws.make("-j", "2", "-k", "-T", targets=("tool", "ok"), check=False)
        self.assertEqual(result.returncode, 1)
        stderr = plain(result.stderr)
        self.assertIn("clang: error: refusing a.cpp", stderr)
        self.assertIn("$(B)/lib/liblib.a broken by dep $(B)/lib/a.cpp.o", stderr)
        self.assertIn("$(B)/tool/tool broken by dep $(B)/lib/liblib.a", stderr)
        self.assertIn(("LD", "$(B)/ok/ok"), built_kinds(stderr))
        self.assertRegex(stderr, r"\nbuild failed: \d+\n$")
        self.assertFalse((ws.inst / "ok").exists())

    def test_keep_going_counts_failures_outside_the_roots(self):
        ws = Workspace(self, BASIC, extra_resources="DECLARE_EXTERNAL_RESOURCE(BROKEN {base}/broken.tar)\n")
        broken = f"{ws.server.base}/broken.tar"
        result = ws.make("-j", "2", "-k", check=False)
        self.assertEqual(result.returncode, 1)
        stderr = plain(result.stderr)
        self.assertIn(f"fetch: {broken} returned 404 Not Found", stderr)
        self.assertTrue(stderr.rstrip().endswith("build failed: 1 nodes failed"), stderr)

        result = ws.make("-j", "2", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"fetch: {broken} returned 404 Not Found", plain(result.stderr))

    def test_corrupt_cache_entries_are_evicted_and_rebuilt(self):
        ws = Workspace(self, BASIC)
        ws.make("-j", "2")
        clang_meta = ws.meta_for("$(B)/resources/CLANG20")
        clang_meta.write_text(json.dumps({"bogus": {"cas": "00"}}))
        ws.write("lib/a.cpp", "int a(){return 3;}\n")
        result = ws.make("-j", "2", "-k", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn(f'malformed meta entry "bogus" in {clang_meta}', plain(result.stderr))
        self.assertFalse(clang_meta.exists())

        python_meta = ws.meta_for("$(B)/resources/YMAKE_PYTHON3")
        python_meta.write_text("{broken json")
        result = ws.make("-j", "2", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "invalid character 'b' looking for beginning of object key string",
            plain(result.stderr),
        )
        self.assertFalse(python_meta.exists())

        ws.make("-j", "2")
        self.assertEqual(ws.server.requests[3:], ["/clang.tar", "/py.tar"])
        self.assertIn("main.cpp.o liblib.a", (ws.inst / "tool" / "tool").read_text())

    def test_invalid_cmd_prefix(self):
        ws = Workspace(self, BASIC)
        result = ws.make("-j", "1", "--cmd-prefix", "=x", check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn('make: --cmd-prefix expects <suffix>=<prefix>, got "=x"', result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
