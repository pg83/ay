from tst.make.lib import MakeTest, PROJECT


class GeneratedMetadataTest(MakeTest):
    def test_generated_cython_induced_dependencies(self):
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        self.write("generate.py", "print('generator')\n")
        self.write("dependency.pxd", "cdef int value\n")
        self.write("native.h", '#include "leaf.h"\n')
        self.write("leaf.h", "#pragma once\n")
        for generator in (f"RUN_PROGRAM({PROJECT}/generator", "RUN_PYTHON3(generate.py"):
            with self.subTest(generator=generator):
                self.write("ya.make", f"PY3_LIBRARY()\nPREPARE_INDUCED_DEPS(PYX_DEPS pyx ${{ARCADIA_ROOT}}/{PROJECT}/dependency.pxd)\nPREPARE_INDUCED_DEPS(CPP_DEPS cpp ${{ARCADIA_ROOT}}/{PROJECT}/native.h)\n{generator} OUT_NOAUTO answer.pyx INDUCED_DEPS $PYX_DEPS $CPP_DEPS)\nPY_SRCS(answer.pyx)\nEND()\n")
                graph = self.graph()
                cython = self.node(graph, "answer.pyx.cpp")
                compiled = self.node(graph, "answer.pyx.cpp.o")
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", cython["inputs"])
                self.assertNotIn(f"$(S)/{PROJECT}/native.h", cython["inputs"])
                self.assertNotIn(f"$(S)/{PROJECT}/dependency.pxd", compiled["inputs"])
                for header in ("native.h", "leaf.h"):
                    self.assertIn(f"$(S)/{PROJECT}/{header}", compiled["inputs"])

    def test_induced_dependencies_through_generator(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nPREPARE_INDUCED_DEPS(DEPS proto ${{ARCADIA_ROOT}}/{PROJECT}/dependency.proto)\nRUN_PROGRAM({PROJECT}/generator OUT_NOAUTO stage.tmp INDUCED_DEPS $DEPS)\nRUN_PROGRAM({PROJECT}/generator IN stage.tmp OUT_NOAUTO answer.proto)\nSRCS(answer.proto)\nEND()\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        self.write("dependency.proto", 'syntax = "proto3";\nmessage Dependency { int32 value = 1; }\n')
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/dependency.proto", self.node(graph, "answer.pb.h")["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/dependency.proto", self.node(graph, "answer.proto")["inputs"])

    def test_flatbuffers_binary_schema_resource(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.fbs64)\nRESOURCE(answer.bfbs64 /schema)\nEND()\n")
        self.write("answer.fbs64", "namespace e2e;\ntable Answer { value: int; }\nroot_type Answer;\n")
        graph = self.graph()
        resources = [node for node in graph["graph"] if f"$(B)/{PROJECT}/answer.bfbs64" in node["inputs"] and any("/objcopy_" in path for path in node["outputs"])]
        self.assertEqual(len(resources), 1)
        self.assertIn(f"$(B)/{PROJECT}/answer.fbs64.h", resources[0]["inputs"])

    def test_flatbuffers_native_include(self):
        for suffix in ("fbs", "fbs64"):
            with self.subTest(suffix=suffix):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.{suffix})\nEND()\n")
                self.write(f"answer.{suffix}", f'native_include "{PROJECT}/native.h";\nnamespace e2e;\ntable Answer {{ value: int; }}\nroot_type Answer;\n')
                self.write("native.h", '#include "leaf.h"\n')
                self.write("leaf.h", "#pragma once\n")
                graph = self.graph()
                generator = self.node(graph, f"answer.{suffix}.h")
                compiled = self.node(graph, f"answer.{suffix}.cpp.o")
                for node in (generator, compiled):
                    for header in ("native.h", "leaf.h"):
                        self.assertIn(f"$(S)/{PROJECT}/{header}", node["inputs"])

    def test_generated_proto_induced_dependencies(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nPREPARE_INDUCED_DEPS(DEPS proto ${{ARCADIA_ROOT}}/{PROJECT}/dependency.proto)\nRUN_PROGRAM({PROJECT}/generator OUT_NOAUTO answer.proto INDUCED_DEPS $DEPS)\nSRCS(answer.proto)\nEND()\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        self.write("dependency.proto", f'syntax = "proto3";\nimport "{PROJECT}/leaf.proto";\nmessage Dependency {{ Leaf value = 1; }}\n')
        self.write("leaf.proto", 'syntax = "proto3";\nmessage Leaf { int32 value = 1; }\n')
        graph = self.graph()
        node = self.node(graph, "answer.pb.h")
        for source in ("dependency.proto", "leaf.proto"):
            self.assertIn(f"$(S)/{PROJECT}/{source}", node["inputs"])

    def test_run_program_output_before_stdout(self):
        self.write("ya.make", f"UNION()\nRUN_PROGRAM({PROJECT}/generator OUT answer.txt STDOUT_NOAUTO output.log)\nEND()\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        node = next(node for node in self.graph()["graph"] if f"$(B)/{PROJECT}/answer.txt" in node["outputs"] and not any(path.endswith(".pkg.fake") for path in node["outputs"]))
        self.assertEqual(node["outputs"], [f"$(B)/{PROJECT}/answer.txt", f"$(B)/{PROJECT}/output.log"])
