from tst.make.lib import MakeTest, PROJECT


class ProtoImportOptionsTest(MakeTest):
    def native_schema(self, imports, extra_sources=""):
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(answer.proto {extra_sources})\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2";\n' + imports + '\nmessage Answer {}\n')
        return self.node(self.graph(), "answer.pb.cc")

    def test_public_and_weak_imports_keep_source_dependencies(self):
        for name in ("public", "weak"):
            self.write(name + ".proto", 'syntax = "proto2";\nmessage ' + name.title() + ' {}\n')
        generated = self.native_schema('import public "public.proto";\nimport weak "weak.proto";\n', "public.proto weak.proto")
        for name in ("public", "weak"):
            self.assertIn(f"$(S)/{PROJECT}/{name}.proto", generated["inputs"])

    def test_import_keyword_suffixes_still_reject_missing_imports(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(answer.proto)\nEND()\n")
        for keyword in ("import publicly", "import weakly"):
            with self.subTest(keyword=keyword):
                self.write("answer.proto", f'syntax = "proto2";\n{keyword} "missing.proto";\nmessage Answer {{}}\n')
                self.assertNotEqual(self.make().returncode, 0)

    def test_other_identifiers_starting_with_import_are_not_directives(self):
        self.native_schema('imported "missing.proto";\n')

    def test_comment_markers_and_unclosed_imports_do_not_create_dependencies(self):
        self.native_schema('import "directory//missing.proto";\nimport "unfinished\n')

    def test_imported_schema_extensions_select_native_header_names(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCOPY_FILE(header.txt opaque.pb.h)\nSRCS(answer.proto event.ev config.cfgproto gazette.gztproto)\nEND()\n")
        self.write("answer.proto", '''syntax = "proto2";
import "event.ev";
import "config.cfgproto";
import "gazette.gztproto";
import "opaque.txt";
message Answer {}
''')
        for name in ("event.ev", "config.cfgproto", "gazette.gztproto"):
            self.write(name, 'syntax = "proto2";\nmessage Dependency {}\n')
        self.write("opaque.txt", "\n")
        self.write("header.txt", "#pragma once\n")
        graph = self.graph()
        generated = self.node(graph, "answer.pb.cc")
        for name in ("event.ev", "config.cfgproto", "gazette.gztproto", "opaque.txt"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", generated["inputs"])
        compiled = self.node(graph, "answer.pb.cc.o")
        for name in ("event.ev.pb.h", "config.cfgproto.pb.h", "gazette.pb.h"):
            self.assertIn(f"$(B)/{PROJECT}/{name}", compiled["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/opaque.pb.h", compiled["inputs"])
