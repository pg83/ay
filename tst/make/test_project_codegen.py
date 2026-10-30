from tst.make.lib import MakeTest, PROJECT


class ProjectCodegenTest(MakeTest):
    def test_yt_record_declared_headers_reach_generated_compilation(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nGENERATE_YT_RECORD(answer.yaml OUTPUT_INCLUDES dependency.h)\nEND()\n")
        self.write("answer.yaml", "namespace: NE2E\ntypes: []\n")
        self.write("dependency.h", "#pragma once\n")
        graph = self.graph()
        generated = self.node(graph, "answer.record.cpp")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn(["--output-include", "dependency.h"], [args[i:i + 2] for i in range(len(args) - 1)])
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", self.node(graph, "answer.record.cpp.o")["inputs"])

    def test_yt_record_rejects_missing_inputs_and_unknown_sections(self):
        self.write("answer.yaml", "namespace: NE2E\ntypes: []\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for args in ("", "answer.yaml INVALID dependency.h"):
            with self.subTest(args=args):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\nGENERATE_YT_RECORD({args})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_nlg_can_generate_empty_cpp_bodies(self):
        self.write("ya.make", "LIBRARY()\nSET(COMPILE_NLG_EMPTY_CPP yes)\nCOMPILE_NLG(answer.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", "{% macro answer() %}42{% endmacro %}\n")
        self.write("config.json", "{}\n")
        self.write("translations.json", "{}\n")
        args = [arg for command in self.node(self.graph(), "answer.nlg.cpp")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--empty-cpp", args)

    def test_nlg(self):
        self.write("ya.make", "LIBRARY()\nCOMPILE_NLG(answer.nlg base.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", f'{{% from "{PROJECT}/base.nlg" import answer %}}\n{{{{ answer() }}}}\n')
        self.write("base.nlg", "{% macro answer() %}42{% endmacro %}\n")
        self.write("config.json", "{}\n")
        self.write("translations.json", "{}\n")
        graph = self.graph()
        self.node(graph, "answer.nlg.cpp")

    def test_yql_prebuilt_program(self):
        self.write("ya.make", "YQL_PREBUILT_PROGRAM(answer)\nSET(ANSWER_RESOURCE_DEFAULT 123456789)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.node(self.graph(), "answer")

    def test_yql_current_library(self):
        self.write("ya.make", f"PROGRAM(answer)\nYQL_CURRENT_ABI_VERSION()\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "YQL_LIBRARY(lib)\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.assertTrue(any("abi_current" in path for node in graph["graph"] for path in node["outputs"]))

    def test_sdc_metadata_kinds(self):
        self.write("ya.make", f"PROGRAM(answer)\nUSE_SDC_META()\nPEERDIR({PROJECT}/meta)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("meta/ya.make", "LIBRARY()\nSDC_SB_RESOURCE_INFO(123456789 answer UNPACK)\nSDC_ROSNODE_INFO(answer)\nSDC_SOLIB_INFO(https://example.com/answer.so)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith(".sdcmeta") for node in graph["graph"] for path in node["outputs"]))

    def test_yt_record(self):
        self.write("ya.make", "LIBRARY()\nGENERATE_YT_RECORD(answer.yaml)\nEND()\n")
        self.write("answer.yaml", "namespace: NE2E\ntypes:\n  - type_name: TAnswer\n    fields:\n      - cpp_name: Value\n        cpp_type: i64\n        column_name: value\n        column_type: Int64\n")
        self.node(self.graph(), "answer.record.cpp")
