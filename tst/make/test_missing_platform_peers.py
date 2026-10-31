from tst.make.lib import MakeTest


class MissingPlatformPeersTest(MakeTest):
    def test_missing_linux_headers_are_a_configuration_error(self):
        config = "contrib/libs/linux-headers/ya.make"
        self.write_tree(config, (self.root / config).read_text())
        (self.root / config).unlink()
        self.write("ya.make", "RECURSE(first second)\n")
        for directory in ("first", "second"):
            self.write(directory + "/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
            self.write(directory + "/answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
