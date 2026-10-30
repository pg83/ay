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

    def test_missing_and_null_resource_uris_are_rejected(self):
        for entry in ({}, {"uri": None}, {"uri": 42}):
            with self.subTest(entry=entry):
                self.write("resources.json", json.dumps({"by_platform": {"linux-x86_64": entry}}))
                self.assertNotEqual(self.make().returncode, 0)

    def test_empty_platform_mappings_are_rejected(self):
        self.write("resources.json", json.dumps({"by_platform": {}}))
        self.assertNotEqual(self.make().returncode, 0)

    def test_missing_target_platform_leaves_the_resource_uri_empty(self):
        self.write("resources.json", json.dumps({"by_platform": {"darwin-arm64": {"uri": "sbr:123456789"}}}))
        generated = self.node(self.graph(), "result.txt")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertFalse(any("sbr:123456789" in arg for arg in args), args)

    def test_resource_mappings_can_grow_past_the_initial_bucket_count(self):
        platforms = ("linux", "linux-x86_64", "linux-aarch64", "darwin", "darwin-x86_64",
                     "darwin-arm64", "win32", "win32-x86_64", "any")
        self.write("resources.json", json.dumps({"by_platform": {
            platform: {"uri": "sbr:123456789"} for platform in platforms
        }}))
        generated = self.node(self.graph(), "result.txt")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("sbr:123456789", args)

    def test_repeated_platform_keys_use_the_last_resource(self):
        self.write("resources.json", '{"by_platform":{"linux-x86_64":{"uri":"sbr:111"},'
                   '"linux-x86_64":{"uri":"sbr:222"}}}\n')
        generated = self.node(self.graph(), "result.txt")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("sbr:222", args)
        self.assertNotIn("sbr:111", args)

    def test_platform_mappings_must_be_non_null_objects(self):
        for value in (None, [], 42, "linux"):
            with self.subTest(value=value):
                self.write("resources.json", json.dumps({"by_platform": value}))
                self.assertNotEqual(self.make().returncode, 0)
