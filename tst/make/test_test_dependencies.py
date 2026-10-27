import json

from tst.make.lib import MakeTest, PROJECT


class TestDependenciesTest(MakeTest):
    # The gate's references are cut without ya's idle-result stripping
    # (YA_STRIP_IDLE_BUILD_RESULTS=0), which ay does not model; under it the
    # non-final variants of a DEPENDS multimodule stay in the results.
    def autocheck_graph(self):
        return self.graph('--dist', '--sandboxing', '--ignore-nodes-exit-code', '--warning-mode', 'dirloops;ChkPeers;allbadrecurses', '--cache-tests', '--output-only-tests', '--cache-namespace', 'AC', '-j', '0', '-DAUTOCHECK=yes', '-DUSE_PREBUILT_TOOLS=no', '--host-platform-flag', 'AUTOCHECK=yes', '--host-platform-flag', 'USE_PREBUILT_TOOLS=no', '--target-platform', 'default-linux-x86_64', '--target-platform-build-type', 'relwithdebinfo', '--target-platform-tests', '--target-platform-flag', 'AUTOCHECK=yes', '--target-platform-flag', 'DEBUGINFO_LINES_ONLY=yes', '--target-platform-flag', 'ADD_PEERDIRS_GEN_TESTS=yes', '--report-skipped-suites', '--tests-retries', '1', '--test-size', 'small', '--test-size', 'medium', '--build', 'release', '-k', '--backup-test-results', '--add-modules-to-results', '--do-not-output-stderrs', '--no-src-links', '--add-changed-ok-configures', '--no-yt-store', env={"YA_STRIP_IDLE_BUILD_RESULTS": "0"})

    def test_depends_keeps_udf_variants(self):
        self.write("tests/test_answer.py", "def test_answer(): assert True\n")
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/udf)\nEND()\n")
        self.write("udf/ya.make", "YQL_UDF(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("udf/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.autocheck_graph()
        for output in ("udf/libanswer.so", "udf/libanswer.global.a", "udf/yql-abi-current-answer.global.a"):
            with self.subTest(output=output):
                self.assertIn(self.node(graph, output)["uid"], graph["result"])

    def test_depends_keeps_proto_schema_languages(self):
        self.write("tests/test_answer.py", "def test_answer(): assert True\n")
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/schema)\nEND()\n")
        self.write("schema/ya.make", "PROTO_SCHEMA()\nONLY_TAGS(CPP_PROTO_FROM_SCHEMA PY3_PROTO_FROM_SCHEMA TS_PROTO_FROM_SCHEMA TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/answer)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.autocheck_graph()
        self.assertIn(self.node(graph, "schema/output.tar")["uid"], graph["result"])
        for output in ("schema/answer.pb.cc", "schema/answer__intpy3___pb2.py"):
            with self.subTest(output=output):
                self.node(graph, output)

    def test_reject_nonfinal_multimodule_dependencies(self):
        self.write("run.py", "print(42)\n")
        variants = {
            "proto": 'PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n',
            "schema": 'PROTO_SCHEMA()\nONLY_TAGS(CPP_PROTO PY3_PROTO DESC_PROTO)\nSRCS(answer.proto)\nEND()\n',
            "python": 'PY23_LIBRARY()\nPY_SRCS(answer.py)\nEND()\n',
        }
        for name, content in variants.items():
            with self.subTest(name=name):
                self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/{name})\nRUN(python3 ${{ARCADIA_ROOT}}/{PROJECT}/run.py)\nEND()\n")
                self.write(name + "/ya.make", content)
                self.write(name + "/answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
                self.write(name + "/answer.py", "answer = 42\n")
                self.write(name + "/value.txt", "42\n")
                result = self.make("-t")
                self.assertNotEqual(result.returncode, 0, "DEPENDS must reject a multimodule without final targets")

        self.write("python/ya.make", "PY23_LIBRARY()\nONLY_TAGS(PY3)\nPY_SRCS(answer.py)\nEND()\n")
        self.graph("-t")

    def test_depends_on_typescript_library_waits_for_build_only(self):
        self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/lib)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        self.write("lib/ya.make", "TS_LIBRARY()\nTS_BUILD_SCRIPT(nots:build)\nTS_BUILD_OUTPUTS(dist)\nEND()\n")
        self.write("lib/package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}, "files": ["dist"], "scripts": {"nots:build": "tsc"}}))
        self.write("lib/pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
        self.write("lib/tsconfig.json", json.dumps({"compilerOptions": {"rootDir": "src", "outDir": "dist"}, "include": ["src/**/*.ts"]}))
        self.write("lib/src/answer.ts", "export const answer = 42;\n")
        graph = self.graph("-t")
        deps = set(self.node(graph, "test-results/py3test/meta.json")["deps"])
        outputs = {path for node in graph["graph"] if node["uid"] in deps for path in node["outputs"]}
        self.assertIn(f"$(B)/{PROJECT}/lib/output.tar", outputs)
        self.assertNotIn(f"$(B)/{PROJECT}/lib/package.json", outputs)

    def test_union_dependencies(self):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/bundle)\nRUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/tool/answer)\nEND()\n")
        self.write("bundle/ya.make", f"UNION()\nPEERDIR({PROJECT}/tool {PROJECT}/library)\nFILES(value.txt)\nEND()\n")
        self.write("bundle/value.txt", "42\n")
        self.write("tool/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.write("library/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("library/answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph("-t"), "tool/answer")

    def test_remote_canonical_data(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    return 42\n")
        resources = [
            {"uri": "https://storage.yandex-team.ru/get-devtools/123456/answer#part", "checksum": "a1b2"},
            {"uri": "https://{canondata_backend}/123456/answer", "checksum": "a1b2"},
            {"uri": "sbr://123456789/answer.txt", "checksum": "a1b2"},
        ]
        self.write("canondata/result.json", json.dumps({"test_answer.test_answer": {"nested": resources * 2, "value": 42}}))
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        self.assertIn("fetch_from_mds.py", commands)
        self.assertIn("123456/answer", commands)

    def test_docker_images(self):
        digest = "a" * 64
        images = [f"docker://registry.example.com/answer@sha256:{digest}=" + "tag" + "x" * length for length in (0, 3, 12, 30, 48, 90)]
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nDOCKER_IMAGE(" + " ".join(images) + ")\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert True\n")
        graph = self.graph("-t")
        nodes = [node for node in graph["graph"] if "fetch_from_docker_repo.py" in json.dumps(node.get("cmds", []))]
        self.assertEqual(len(nodes), len(images))
