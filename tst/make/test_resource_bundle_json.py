import json

from tst.make.lib import MakeTest


class ResourceBundleJsonTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSET_RESOURCE_URI_FROM_JSON(E2E_RESOURCE resources.json)\nRUN_PYTHON3(generate.py ${E2E_RESOURCE} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("generate.py", "import sys\nprint(sys.argv[1:])\n")

    def test_resource_uri_includes_strip_prefix_arguments(self):
        for prefix in (0, 1, 65535):
            with self.subTest(strip_prefix=prefix):
                self.write("resources.json", json.dumps({"by_platform": {
                    "linux-x86_64": {"uri": "sbr:123456789", "strip_prefix": prefix},
                }}))
                node = self.node(self.graph(), "result.txt")
                args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertIn("sbr:123456789", args)
                if prefix:
                    self.assertIn("STRIP_PREFIX", args)
                    self.assertIn(str(prefix), args)
                else:
                    self.assertNotIn("STRIP_PREFIX", args)

    def test_strip_prefix_must_be_an_unsigned_sixteen_bit_integer(self):
        for prefix in (None, -1, 1.5, 65536, "1", True):
            with self.subTest(strip_prefix=prefix):
                self.write("resources.json", json.dumps({"by_platform": {
                    "linux-x86_64": {"uri": "sbr:123456789", "strip_prefix": prefix},
                }}))
                self.assertNotEqual(self.make().returncode, 0)

    def test_empty_and_unknown_platform_names_are_rejected(self):
        for platform in ("", "not-a-platform"):
            with self.subTest(platform=platform):
                self.write("resources.json", json.dumps({"by_platform": {
                    platform: {"uri": "sbr:123456789"},
                }}))
                self.assertNotEqual(self.make().returncode, 0)
