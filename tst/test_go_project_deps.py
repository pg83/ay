import unittest

import lib
from test_go_tests import go_proto_world
from test_java import contrib
from test_mlaunch import files as mlaunch_files
from test_proto_desc import desc_files


def vet(graph, project):
    return next(node for node in graph["graph"] if node["kv"].get("path") == project + "/govet")


class GoProjectDependenciesTest(unittest.TestCase):
    def test_descriptor_module_is_a_govet_project_dependency(self):
        for descriptor in (False, True):
            with self.subTest(descriptor=descriptor):
                files = go_proto_world()
                files.update(desc_files())
                tags = "GO_PROTO DESC_PROTO" if descriptor else "GO_PROTO"
                files["p/ya.make"] = f"PROTO_LIBRARY()\nONLY_TAGS({tags})\nSRCS(p.proto)\nEND()\n"
                files["p/p.proto"] = 'syntax = "proto3";\n'
                graph = lib.make(files, "p", "-t")
                suite = vet(graph, "p")
                own = lib.node_by_output(graph, "$(B)/p/p.a")
                context = lib.node_by_output(graph, "$(B)/common_test.context")
                expected = {context["uid"], own["uid"]}
                if descriptor:
                    desc = lib.node_by_output(graph, "$(B)/p/p.self.protodesc")
                    expected.add(desc["uid"])
                self.assertEqual(set(suite["deps"]), expected)

    def test_java_module_is_a_govet_project_dependency(self):
        for java in (False, True):
            with self.subTest(java=java):
                files = go_proto_world()
                files.update(lib.java_tree())
                files.update(contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7))
                tags = "GO_PROTO JAVA_PROTO" if java else "GO_PROTO"
                files["p/ya.make"] = f"PROTO_LIBRARY()\nONLY_TAGS({tags})\nSRCS(p.proto)\nEND()\n"
                files["p/p.proto"] = 'syntax = "proto3";\n'
                graph = lib.make(files, "p", "-t")
                suite = vet(graph, "p")
                own = lib.node_by_output(graph, "$(B)/p/p.a")
                context = lib.node_by_output(graph, "$(B)/common_test.context")
                expected = {context["uid"], own["uid"]}
                if java:
                    jar = lib.node_by_output(graph, "$(B)/p/p.jar")
                    expected.add(jar["uid"])
                self.assertEqual(set(suite["deps"]), expected)

    def test_global_archive_is_a_govet_project_dependency(self):
        for cpp in (False, True):
            with self.subTest(cpp=cpp):
                files = mlaunch_files()
                tags = "CPP_MLAUNCH GO_MLAUNCH" if cpp else "GO_MLAUNCH"
                files["component/ya.make"] = (
                    "MLAUNCH_LIBRARY(part)\n" + lib.BARE_MODULE
                    + f'ONLY_TAGS({tags})\nGOLAUNCH_SRCS(part.go)\nGOLAUNCH_FUNCTION(Start)\nIF(MODULE_TAG == "CPP_MLAUNCH")\n'
                    "SRCS(main.cpp GLOBAL register.cpp)\nENDIF()\nEND()\n"
                )
                files["component/main.cpp"] = "int main_part;\n"
                files["component/register.cpp"] = "int registered;\n"
                files["component/part.go"] = "package part\nfunc Start() {}\n"
                files["build/scripts/append_file.py"] = ""
                graph = lib.make(files, "component", "-t")
                suite = vet(graph, "component")
                context = lib.node_by_output(graph, "$(B)/common_test.context")
                own = lib.node_by_output(graph, "$(B)/component/part.a")
                expected = {context["uid"], own["uid"]}
                if cpp:
                    archive = lib.node_by_output(graph, "$(B)/component/libpart.a")
                    global_archive = lib.node_by_output(graph, "$(B)/component/libpart.global.a")
                    expected.update((archive["uid"], global_archive["uid"]))
                self.assertEqual(set(suite["deps"]), expected)



if __name__ == "__main__":
    unittest.main(verbosity=2)
