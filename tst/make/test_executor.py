import http.server
import io
import json
import os
import shlex
import shutil
import sys
import subprocess
import time
import tarfile
import tempfile
import threading

from tst.backends import ay_only
from tst.make.lib import MakeTest, PROJECT


@ay_only("local executor, content cache and process failures")
class ExecutorTest(MakeTest):
    def setUp(self):
        super().setUp()
        archive = io.BytesIO()
        interpreter = f'#!/bin/sh\nexec {shlex.quote(sys.executable)} "$@"\n'.encode()
        with tarfile.open(fileobj=archive, mode="w") as output:
            entry = tarfile.TarInfo("bin/python3")
            entry.size = len(interpreter)
            entry.mode = 0o755
            output.addfile(entry, io.BytesIO(interpreter))
        runtime = archive.getvalue()
        self.payloads = {"/runtime.tar": runtime}
        payloads = self.payloads
        self.requests = []
        requests = self.requests

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                payload = payloads[self.path]
                self.send_response(200)
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

            def log_message(self, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.server_url = f"http://127.0.0.1:{server.server_port}"
        self.write_tree("build/platform/python/ymake_python3/ya.make", (
            "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\n"
            f"DECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 http://127.0.0.1:{server.server_port}/runtime.tar)\nEND()\n"
        ))
        self.cache = self.work / "cache"
        self.install = self.work / "install"

    def execute(self, *flags, sandboxing=True, ninja=True, env=None):
        return self.make("-j2", "--build-dir", str(self.cache), "--install", str(self.install),
                         *(["--ninja"] if ninja else []), *flags, dump_graph=False,
                         sandboxing=sandboxing, env=env)

    def copy_project(self):
        self.write("ya.make", "UNION(answer)\nCOPY_FILE(value.txt value.out)\nEXPLICIT_OUTPUTS(${BINDIR}/value.out)\nEND()\n")
        self.write("value.txt", "first\n")
        return self.install / PROJECT / "value.out"

    def test_cached_build_and_source_change(self):
        artifact = self.copy_project()
        first = self.execute("--stat")
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(artifact.read_text(), "first\n")
        self.assertTrue(artifact.is_symlink())
        self.assertIn("10 longest tasks:", first.stderr)
        self.assertEqual(self.requests, ["/runtime.tar"])
        manifests = set((self.cache / "uid").rglob("*"))
        second = self.execute("--stat")
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertNotIn("10 longest tasks:", second.stderr)
        self.assertEqual(set((self.cache / "uid").rglob("*")), manifests)
        self.assertEqual(self.requests, ["/runtime.tar"])
        self.write("value.txt", "second\n")
        changed = self.execute()
        self.assertEqual(changed.returncode, 0, changed.stderr)
        self.assertEqual(artifact.read_text(), "second\n")
        self.assertGreater(len(set((self.cache / "uid").rglob("*"))), len(manifests))
        self.assertEqual(self.requests, ["/runtime.tar"])

    def test_keep_going_records_failure_and_finishes_independent_commands(self):
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(fail.py OUT failed.dat)\nCOPY_FILE(value.txt value.out)\nEXPLICIT_OUTPUTS(${BINDIR}/failed.dat ${BINDIR}/value.out)\nEND()\n")
        self.write("fail.py", "import sys\nprint('executor fixture failure', file=sys.stderr)\nraise SystemExit(17)\n")
        self.write("value.txt", "survives\n")
        result = self.execute("--keep-going", "--stat")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("executor fixture failure", result.stderr)
        self.assertIn("exit status 17", result.stderr)
        self.assertIn("build failed:", result.stderr)
        outputs = {}
        for manifest in (self.cache / "uid").rglob("*"):
            if manifest.is_file():
                outputs.update(json.loads(manifest.read_text()))
        copied = outputs[f"$(B)/{PROJECT}/value.out"]["cas"]
        self.assertEqual((self.cache / "cas" / copied[:2] / copied).read_text(), "survives\n")
        self.assertNotIn(f"$(B)/{PROJECT}/failed.dat", outputs)

    def test_clear_rebuilds_without_source_sandbox(self):
        artifact = self.copy_project()
        for flags in ((), ("--clear",)):
            result = self.execute(*flags, sandboxing=False, ninja=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(artifact.read_text(), "first\n")
        self.assertEqual(self.requests, ["/runtime.tar", "/runtime.tar"])

    def test_native_executor_hashes_large_source_inputs(self):
        artifact = self.copy_project()
        payload = "x" * (2 * 1024 * 1024)
        for text in (payload, payload[:-1] + "y"):
            with self.subTest(last_byte=text[-1]):
                self.write("value.txt", text)
                result = self.execute(sandboxing=False, ninja=False)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue(artifact.read_bytes() == text.encode(), "large source contents differ")

    def test_native_executor_invalidates_generated_outputs_when_the_tool_changes(self):
        self.write("ya.make", f"UNION(answer)\nRUN_PROGRAM({PROJECT}/tool STDOUT result.txt)\nEXPLICIT_OUTPUTS(${{BINDIR}}/result.txt)\nEND()\n")
        self.write("tool/ya.make", f"YQL_PREBUILT_TOOL(generator)\nPRIMARY_OUTPUT(${{ARCADIA_ROOT}}/{PROJECT}/tool/generate.sh)\nEND()\n")
        artifact = self.install / PROJECT / "result.txt"
        for value in ("first", "second"):
            with self.subTest(value=value):
                self.write("tool/generate.sh", f"#!/bin/sh\nprintf '{value}\\n'\n")
                (self.project / "tool/generate.sh").chmod(0o755)
                result = self.execute(ninja=False)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(artifact.read_text(), value + "\n")

    def test_native_executor_fetches_a_stripped_host_runtime_bundle(self):
        archive = io.BytesIO()
        interpreter = f'#!/bin/sh\nexec {shlex.quote(sys.executable)} "$@"\n'.encode()
        with tarfile.open(fileobj=archive, mode="w") as output:
            entry = tarfile.TarInfo("runtime/bin/python3")
            entry.size = len(interpreter)
            entry.mode = 0o755
            output.addfile(entry, io.BytesIO(interpreter))
        self.payloads["/runtime.tar"] = archive.getvalue()
        self.write_tree("build/platform/python/ymake_python3/ya.make", (
            "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\n"
            f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(YMAKE_PYTHON3 {self.server_url}/runtime.tar FOR LINUX STRIP_PREFIX 1)\nEND()\n"
        ))
        artifact = self.copy_project()
        for attempt in range(2):
            with self.subTest(attempt=attempt):
                result = self.execute(ninja=False, env={"YA_TOKEN": ""})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(artifact.read_text(), "first\n")
                self.assertEqual(self.requests, ["/runtime.tar"])

    def test_native_executor_keeps_stderr_from_each_command_in_one_node(self):
        archive = io.BytesIO()
        interpreter = (f'#!/bin/sh\nprintf "E2E_EXECUTOR_DIAGNOSTIC\\n" >&2\n'
                       f'exec {shlex.quote(sys.executable)} "$@"\n').encode()
        with tarfile.open(fileobj=archive, mode="w") as output:
            entry = tarfile.TarInfo("bin/python3")
            entry.size = len(interpreter)
            entry.mode = 0o755
            output.addfile(entry, io.BytesIO(interpreter))
        self.payloads["/runtime.tar"] = archive.getvalue()
        self.write("ya.make", "UNION(answer)\nENABLE(USE_SDC_META)\nEND()\n")
        result = self.execute(ninja=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertGreaterEqual(result.stderr.count("E2E_EXECUTOR_DIAGNOSTIC"), 2)
        metadata = self.install / PROJECT / "answer.sdcmeta"
        self.assertEqual(json.loads(metadata.read_text()), {})

    def test_native_executors_share_a_cached_result_after_waiting_for_its_lock(self):
        if not os.path.isdir("/proc/self/fd"):
            self.skipTest("requires Linux process file descriptors")
        started = self.work / "started"
        release = self.work / "release"
        calls = self.work / "calls"
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(generate.py STDOUT result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("generate.py", (
            "import os, time\nfrom pathlib import Path\n"
            f"with Path({str(calls)!r}).open('a') as out: out.write('called\\n')\n"
            f"Path({str(started)!r}).write_text(os.getcwd())\n"
            f"while not Path({str(release)!r}).exists(): time.sleep(0.01)\n"
            "print('shared result')\n"
        ))
        args = [self.binary, "make", "-j2", "--build-dir", str(self.cache), "--install", str(self.install), PROJECT]
        processes = []
        try:
            first = subprocess.Popen(args, cwd=self.root, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            processes.append(first)
            deadline = time.monotonic() + 30
            while not started.exists() and first.poll() is None and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue(started.exists(), "first executor did not reach the generator")
            lock_directory = started.read_text()
            second = subprocess.Popen(args, cwd=self.root, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            processes.append(second)
            deadline = time.monotonic() + 30
            waiting = False
            while second.poll() is None and time.monotonic() < deadline:
                try:
                    for fd in os.scandir(f"/proc/{second.pid}/fd"):
                        try:
                            waiting = os.readlink(fd.path) == lock_directory
                        except FileNotFoundError:
                            continue
                        if waiting:
                            break
                except FileNotFoundError:
                    break
                if waiting:
                    break
                time.sleep(0.01)
            self.assertTrue(waiting, "second executor did not open the shared node lock")
            release.write_text("continue")
            for process in processes:
                _, stderr = process.communicate(timeout=30)
                self.assertEqual(process.returncode, 0, stderr)
            self.assertEqual(calls.read_text().splitlines(), ["called"])
            self.assertEqual((self.install / PROJECT / "result.txt").read_text(), "shared result\n")
        finally:
            release.write_text("continue")
            for process in processes:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.communicate(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.communicate()

    def mock_test_tool(self):
        calls = self.work / "test-tool-calls.jsonl"
        tool = self.work / "test-tool.py"
        tool.write_text(
            "import json, sys\nfrom pathlib import Path\n"
            "def unpack(words):\n"
            "    for word in words:\n"
            "        if word.startswith('@') and Path(word[1:]).is_file():\n"
            "            yield from unpack(Path(word[1:]).read_text().splitlines())\n"
            "        else:\n"
            "            yield word\n"
            "args = list(unpack(sys.argv[2:]))\n"
            f"with Path({str(calls)!r}).open('a') as output: output.write(json.dumps(args) + '\\n')\n"
            "if args[0] == 'download':\n"
            "    root = Path(args[args.index('--storage-root') + 1])\n"
            "    identifier = args[args.index('--id') + 1]\n"
            "    target = root / 'sandbox-storage' / identifier\n"
            "    target.mkdir(parents=True, exist_ok=True)\n"
            "    (target / 'resource_info.json').write_text(json.dumps({'id': identifier}))\n"
            "    (target / 'resource').write_text(identifier)\n"
            "for i, arg in enumerate(args[:-1]):\n"
            "    if arg in ('--meta', '--trace', '--trace-path', '--log-path', '--tar', '--dir-output-tared-path'):\n"
            "        output = Path(args[i + 1]); output.parent.mkdir(parents=True, exist_ok=True)\n"
            "        output.write_text('{}\\n')\n"
            "    elif arg == '--out-path':\n"
            "        Path(args[i + 1]).mkdir(parents=True, exist_ok=True)\n"
        )
        return calls, "test_tool=" + shlex.quote(sys.executable) + " " + shlex.quote(str(tool))

    def test_native_executor_revalidates_resources_after_validation_restart(self):
        self.write("value.txt", "local output\n")
        calls, prefix = self.mock_test_tool()
        python = self.work / "test-python.py"
        python.write_text("import os, sys\nos.execv(sys.executable, [sys.executable, *sys.argv[2:]])\n")
        python_prefix = "bin/python3=" + shlex.quote(sys.executable) + " " + shlex.quote(str(python))
        for index, restart in enumerate(("first", "second"), 1):
            with self.subTest(restart=restart):
                self.write("ya.make", f"UNION(answer)\nVALIDATE_DATA_RESTART({restart})\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO unused.txt)\nCOPY_FILE(value.txt value.out)\nEXPLICIT_OUTPUTS(${{BINDIR}}/value.out)\nEND()\n")
                result = self.execute("-t", "--cmd-prefix", prefix, "--cmd-prefix", python_prefix, ninja=False)
                self.assertEqual(result.returncode, 0, result.stderr)
                records = [json.loads(line) for line in calls.read_text().splitlines()]
                validation = [args for args in records if args[0] == "run_test"]
                self.assertEqual(len(validation), index)
                self.assertIn("123456789", validation[-1])
                self.assertEqual((self.install / PROJECT / "value.out").read_text(), "local output\n")
        self.assertEqual(self.requests, ["/runtime.tar"])

    def test_native_executor_caches_sandbox_data_downloads(self):
        self.write("ya.make", "EXECTEST()\nRUN(/bin/true)\nDATA(sbr://123456789)\nEND()\n")
        calls, prefix = self.mock_test_tool()
        python = self.work / "test-python.py"
        python.write_text("import os, sys\nos.execv(sys.executable, [sys.executable, *sys.argv[2:]])\n")
        python_prefix = "bin/python3=" + shlex.quote(sys.executable) + " " + shlex.quote(str(python))
        for _ in range(2):
            result = self.execute("-t", "--cmd-prefix", prefix, "--cmd-prefix", python_prefix, ninja=False)
            self.assertEqual(result.returncode, 0, result.stderr)
        records = [json.loads(line) for line in calls.read_text().splitlines()]
        downloads = [args for args in records if args[0] == "download"]
        self.assertEqual(len(downloads), 1)
        self.assertIn("123456789", downloads[0])
        outputs = {}
        for manifest in (self.cache / "uid").rglob("*"):
            if manifest.is_file():
                outputs.update(json.loads(manifest.read_text()))
        cached = outputs["$(B)/sandbox-storage/123456789/resource"]["cas"]
        self.assertEqual((self.cache / "cas" / cached[:2] / cached).read_text(), "123456789")

    def test_malformed_cached_manifest_is_invalidated_and_rebuilt(self):
        artifact = self.copy_project()
        result = self.execute()
        self.assertEqual(result.returncode, 0, result.stderr)
        for manifest in (self.cache / "uid").rglob("*"):
            if manifest.is_file():
                outputs = json.loads(manifest.read_text())
                if f"$(B)/{PROJECT}/value.out" in outputs and len(outputs) > 1:
                    manifest.write_text(json.dumps({"invalid-path": next(iter(outputs.values()))}))
                    break
        else:
            self.fail("root cache manifest was not written")
        broken = self.execute()
        self.assertNotEqual(broken.returncode, 0)
        self.assertIn('malformed meta entry "invalid-path"', broken.stderr)
        self.assertFalse(manifest.exists())
        recovered = self.execute()
        self.assertEqual(recovered.returncode, 0, recovered.stderr)
        self.assertEqual(artifact.read_text(), "first\n")

    def test_invalid_cache_shard_and_stale_work_directory_are_ignored(self):
        artifact = self.copy_project()
        shard = self.cache / "uid/not-a-directory"
        shard.parent.mkdir(parents=True)
        shard.write_text("stale cache entry")
        first = self.execute()
        self.assertEqual(first.returncode, 0, first.stderr)
        for manifest in (self.cache / "uid").rglob("*"):
            if manifest.is_file() and manifest != shard:
                outputs = json.loads(manifest.read_text())
                if f"$(B)/{PROJECT}/value.out" in outputs and len(outputs) > 1:
                    break
        else:
            self.fail("root cache manifest was not written")
        temporary = self.cache / "tmp" / manifest.name
        temporary.mkdir(parents=True)
        (temporary / "abandoned").write_text("previous interrupted command")
        manifest.unlink()
        second = self.execute()
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(artifact.read_text(), "first\n")
        self.assertTrue(manifest.is_file())
        self.assertFalse(temporary.exists())

    def test_failed_temporary_directory_listing_does_not_abort_build(self):
        artifact = self.copy_project()
        result = self.execute(env={"AY_CHAOS": "readdir-fail=0"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(artifact.read_text(), "first\n")

    def test_terminal_output_includes_command_stderr_and_failures(self):
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(command.py ${BINDIR}/result.txt OUT_NOAUTO result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("command.py", "from pathlib import Path\nimport sys\nprint('command succeeded', file=sys.stderr)\nPath(sys.argv[1]).write_text('42')\n")
        success = self.execute(ninja=False)
        self.assertEqual(success.returncode, 0, success.stderr)
        self.assertIn("command succeeded", success.stderr)
        self.assertEqual((self.install / PROJECT / "result.txt").read_text(), "42")
        self.write("command.py", "import sys\nprint('command failed', file=sys.stderr)\nraise SystemExit(23)\n")
        failure = self.execute("--keep-going", ninja=False)
        self.assertNotEqual(failure.returncode, 0)
        self.assertIn("command failed", failure.stderr)
        self.assertIn("exit status 23", failure.stderr)

    def test_statistics_limit_longest_tasks_to_ten(self):
        commands = []
        outputs = []
        for index in range(12):
            self.write(f"input{index}.txt", f"value {index}\n")
            commands.append(f"COPY_FILE(input{index}.txt output{index}.txt)")
            outputs.append(f"${{BINDIR}}/output{index}.txt")
        self.write("ya.make", "UNION(answer)\n" + "\n".join(commands) + "\nEXPLICIT_OUTPUTS(" + " ".join(outputs) + ")\nEND()\n")
        result = self.execute("--stat")
        self.assertEqual(result.returncode, 0, result.stderr)
        longest = result.stderr.split("10 longest tasks:\n", 1)[1].split("\n\n", 1)[0]
        self.assertEqual(len(longest.splitlines()), 10)
        for index in range(12):
            self.assertEqual((self.install / PROJECT / f"output{index}.txt").read_text(), f"value {index}\n")

    def test_directory_and_symlink_outputs_survive_cache_restoration(self):
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(produce.py ${BINDIR}/tree ${BINDIR}/link OUT_NOAUTO tree link)\nEXPLICIT_OUTPUTS(${BINDIR}/tree ${BINDIR}/link)\nEND()\n")
        self.write("produce.py", (
            "from pathlib import Path\nimport os, sys\n"
            "tree = Path(sys.argv[1]); tree.mkdir(parents=True)\n"
            "(tree / 'data').write_text('nested output')\n"
            "os.symlink('tree/data', sys.argv[2])\n"
        ))
        for _ in range(2):
            result = self.execute()
            self.assertEqual(result.returncode, 0, result.stderr)
            link = self.install / PROJECT / "link"
            self.assertEqual(os.readlink(link), "tree/data")
            self.assertEqual(link.read_text(), "nested output")
            link.unlink()

    def test_command_prefix_nested_response_files_and_stderr(self):
        self.write("ya.make", (
            "UNION(answer)\nRUN_PYTHON3(consume.py ${BINDIR}/result.json "
            "--ya-start-command-file outer --ya-start-command-file inner --ya-end-command-file "
            "tail --ya-end-command-file OUT_NOAUTO result.json)\n"
            "EXPLICIT_OUTPUTS(${BINDIR}/result.json)\nEND()\n"
        ))
        self.write("consume.py", (
            "from pathlib import Path\nimport json, sys\n"
            "args = Path(sys.argv[2][1:]).read_text().splitlines()\n"
            "args[1] = Path(args[1][1:]).read_text().splitlines()\n"
            "Path(sys.argv[1]).write_text(json.dumps(args))\n"
            "print('executor stderr retained', file=sys.stderr)\n"
        ))
        wrapper = self.work / "prefix.py"
        wrapper.write_text("import subprocess, sys\nraise SystemExit(subprocess.call(sys.argv[1:]))\n")
        result = self.execute("--cmd-prefix", "bin/python3=" + sys.executable + " " + str(wrapper))
        self.assertEqual(result.returncode, 0, result.stderr)
        output = self.install / PROJECT / "result.json"
        self.assertEqual(json.loads(output.read_text()), ["outer", ["inner"], "tail"])
        self.assertIn("executor stderr retained", result.stderr)

    def test_stdout_redirection_and_command_working_directory(self):
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(emit.py CWD ${BINDIR} STDOUT_NOAUTO result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("emit.py", "from pathlib import Path\nprint(Path.cwd().name)\n")
        result = self.execute("--clear")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.install / PROJECT / "result.txt").read_text(), PROJECT + "\n")

    def test_command_working_directory_can_be_the_source_root(self):
        self.write("read.py", f"from pathlib import Path\nprint(Path('{PROJECT}/value.txt').read_text().strip())\n")
        self.write("value.txt", "source root\n")
        # The command reads this file dynamically, so declare it explicitly.
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(read.py IN value.txt CWD ${ARCADIA_ROOT} STDOUT_NOAUTO result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        result = self.execute()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.install / PROJECT / "result.txt").read_text(), "source root\n")

    def test_cache_restoration_across_filesystems_uses_symlinks(self):
        if not os.path.isdir("/dev/shm"):
            self.skipTest("requires a second filesystem for the content cache")
        external = tempfile.TemporaryDirectory(prefix="ay-e2e-cas-", dir="/dev/shm")
        self.addCleanup(external.cleanup)
        destination = os.path.join(external.name, "cas")
        artifact = self.copy_project()
        first = self.execute()
        self.assertEqual(first.returncode, 0, first.stderr)
        if os.stat(external.name).st_dev == self.cache.stat().st_dev:
            self.skipTest("the temporary content cache must be on a different filesystem")
        shutil.move(str(self.cache / "cas"), destination)
        (self.cache / "cas").symlink_to(destination)
        for manifest in (self.cache / "uid").rglob("*"):
            if manifest.is_file():
                outputs = json.loads(manifest.read_text())
                if f"$(B)/{PROJECT}/value.out" in outputs and len(outputs) > 1:
                    manifest.unlink()
                    break
        else:
            self.fail("root cache manifest was not written")
        artifact.unlink()
        second = self.execute()
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertTrue(artifact.is_symlink())
        self.assertEqual(artifact.read_text(), "first\n")

    def test_unterminated_response_file_consumes_remaining_arguments(self):
        self.write("ya.make", "UNION(answer)\nRUN_PYTHON3(consume.py ${BINDIR}/result.txt --ya-start-command-file first second OUT_NOAUTO result.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/result.txt)\nEND()\n")
        self.write("consume.py", "from pathlib import Path\nimport sys\nPath(sys.argv[1]).write_text(Path(sys.argv[2][1:]).read_text())\n")
        result = self.execute()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.install / PROJECT / "result.txt").read_text(), "first\nsecond\n")

    def test_invalid_command_prefix_is_rejected(self):
        self.copy_project()
        for value, message in (("", "requires an argument"), ("=prefix", "--cmd-prefix expects"), ("missing-equals", "--cmd-prefix expects")):
            with self.subTest(value=value):
                result = self.execute("--cmd-prefix", value)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
        self.assertEqual(self.requests, [])

    def test_cache_link_failure_stops_the_build(self):
        self.copy_project()
        result = self.execute(env={"AY_CHAOS": "link-exdev=0"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("link", result.stderr)
        self.assertFalse((self.install / PROJECT / "value.out").exists())

    def test_sandbox_commands_use_native_fetch_and_resolve_token(self):
        self.payloads.update({"/first.txt": b"first resource\n", "/second.txt": b"second resource\n"})
        self.write_tree("build/mapping.conf.json", json.dumps({"resources": {
            "123456789": self.server_url + "/first.txt",
            "123456790": self.server_url + "/second.txt",
        }}))
        profile = self.work / "profile"
        profile.mkdir()
        (profile / ".ya_token").write_text("fixture-token\n")
        self.write("ya.make", "UNION(answer)\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO first.txt)\nFROM_SANDBOX(FILE 123456790 OUT_NOAUTO second.txt)\nEXPLICIT_OUTPUTS(${BINDIR}/first.txt ${BINDIR}/second.txt)\nEND()\n")
        result = self.execute(env={"HOME": str(profile), "YA_TOKEN": "", "SSH_AUTH_SOCK": ""})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.install / PROJECT / "first.txt").read_text(), "first resource\n")
        self.assertEqual((self.install / PROJECT / "second.txt").read_text(), "second resource\n")
        self.assertIn("/first.txt", self.requests)
        self.assertIn("/second.txt", self.requests)

    def test_sandbox_tool_resource_uses_local_mapping_and_token(self):
        self.write_tree("build/platform/python/ymake_python3/ya.make", "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:123456788)\nEND()\n")
        self.write_tree("build/mapping.conf.json", json.dumps({"resources": {
            "123456788": self.server_url + "/runtime.tar",
        }}))
        profile = self.work / "profile"
        profile.mkdir()
        (profile / ".ya_token").write_text("fixture-token\n")
        artifact = self.copy_project()
        first = self.execute(env={"HOME": str(profile), "YA_TOKEN": "", "SSH_AUTH_SOCK": ""})
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(artifact.read_text(), "first\n")
        second = self.execute(env={"HOME": str(profile), "YA_TOKEN": "", "SSH_AUTH_SOCK": ""})
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(self.requests, ["/runtime.tar"])

    def test_garbage_collection_repairs_readonly_directories(self):
        self.copy_project()
        locked = self.cache / "grb/locked"
        locked.mkdir(parents=True)
        garbage = locked / "old-output"
        garbage.write_text("discarded")
        locked.chmod(0o500)
        self.addCleanup(lambda: locked.chmod(0o700) if locked.exists() else None)
        commands = self.work / "commands"
        commands.mkdir()
        rm = commands / "rm"
        rm.write_text("#!/bin/sh\nexit 1\n")
        rm.chmod(0o755)
        env = {"PATH": str(commands) + os.pathsep + os.environ["PATH"]}
        result = self.execute(env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(garbage.exists())

    def test_garbage_collection_falls_back_when_rm_fails(self):
        artifact = self.copy_project()
        garbage = self.cache / "grb/abandoned"
        garbage.parent.mkdir(parents=True)
        garbage.write_text("discarded")
        commands = self.work / "commands"
        commands.mkdir()
        rm = commands / "rm"
        rm.write_text("#!/bin/sh\nexit 1\n")
        rm.chmod(0o755)
        result = self.execute(env={"PATH": str(commands) + os.pathsep + os.environ["PATH"]})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(artifact.read_text(), "first\n")
        self.assertFalse(garbage.exists())
