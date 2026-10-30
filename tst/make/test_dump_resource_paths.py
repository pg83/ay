import json
import subprocess

from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("ay dump commands and their path normalization")
class DumpResourcePathsTest(MakeTest):
    def invoke(self, *args):
        return subprocess.run([self.binary, "dev", "dump", *args], cwd=self.root, capture_output=True, text=True, timeout=30)

    def test_exact_output_matching_normalizes_resource_mounts(self):
        paths = (
            ("$(BUILD_ROOT)/resources/CLANG20/bin/clang++", "$(CLANG)/bin/clang++"),
            ("$(B)/resources/YMAKE_PYTHON3/bin/python3", "$(YMAKE_PYTHON3)/bin/python3"),
            ("$(CLANG-sbr:123456789)/bin/clang++", "$(CLANG)/bin/clang++"),
            ("$(JDK17-123456789)/bin/java", "$(JDK17)/bin/java"),
        )
        source = self.work / "nodes.jsonl"
        for index, (output, query) in enumerate(paths):
            with self.subTest(output=output):
                node = {"self_uid": str(index), "outputs": [output]}
                source.write_text(json.dumps(node) + "\n")
                result = self.invoke("grep", "--in", str(source), query)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout), node)

    def test_value_flags_require_a_following_argument(self):
        for command, flag in (("grep", "--in"), ("diff", "--left"), ("diff", "--right"), ("diff", "--out"), ("diff", "--group"), ("diff", "--pair")):
            with self.subTest(command=command, flag=flag):
                result = self.invoke(command, flag)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(f'missing value for flag "{flag}"', result.stderr)
