import json

from tst.make.lib import MakeTest, PROJECT


class ResourceDeclarationsTest(MakeTest):
    def package(self, declaration):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/resources)\nRUN_PYTHON3(generate.py ${{E2E_RESOURCE_RESOURCE_GLOBAL}} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        self.write("resources/ya.make", f"RESOURCES_LIBRARY()\n{declaration}\nEND()\n")

    def test_host_resource_bundles_accept_explicit_strip_prefixes(self):
        for prefix in ("+1", "2"):
            with self.subTest(prefix=prefix):
                self.package(f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(E2E_RESOURCE sbr:123456789 FOR LINUX STRIP_PREFIX {prefix})")
                args = [arg for command in self.node(self.graph(), "result.txt")["cmds"] for arg in command["cmd_args"]]
                self.assertTrue(any("E2E_RESOURCE" in arg and arg.startswith("$(") for arg in args), args)

    def test_host_resource_bundle_strip_prefixes_must_be_integral(self):
        for prefix in ("-1", "1.5", "bad", "4294967296"):
            with self.subTest(prefix=prefix):
                self.package(f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(E2E_RESOURCE sbr:123456789 FOR LINUX STRIP_PREFIX {prefix})")
                self.assertNotEqual(self.make().returncode, 0)

    def test_host_resource_bundles_require_complete_platform_entries(self):
        for tail in ("sbr:123456789", "sbr:123456789 FOR", "sbr:123456789 FOR LINUX dangling"):
            with self.subTest(tail=tail):
                self.package(f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(E2E_RESOURCE {tail})")
                self.assertNotEqual(self.make().returncode, 0)

    def test_json_resource_uris_follow_the_target_platform(self):
        self.write("ya.make", "LIBRARY()\nSET_RESOURCE_URI_FROM_JSON(E2E_RESOURCE resources.json)\nRUN_PYTHON3(generate.py ${E2E_RESOURCE} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        platforms = (("default-linux-aarch64", "linux-aarch64", "sbr:123456781"),
                     ("default-darwin-x86_64", "darwin", "sbr:123456782"),
                     ("default-darwin-arm64", "darwin-arm64", "sbr:123456783"),
                     ("default-win-x86_64", "win32", "sbr:123456784"))
        self.write("resources.json", json.dumps({"by_platform": {key: {"uri": uri} for _, key, uri in platforms}}))
        for platform, _, uri in platforms:
            with self.subTest(platform=platform):
                args = [arg for command in self.node(self.graph("--target-platform=" + platform), "result.txt")["cmds"] for arg in command["cmd_args"]]
                self.assertIn(uri, args)
