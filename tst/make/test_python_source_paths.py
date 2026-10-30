import base64

from tst.make.lib import MakeTest, PROJECT


class PythonSourcePathsTest(MakeTest):
    def test_explicit_generated_source_keeps_its_producer(self):
        for module, suffix in (("PY2_LIBRARY", ".yapyc"), ("PY3_LIBRARY", ".yapyc3")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}()\nCOPY_FILE(template.py generated.py)\nPY_SRCS(${{BINDIR}}/generated.py)\nEND()\n")
                self.write("template.py", "VALUE = 42\n")
                graph = self.graph()
                generated = self.node(graph, "generated.py")
                compiled = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated.py" in node["inputs"] and any(path.endswith(suffix) for path in node["outputs"])]
                self.assertEqual(len(compiled), 1)
                self.assertIn(generated["uid"], compiled[0]["deps"])
                packaged_inputs = {path for node in graph["graph"] if node != compiled[0] for path in node["inputs"]}
                self.assertTrue(set(compiled[0]["outputs"]) & packaged_inputs)

    def test_python_two_explicit_module_name_is_packaged(self):
        self.write("ya.make", "PY2_LIBRARY()\nPY_SRCS(answer.py=custom.answer)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        graph = self.graph()
        args = [arg for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        for key in ("/py_modules/custom.answer", "/py_code/custom.answer"):
            self.assertIn(base64.b64encode(key.encode()).decode(), args)

    def test_missing_explicit_python_two_source_is_an_error(self):
        self.write("ya.make", "PY2_LIBRARY()\nPY_SRCS(${BINDIR}/missing.py)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_multiple_namespaces_share_the_same_source_directory(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(NAMESPACE second second.py NAMESPACE first first.py)\nEND()\n")
        self.write("first.py", "VALUE = 42\n")
        self.write("second.py", "VALUE = 17\n")
        graph = self.graph()
        args = [arg for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        namespaces = [arg for arg in args if arg.startswith("py/namespace/") and f"/{PROJECT}=" in arg]
        self.assertTrue(namespaces)
        self.assertTrue(any(arg.endswith("=first.:second.") for arg in namespaces), namespaces)

    def test_proto_parent_segments_resolve_in_source_directories(self):
        for location in ("", "src"):
            with self.subTest(location=location):
                self.write("ya.make", f"PY3_LIBRARY()\nSRCDIR({PROJECT}/empty {PROJECT}/src)\nPY_SRCS(nested/../answer.proto)\nEND()\n")
                self.write("empty/.keep", "")
                self.write("src/nested/.keep", "")
                self.write("nested/.keep", "")
                source = f"{location}/answer.proto".lstrip("/")
                self.write(source, 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
                graph = self.graph()
                expected = f"$(S)/{PROJECT}/{source}"
                protoc = [node for node in graph["graph"] if expected in node["inputs"] and any(path.endswith("_pb2.py") for path in node["outputs"])]
                self.assertEqual(len(protoc), 1)
                (self.project / source).unlink()
