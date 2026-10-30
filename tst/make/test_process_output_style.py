import os
from pathlib import Path
from unittest.mock import patch

from tst.make.lib import MakeTest


class ProcessOutputStyleTest(MakeTest):
    def unmarked_environment(self):
        markers = ("OPENCODE", "CLAUDECODE", "CODEX_THREAD_ID", "CURSOR_TRACE_ID", "CODEIUM_EDITOR_APP_ROOT",
                   "AIDER_API_KEY", "AMP_CURRENT_THREAD_ID", "ANTIGRAVITY_AGENT", "AUGMENT_AGENT", "GEMINI_CLI",
                   "QWEN_CODE", "REPL_ID")
        return {key: value for key, value in os.environ.items() if key not in markers}

    def test_agent_process_name_selects_json_output_without_environment_markers(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        environment = self.unmarked_environment()
        environment["YA_AUTO_DETECT_AGENT"] = "yes"
        process_name = Path("/proc/self/comm")
        original_name = process_name.read_text()
        try:
            process_name.write_text("codex")
            with patch.dict(os.environ, environment, clear=True):
                node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        finally:
            process_name.write_text(original_name.rstrip("\n"))
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--output-style") + 1], "jsonl")

    def test_unreadable_process_metadata_keeps_human_output(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        environment = self.unmarked_environment()
        environment.update(YA_AUTO_DETECT_AGENT="yes", AY_CHAOS="readfile-eio=0")
        with patch.dict(os.environ, environment, clear=True):
            node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--output-style") + 1], "ninja")
