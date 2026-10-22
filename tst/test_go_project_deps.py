import unittest

import lib
from test_go_tests import go_module, go_proto_world
from test_java import contrib
from test_mlaunch import files as mlaunch_files
from test_proto_desc import desc_files


def vet(graph, project):
    return next(node for node in graph["graph"] if node["kv"].get("path") == project + "/govet")


class GoProjectDependenciesTest(unittest.TestCase):
    def test_project_query_includes_depends_from_later_suites_in_either_target_order(self):
        files = go_proto_world()
        files.update(desc_files())
        files['p/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO DESC_PROTO)\nSRCS(p.proto)\nEND()\n'
        files['p/p.proto'] = 'syntax = "proto3";\n'
        files['catalog/ya.make'] = 'PROTO_DESCRIPTIONS()\nPEERDIR(p)\nEND()\n'
        files['tests/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(catalog)\nEND()\n'
        go_module(files, 'root', macros='PEERDIR(p)\n')
        for targets in (('root', 'tests'), ('tests', 'root')):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, '-t', '--target-platform-flag',
                                 'ADD_PEERDIRS_GEN_TESTS=yes', '--strip-packages-from-results')
                suite = vet(graph, 'p')
                descriptor = lib.node_by_output(graph, '$(B)/p/p.self.protodesc')
                self.assertIn(descriptor['uid'], suite['deps'])

    def test_unretained_peer_descriptor_does_not_become_a_project_dependency(self):
        files = go_proto_world()
        files.update(desc_files())
        files['p/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO DESC_PROTO)\nPEERDIR(dep)\nSRCS(p.proto)\nEND()\n'
        files['p/p.proto'] = 'syntax = "proto3";\n'
        files['dep/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO DESC_PROTO)\nSRCS(dep.proto)\nEND()\n'
        files['dep/dep.proto'] = 'syntax = "proto3";\n'
        for requested in (False, True):
            with self.subTest(requested=requested):
                graph = lib.make(files, 'p', *(['dep'] if requested else []), '-t',
                                 '--target-platform-flag', 'ADD_PEERDIRS_GEN_TESTS=yes',
                                 '--strip-packages-from-results')
                suite = vet(graph, 'dep')
                expected = {node['uid'] for node in graph['graph'] if any(output in node['outputs'] for output in (
                    '$(B)/dep/dep.a', '$(B)/common_test.context'))}
                if requested:
                    expected.add(lib.node_by_output(graph, '$(B)/dep/dep.self.protodesc')['uid'])
                else:
                    self.assertFalse(any('$(B)/dep/dep.self.protodesc' in node['outputs'] for node in graph['graph']))
                self.assertEqual(set(suite['deps']), expected)

    def test_project_query_uses_materialized_contexts_outside_suite_discovery(self):
        files = go_proto_world()
        files.update(desc_files())
        files.update(lib.java_tree())
        files['p/ya.make'] = 'PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO DESC_PROTO)\nSRCS(p.proto)\nEND()\n'
        files['p/p.proto'] = 'syntax = "proto3";\n'
        go_module(files, 'app', module='GO_PROGRAM', macros='PEERDIR(p)\n')
        for descriptor in (False, True):
            with self.subTest(descriptor=descriptor):
                files['java/ya.make'] = 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n' + ('RECURSE(../p)\n' if descriptor else '')
                graph = lib.make(files, 'java', 'app', '-t', '--target-platform-flag',
                                 'ADD_PEERDIRS_GEN_TESTS=yes', '--strip-packages-from-results')
                suite = vet(graph, 'p')
                expected = {lib.node_by_output(graph, output)['uid'] for output in (
                    '$(B)/p/p.a', '$(B)/common_test.context')}
                if descriptor:
                    expected.add(lib.node_by_output(graph, '$(B)/p/p.self.protodesc')['uid'])
                else:
                    self.assertFalse(any('$(B)/p/p.self.protodesc' in node['outputs'] for node in graph['graph']))
                self.assertEqual(set(suite['deps']), expected)

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
