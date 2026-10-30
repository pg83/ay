from tst.make.lib import MakeTest, PROJECT


class ArchiveInputBoundariesTest(MakeTest):
    def test_archiver_rejects_a_resource_only_tool_module(self):
        self.write_tree("tools/archiver/ya.make", "RESOURCES_LIBRARY()\nEND()\n")
        self.write("ya.make", "LIBRARY()\nARCHIVE(NAME values.inc value.txt)\nRESOURCE(values.inc /data)\nEND()\n")
        self.write("value.txt", "42\n")
        self.assertNotEqual(self.make("--target-platform=default-linux-aarch64").returncode, 0)

    def test_repeated_archive_members_have_one_input_dependency(self):
        self.write("ya.make", "LIBRARY()\nARCHIVE(NAME values.inc value.txt value.txt)\nRESOURCE(values.inc /data)\nEND()\n")
        self.write("value.txt", "42\n")
        archived = self.node(self.graph(), "values.inc")
        self.assertEqual(archived["inputs"].count(f"$(S)/{PROJECT}/value.txt"), 1)

    def test_archiver_requires_a_runnable_tool_module(self):
        self.write_tree("tools/archiver/ya.make", "LIBRARY()\nSRCS(__ay_e2e_stub.cpp)\nEND()\n")
        self.write_tree("tools/archiver/__ay_e2e_stub.cpp", "int stub() { return 42; }\n")
        self.write("ya.make", "LIBRARY()\nARCHIVE(NAME values.inc value.txt)\nRESOURCE(values.inc /data)\nEND()\n")
        self.write("value.txt", "42\n")
        self.assertNotEqual(self.make("--target-platform=default-linux-aarch64").returncode, 0)
