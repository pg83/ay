from tst.make.lib import MakeTest, PROJECT


class BundleSourcesTest(MakeTest):
    def consumer(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/subject NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")

    def test_static_library_bundle_targets_are_configuration_errors(self):
        self.consumer()
        self.write("subject/ya.make", "LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("subject/answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_suffix_bundles_rename_the_selected_artifact(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/subject SUFFIX .extra NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("subject/ya.make", "PROGRAM(subject)\nSRCS(main.cpp)\nCOPY_FILE(extra.txt subject.extra)\nRESOURCE(subject.extra /extra)\nEND()\n")
        self.write("subject/main.cpp", "int main() { return 0; }\n")
        self.write("subject/extra.txt", "42\n")
        graph = self.graph()
        bundled = self.node(graph, "bundled")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)")
                for command in bundled["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[-2:], [f"$(B)/{PROJECT}/subject/subject.extra", f"$(B)/{PROJECT}/bundled"])
        self.assertIn(self.node(graph, "subject/subject")["uid"], bundled["deps"])

    def test_missing_bundle_modules_are_configuration_errors(self):
        self.consumer()
        self.assertNotEqual(self.make().returncode, 0)

    def test_bundle_modules_without_an_artifact_are_configuration_errors(self):
        self.consumer()
        self.write("subject/ya.make", "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(E2E_MARK sbr:123456789)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
