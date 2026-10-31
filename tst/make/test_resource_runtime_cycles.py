from tst.make.lib import MakeTest


class ResourceRuntimeCyclesTest(MakeTest):
    def test_resource_runtime_cannot_depend_on_its_own_resource_generator(self):
        config = "library/cpp/resource/ya.make"
        original = (self.root / config).read_text()
        self.assertEqual(original.count("\nEND()"), 1)
        self.write_tree(config, original.replace("\nEND()", "\nRESOURCE(- E2E_SELF_RESOURCE=42)\nEND()"))
        self.write("ya.make", "LIBRARY()\nRESOURCE(- E2E_RESOURCE=42)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertRegex(result.stderr.lower(), r"peerdir.*own dir")
