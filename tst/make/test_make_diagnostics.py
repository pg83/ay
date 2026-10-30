from tst.make.lib import MakeTest


class MakeDiagnosticsTest(MakeTest):
    def native(self, macro, declaration="LIBRARY()"):
        self.write("ya.make", f"{declaration}\nSRCS(answer.cpp)\n{macro}\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_all_resource_files_requires_an_extension(self):
        self.native("ALL_RESOURCE_FILES()")
        self.assertNotEqual(self.make().returncode, 0)

    def test_unused_empty_archive_names_do_not_block_compilation(self):
        self.write("value.txt", "42\n")
        for macro in ("ARCHIVE", "ARCHIVE_BY_KEYS"):
            with self.subTest(macro=macro):
                self.native(f'{macro}(NAME "" value.txt)')
                self.node(self.graph(), "answer.cpp.o")

    def test_apphost_rejects_unknown_options(self):
        self.write("ya.make", "PROTO_LIBRARY()\nAPPHOST(E2E_UNKNOWN_OPTION)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2"; message Answer { optional int32 value = 1; }\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_build_mn_requires_both_info_and_name(self):
        self.native("BUILD_MN(only_info)")
        self.assertNotEqual(self.make().returncode, 0)

    def test_library_linker_script_does_not_block_compilation(self):
        self.write("answer.ld", "SECTIONS {}\n")
        self.native("LINKER_SCRIPT(answer.ld)")
        self.node(self.graph(), "answer.cpp.o")

    def test_linker_script_rejects_an_unsupported_extension(self):
        self.write("answer.txt", "SECTIONS {}\n")
        self.native("LINKER_SCRIPT(answer.txt)", "PROGRAM(answer)")
        self.assertNotEqual(self.make().returncode, 0)

    def test_else_block_cannot_be_followed_by_else_if(self):
        self.write("ya.make", "LIBRARY()\nIF(no)\nSRCS(answer.cpp)\nELSE()\nSRCS(other.cpp)\nELSEIF(yes)\nENDIF()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("other.cpp", "int other() { return 17; }\n")
        self.assertNotEqual(self.make().returncode, 0)
