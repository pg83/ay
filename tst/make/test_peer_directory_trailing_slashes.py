from tst.make.lib import MakeTest


class PeerDirectoryTrailingSlashesTest(MakeTest):
    def test_trailing_slashes_in_peer_directory_paths_are_rejected(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({self.project_name}/peer/)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("peer/ya.make", "LIBRARY(peer)\nSRCS(value.cpp)\nEND()\n")
        self.write("peer/value.cpp", "int answer() { return 42; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("trailing", result.stderr)
