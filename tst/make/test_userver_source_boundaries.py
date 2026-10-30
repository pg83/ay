import io
import json
import tarfile

from tst.make.lib import MakeTest, PROJECT


class UserverSourceBoundariesTest(MakeTest):
    def test_archive_keywords_require_values_and_a_source_archive(self):
        for macro in ("USRV_BUILD(answer.cpp)", "USRV_BUILD(answer.cpp FROM)",
                      "USRV_BUILD(answer.cpp FROM sources.tar DEPS_FILE)", "USRV_ALL_SRCS(SUFFIX)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_implicit_source_directory_and_suffix_filter(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("ignored.cpp", "int ignored() { return 17; }\n")
        for suffix, included in (("", True), ("SUFFIX answer", False)):
            with self.subTest(suffix=suffix):
                self.write("ya.make", f"LIBRARY()\nUSRV_ALL_SRCS({suffix})\nEND()\n")
                graph = self.graph()
                self.node(graph, "answer.cpp.o")
                self.assertEqual(any(f"$(B)/{PROJECT}/ignored.cpp.o" in node["outputs"] for node in graph["graph"]), included)

    def test_recursive_source_collection_rejects_nested_modules(self):
        self.write("ya.make", "LIBRARY()\nUSRV_ALL_SRCS(RECURSIVE)\nEND()\n")
        self.write("child/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("child/answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def archive(self):
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            for name, text in (("answer.h", '#include "dependency.h"\n'),
                               ("answer.proto", 'syntax = "proto3"; message Answer {}\n')):
                data = text.encode()
                entry = tarfile.TarInfo(name)
                entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))

    def test_explicit_archive_outputs_keep_declared_header_dependencies(self):
        self.archive()
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_ROOT}}/{PROJECT} ${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nUSRV_BUILD(FROM sources.tar OUT_NOAUTO answer.h DEPS_FILE deps.json)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "answer.h"\nint answer() { return 42; }\n')
        self.write("dependency.h", "#pragma once\n")
        self.write("deps.json", json.dumps({"answer.h": ["dependency.h"]}))
        graph = self.graph()
        self.node(graph, "answer.h")
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", self.node(graph, "answer.cpp.o")["inputs"])

    def test_archive_dependency_maps_require_an_entry_for_each_output(self):
        self.archive()
        self.write("ya.make", "LIBRARY()\nUSRV_BUILD(answer.cpp FROM sources.tar DEPS_FILE deps.json)\nEND()\n")
        self.write("deps.json", "{}\n")
        self.assertNotEqual(self.make().returncode, 0)
