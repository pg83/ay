from tst.make.lib import MakeTest


class LongTestNamesTest(MakeTest):
    project_name = "__ay_e2e__" + "x" * 70

    def package(self, directory=""):
        prefix = directory + "/" if directory else ""
        if directory:
            self.write("ya.make", f"RECURSE({directory})\n")
        self.write(prefix + "ya.make", "UNITTEST()\nSRCS(answer.cpp)\nEND()\n")
        self.write(prefix + "answer.cpp", '#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT(true); } }\n')

    def test_single_long_directory_preserves_its_test_binary_name(self):
        self.package()
        self.node(self.graph("-t"), self.project_name)

    def test_long_parent_paths_keep_the_short_test_directory_name(self):
        self.package("tests")
        self.node(self.graph("-t"), "tests/tests")

    def test_long_parent_and_test_directory_names_keep_the_last_component(self):
        name = "long_native_test_directory_name"
        self.package(name)
        self.node(self.graph("-t"), name + "/" + name)
