import base64
import json
import os
import subprocess
import sys
import unittest

import lib
from test_link_test_nodes import library
from test_ymake_conf import Tree


AGENT_ENV = (
    "OPENCODE", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_TRACE_ID", "CODEIUM_EDITOR_APP_ROOT",
    "AIDER_API_KEY", "AMP_CURRENT_THREAD_ID", "ANTIGRAVITY_AGENT", "AUGMENT_AGENT", "GEMINI_CLI",
    "QWEN_CODE", "REPL_ID",
)


def test_args(graph, suite="exectest", chunk=""):
    run = lib.node_by_output(graph, f"$(B)/app/test-results/{suite}/{chunk}meta.json")
    return run, run["cmds"][-1]["cmd_args"]


class TestRunnerContractsTest(unittest.TestCase):
    def test_exec_split_subtests_sanitizer_and_explicit_data(self):
        files = {
            "app/ya.make": "EXECTEST()\nRUN(check)\nEXPLICIT_DATA()\n"
                "DATA(AUTOUPDATED ignored arcadia/fixtures/a)\n"
                "DATA_FILES(fixtures/b ${ARCADIA_ROOT}/fixtures/c)\n"
                "FORK_SUBTESTS(MODULO)\nSPLIT_FACTOR(2)\nEND()\n",
            "fixtures/a": "a", "fixtures/b": "b", "fixtures/c": "c",
        }
        graph = lib.make(files, "app", "-t", "--dist", "-DSANITIZER_TYPE=address")
        for index in range(2):
            run, args = test_args(graph, chunk=f"chunk{index}/")
            self.assertIn("--split-by-tests", args)
            self.assertEqual(args[args.index("--modulo") + 1], "2")
            self.assertEqual(args[args.index("--modulo-index") + 1], str(index))
            self.assertEqual(args[args.index("--partition-mode") + 1], "MODULO")
            self.assertEqual(args[args.index("--sanitize") + 1], "address")
            self.assertNotIn("--collect-cores", args)
            related = [args[i + 1] for i, arg in enumerate(args) if arg == "--test-related-path"]
            self.assertEqual(related, ["$(S)/fixtures/a", "$(S)/fixtures/b", "$(S)/fixtures/c"])
            self.assertNotIn("$(S)/ignored", run["inputs"])

    def test_benchmark_and_fuzz_options_reach_their_runners(self):
        for macro, suite, runner, options, peers in (
            ("G_BENCHMARK", "app", "run_g_benchmark", "BENCHMARK_OPTS(--benchmark_min_time=1 --benchmark_repetitions=2)",
             ("library/cpp/testing/gbenchmark",)),
            ("FUZZ", "fuzz", "run_fuzz", "FUZZ_OPTS(-max_len=64 -runs=10)",
             ("library/cpp/testing/nofuzz", "contrib/libs/libfuzzer")),
        ):
            with self.subTest(macro=macro):
                files = {"app/ya.make": f"{macro}()\n{lib.BARE_MODULE}{options}\nSRCS(main.cpp)\nEND()\n",
                         "app/main.cpp": "int main() { return 0; }\n"}
                files.update({f"{peer}/ya.make": library() for peer in peers})
                _, args = test_args(lib.make(files, "app", "-t", "--dist"), suite=suite)
                self.assertIn(runner, args)
                if macro == "G_BENCHMARK":
                    self.assertIn("--truncate-logs", args)
                    self.assertIn("--test-binary-args=--benchmark_min_time=1", args)
                    self.assertIn("--test-binary-args=--benchmark_repetitions=2", args)
                else:
                    self.assertIn("--fuzz-opts=-max_len=64 -runs=10", args)

    def test_exec_commands_escape_quotes_and_backslashes(self):
        graph = lib.make({"app/ya.make": r'''EXECTEST()
RUN(check "path with spaces" 'say"hello' 'C:\tmp\file')
END()
'''}, "app", "-t")
        _, args = test_args(graph)
        encoded = next(a.removeprefix("commands=") for a in args if a.startswith("commands="))
        self.assertEqual(base64.b64decode(encoded).decode(), '\ncheck "path with spaces" say\\"hello C:\\tmp\\file')

    def test_output_style_and_directory_options_follow_environment(self):
        files = {"app/ya.make": "EXECTEST()\nRUN(check)\nEND()\n",
                 "ya.conf": "auto_detect_agent = false\ndir_outputs = false\n"}
        for signature in ("", "1"):
            with self.subTest(signature=signature):
                _, args = test_args(lib.make(files, "app", "-t", env={
                    "YA_AUTO_DETECT_AGENT": "yes", "CODEX_THREAD_ID": signature, "YA_DIR_OUTPUTS": "no",
                }))
                self.assertEqual(args[args.index("--output-style") + 1], "jsonl")

    @unittest.skipUnless(sys.platform.startswith("linux"), "process names are read from Linux procfs")
    def test_parent_process_name_selects_jsonl_without_environment_signature(self):
        tree = Tree(self, {"app/ya.make": "EXECTEST()\nRUN(check)\nEND()\n"},
                    ya_conf="auto_detect_agent = true\n")
        env = {k: v for k, v in os.environ.items() if k not in (*AGENT_ENV, *lib.TOOLCHAIN_ENV_VARS)}
        env.pop("YA_AUTO_DETECT_AGENT", None)
        # The Python launcher supplies a real parent process name; ay reads /proc itself.
        launcher = "import pathlib, subprocess, sys; pathlib.Path('/proc/self/comm').write_text('codex'); sys.exit(subprocess.call(sys.argv[1:]))"
        result = subprocess.run([sys.executable, "-c", launcher, str(lib.AY), "make", "-j0", "-G", "-t",
                                 "--source-root", str(tree.src), "--target-platform", "default-linux-aarch64",
                                 "--host-platform", "default-linux-x86_64", "app"],
                                env=env, capture_output=True, text=True, timeout=60)
        self.assertEqual(result.returncode, 0, result.stderr)
        _, args = test_args(json.loads(result.stdout))
        self.assertEqual(args[args.index("--output-style") + 1], "jsonl")


if __name__ == "__main__":
    unittest.main(verbosity=2)
