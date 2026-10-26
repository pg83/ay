from tst.make.lib import MakeTest, PROJECT


class YqlPrebuiltContextsTest(MakeTest):
    def graph(self, *flags):
        return super().graph("--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "-DAUTOCHECK=yes", *flags)

    def package_udf(self, prebuilt):
        self.write("ya.make", f"PACKAGE()\nPEERDIR({self.project_name}/udf)\nEND()\n")
        resource = "SET(ANSWER_UDF_RESOURCE_DEFAULT 123456789)\n" if prebuilt else ""
        self.write("udf/ya.make", f"YQL_UDF_CONTRIB(answer_udf)\n{resource}SRCS(answer.cpp)\nSTYLE_CPP()\nEND()\n")
        self.write("udf/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("udf/answer.h", "#pragma once\n")

    def test_prebuilt_udf_does_not_style_ignored_sources(self):
        self.package_udf(True)
        node = self.node(self.graph("-t", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes"), "udf/test-results/clang_format/meta.json")
        self.assertNotIn(f"$(S)/{self.project_name}/udf/answer.cpp", node["inputs"])

    def test_prebuilt_udf_validates_resource(self):
        self.package_udf(True)
        self.node(self.graph("-t", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes"), "udf/test-results/validate_resource/meta.json")

    def test_source_udf_styles_vcs_template(self):
        self.package_udf(False)
        node = self.node(self.graph("-t", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes"), "udf/test-results/clang_format/meta.json")
        self.assertIn("$(S)/build/scripts/c_templates/svn_interface.c", node["inputs"])

    def test_prebuilt_udf_does_not_test_unused_source_peers(self):
        self.package_udf(True)
        self.write("udf/ya.make", f"YQL_UDF_CONTRIB(answer_udf)\nSET(ANSWER_UDF_RESOURCE_DEFAULT 123456789)\nSRCS(answer.cpp)\nPEERDIR({self.project_name}/unused)\nSTYLE_CPP()\nEND()\n")
        self.write("unused/ya.make", "LIBRARY()\nSRCS(unused.cpp)\nSTYLE_CPP()\nEND()\n")
        self.write("unused/unused.cpp", "int unused() { return 0; }\n")
        graph = self.graph("-t", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes")
        self.assertFalse(any(f"$(B)/{self.project_name}/unused/test-results/clang_format/meta.json" in node["outputs"] for node in graph["graph"]))

    def test_recursed_udf_dependency_keeps_static_peer_checks(self):
        self.package_udf(True)
        self.write("udf/ya.make", f"YQL_UDF_CONTRIB(answer_udf)\nSET(ANSWER_UDF_RESOURCE_DEFAULT 123456789)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/lib)\nSTYLE_CPP()\nEND()\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(shared.cpp)\nSTYLE_CPP()\nEND()\n")
        self.write("lib/shared.cpp", "int shared() { return 42; }\n")
        self.write("test/ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/udf)\nEND()\n")
        self.write("test/test_answer.py", "def test_answer():\n    pass\n")
        for order in ("udf test", "test udf"):
            with self.subTest(order=order):
                self.write("ya.make", f"RECURSE({order})\n")
                node = self.node(self.graph("-t", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes"), "lib/test-results/clang_format/meta.json")
                self.assertIn(f"$(S)/{PROJECT}/lib/shared.cpp", node["inputs"])
