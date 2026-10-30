from tst.make.lib import MakeTest


class RecursiveLinkerFlagsTest(MakeTest):
    def test_recursive_linker_configuration_is_rejected(self):
        configuration = "build/ymake.core.conf"
        original = (self.root / configuration).read_text()
        self.write_tree(configuration, original + "\nwhen ($OS_LINUX == \"yes\") {\n    E2E_LINK_LEFT=$E2E_LINK_RIGHT\n    E2E_LINK_RIGHT=$E2E_LINK_LEFT\n    LDFLAGS=$E2E_LINK_LEFT\n}\n")
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.assertNotEqual(self.make().returncode, 0)
