import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class CodegenFormatsTest(MakeTest):
    def test_decimal_md5(self):
        self.write("ya.make", "LIBRARY()\nDECIMAL_MD5_LOWER_32_BITS(hash.cpp FUNCNAME e2e_hash input.txt)\nEND()\n")
        self.write("input.txt", "42\n")
        self.node(self.graph(), "hash.cpp")

    def test_from_archive(self):
        self.write("ya.make", "LIBRARY()\nFROM_ARCHIVE(sources.tar OUT answer.cpp)\nEND()\n")
        value = b"int answer() { return 42; }\n"
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            entry = tarfile.TarInfo("answer.cpp")
            entry.size = len(value)
            archive.addfile(entry, io.BytesIO(value))
        graph = self.graph()
        self.node(graph, "answer.cpp")
        self.node(graph, "answer.cpp.o")

    def test_cfgproto(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSRCS(answer.cfgproto)\nEND()\n")
        self.write("answer.cfgproto", 'syntax = "proto2";\npackage e2e;\noption (NProtoConfig.Include) = "value.h";\nmessage Answer { optional int32 value = 1; }\n')
        self.write("value.h", "#pragma once\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.cfgproto" in node["inputs"] and any(o.endswith(".cc") for o in node["outputs"]) for node in graph["graph"]))

    def test_flatbuffers_comments(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.fbs value.fbs)\nEND()\n")
        self.write("answer.fbs", '// line comment\n/* multiline\nordinary comment text\n*/\ninclude "value.fbs";\nnamespace e2e;\ntable Answer { value: Value; text: string = "text with \\"quoted\\" // data"; }\nroot_type Answer;\n')
        self.write("value.fbs", "namespace e2e; table Value { number: int; }\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.fbs" in node["inputs"] and f"$(S)/{PROJECT}/value.fbs" in node["inputs"] for node in graph["graph"]))

    def test_event_proto(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.ev)\nEND()\n")
        self.write("answer.ev", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        self.node(self.graph(), "answer.ev.pb.cc")

    def test_html_template(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.asp)\nEND()\n")
        self.write("answer.asp", "<html><body>42</body></html>\n")
        self.node(self.graph(), "answer.asp.cpp")

    def test_formula(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.fml)\nRESOURCE(answer.fml.inc /formula)\nEND()\n")
        self.write("answer.fml", "42\n")
        self.node(self.graph(), "answer.fml.inc")

    def test_static_formula(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.inc.sfdl)\nRESOURCE(answer.inc /formula)\nEND()\n")
        self.write("answer.inc.sfdl", "42\n")
        self.node(self.graph(), "answer.inc")

    def test_gettext(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.po)\nRESOURCE(answer.mo /translations)\nEND()\n")
        self.write("answer.po", 'msgid "answer"\nmsgstr "42"\n')
        self.node(self.graph(), "answer.mo")

    def test_domain_schema(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(FOR sc {PROJECT})\nSRCS(answer.sc)\nRESOURCE(answer.sc.h /schema)\nEND()\n")
        self.write("answer.sc", 'include "base.sc";\n')
        self.write("base.sc", "\n")
        self.node(self.graph(), "answer.sc.h")

    def test_proto_descriptions(self):
        self.write("ya.make", f"PROTO_DESCRIPTIONS(answer)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.node(graph, "answer.protodesc")
        self.node(graph, "answer.tar")

    def test_proto_registry(self):
        self.write("ya.make", f"PROTO_REGISTRY(answer)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        self.node(self.graph(), "answer.protodesc")

    def test_list_proto(self):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/proto)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", f'#include "{PROJECT}/proto/files.pb.h"\nint answer() {{ return 42; }}\n')
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nLIST_PROTO(TO files.proto answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.node(graph, "proto/files.proto")
        self.node(graph, "proto/files.pb.h")

    def test_import_library(self):
        self.write("ya.make", "LIBRARY()\nGENERATE_IMPLIB(answer /usr/lib/libanswer.so SONAME libanswer.so DLOPEN_CALLBACK load_answer)\nEND()\n")
        graph = self.graph()
        self.node(graph, "libanswer.so.init.c")
        self.node(graph, "libanswer.so.tramp.S")
