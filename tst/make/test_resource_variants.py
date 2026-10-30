from tst.make.lib import MakeTest, PROJECT


class ResourceVariantsTest(MakeTest):
    def test_resource_value_from_environment(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(- e2e.environment=$ENV{E2E_RESOURCE_VALUE})\nEND()\n")
        graph = self.graph(env={"E2E_RESOURCE_VALUE": "forty_two"})
        args = [arg for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("e2e.environment=forty_two", args)

    def test_resource_file_keywords_require_values(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for keyword in ("PREFIX", "DEST", "STRIP"):
            with self.subTest(keyword=keyword):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\nRESOURCE_FILES({keyword})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_resource_directory_can_be_relative_to_source_root(self):
        self.write("ya.make", f"LIBRARY()\nALL_RESOURCE_FILES(txt PREFIX /data/ ${{ARCADIA_ROOT}}/{PROJECT}/data)\nEND()\n")
        self.write("data/answer.txt", "42\n")
        inputs = {path for node in self.graph()["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/data/answer.txt", inputs)

    def test_absolute_resource_directory_is_ignored(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nALL_RESOURCE_FILES(txt /ay_e2e_missing_directory)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "answer.cpp.o")
        self.assertFalse(any("ay_e2e_missing_directory" in path for node in graph["graph"] for path in node["inputs"]))

    def test_source_root_relative_generated_resource(self):
        self.write("ya.make", "RECURSE(generated)\n")
        self.write("generated/test/ya.make", f"PY3TEST()\nSET(NLG_LIBRARY_PATH {PROJECT}/generated)\nINCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/shared/resources.inc)\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("shared/resources.inc", "PEERDIR(${NLG_LIBRARY_PATH})\nRESOURCE(${NLG_LIBRARY_PATH}/localizable_phrases.pb.txt /phrases ${NLG_LIBRARY_PATH}/tmacros.pb.txt /macros ${NLG_LIBRARY_PATH}/config.json /config ${NLG_LIBRARY_PATH}/translations.json /translations)\n")
        self.write("generated/test/test_answer.py", "def test_answer():\n    assert 42\n")
        self.write("generated/ya.make", "LIBRARY()\nCOMPILE_NLG(NLG_CONFIG config.json TRANSLATIONS_JSON translations.json answer.nlg)\nEND()\nRECURSE_FOR_TESTS(test)\n")
        self.write("generated/answer.nlg", "{% macro answer() %}42{% endmacro %}\n")
        self.write("generated/config.json", "{}\n")
        self.write("generated/translations.json", "{}\n")
        graph = self.graph("-t", "--output-only-tests", "--add-modules-to-results", "-DADD_PEERDIRS_GEN_TESTS=yes", "-DAUTOCHECK=yes")
        generated = f"$(B)/{PROJECT}/generated/localizable_phrases.pb.txt"
        resources = [node for node in graph["graph"] if generated in node["inputs"] and any(path.endswith((".o", ".cpp")) for path in node["outputs"])]
        self.assertTrue(resources)
        self.assertTrue(any(f"$(B)/{PROJECT}/generated/dummy.h" in node["inputs"] for node in resources))

    def test_parent_relative_resource_resolves_from_root(self):
        self.write("lib/contexts/ya.make", "PY3_LIBRARY()\nRESOURCE(${MODDIR}/../../configs/config.yaml config.yaml)\nEND()\n")
        self.write("configs/config.yaml", "answer: 42\n")
        self.write("ya.make", "RECURSE(lib/contexts)\n")
        graph = self.graph()
        resources = [node for node in graph["graph"] if any("config.yaml" in path for path in node["inputs"])]
        self.assertTrue(resources)
        for node in resources:
            self.assertIn(f"$(S)/{PROJECT}/configs/config.yaml", node["inputs"])

    def test_resource_conftest(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(conftest.py /conftest.py - /inline=answer)\nEND()\n")
        self.write("conftest.py", "ANSWER = 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/conftest.py" in node["inputs"] for node in graph["graph"]))

    def test_resource_uncompressed(self):
        self.write("ya.make", "LIBRARY()\nRESOURCE(DONT_COMPRESS conftest.py /conftest.py)\nEND()\n")
        self.write("conftest.py", "ANSWER = 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/conftest.py" in node["inputs"] for node in graph["graph"]))

    def test_resource_file_extension(self):
        self.write("ya.make", "LIBRARY()\nALL_RESOURCE_FILES(txt PREFIX /data/ data data/deep)\nEND()\n")
        self.write("data/one.txt", "1\n")
        self.write("data/deep/two.txt", "2\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/data/deep/two.txt", inputs)

    def test_resource_generated_header(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(value.h generated.h)\nRESOURCE(generated.h /generated.h)\nEND()\n")
        self.write("value.h", '#include "dependency.h"\n')
        self.write("dependency.h", "#define VALUE 42\n")
        graph = self.graph()
        producer = self.node(graph, "generated.h")
        consumers = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated.h" in node["inputs"]]
        self.assertTrue(consumers)
        self.assertTrue(any(producer["uid"] in node.get("deps", []) for node in consumers))
