import unittest

from tst import lib


def fixture(mode):
    files = lib.java_tree(**{
        "grove/leaf/ya.make": "",
        "grove/leaf/layout.yaml": "kind: leaf\n",
        "grove/leaf/Main.java": "class Main {}\n",
        "grove/reader/ya.make": "JAVA_LIBRARY_NON_SPLIT(reader)\nPEERDIR(grove/leaf)\nEND()\n",
    })
    lib.tool_program(files, "contrib/tools/ijar", "ijar")
    body = "COPY_FILE(${CURDIR}/layout.yaml ${BINDIR}/pack/META-INF/layout.yaml)\n"
    if mode != "unlisted":
        body += "JAVA_SRCS(SRCDIR ${BINDIR}/pack FILES ${BINDIR}/pack/META-INF/layout.yaml)\n"
    if mode == "relative":
        body = body.replace("FILES ${BINDIR}/pack/META-INF/layout.yaml", "FILES pack/META-INF/layout.yaml")
    if mode in ("physical", "physical-relative", "physical-bare"):
        files["grove/leaf/assets/layout.yaml"] = "kind: physical\n"
        body = "JAVA_SRCS(SRCDIR ${CURDIR}/assets FILES ${CURDIR}/assets/layout.yaml)\n"
        if mode == "physical-relative":
            body = "JAVA_SRCS(SRCDIR assets FILES assets/layout.yaml)\n"
        if mode == "physical-bare":
            body = "JAVA_SRCS(SRCDIR assets FILES layout.yaml)\n"
    if mode == "auto":
        body = "COPY_FILE(${CURDIR}/layout.yaml ${BINDIR}/pack/META-INF/layout.yaml AUTO)\n"
    if mode == "context":
        body = body.replace("COPY_FILE(", "COPY_FILE_WITH_CONTEXT(")
    if mode == "outside":
        body = body.replace("${BINDIR}/pack", "${ARCADIA_BUILD_ROOT}/shared/assets")
    if mode in ("chain", "reverse-chain"):
        first = "COPY_FILE(${CURDIR}/layout.yaml ${BINDIR}/stage.yaml)\n"
        second = "COPY_FILE(${BINDIR}/stage.yaml ${BINDIR}/pack/META-INF/layout.yaml)\n"
        tail = "JAVA_SRCS(SRCDIR ${BINDIR}/pack FILES ${BINDIR}/pack/META-INF/layout.yaml)\n"
        body = (first + second if mode == "chain" else second + first) + tail
    files["grove/leaf/ya.make"] = "JAVA_LIBRARY(leaf)\n" + body + "JAVA_SRCS(Main.java)\nEND()\n"
    targets = ["grove/reader"]
    if mode == "order-left":
        targets = ["grove/leaf", "grove/reader"]
    if mode == "order-right":
        targets = ["grove/reader", "grove/leaf"]
    return files, targets


class JavaGeneratedResourceTest(unittest.TestCase):
    def test_copied_resources_retain_one_producer_in_each_native_context(self):
        for mode in ("listed", "relative", "unlisted", "auto", "context", "outside", "chain", "reverse-chain", "order-left", "order-right"):
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets)
                output = "$(B)/shared/assets/META-INF/layout.yaml" if mode == "outside" else "$(B)/grove/leaf/pack/META-INF/layout.yaml"
                copied = lib.node_by_output(graph, output)
                jar = lib.node_by_output(graph, "$(B)/grove/leaf/leaf.jar")
                interface = lib.node_by_output(graph, "$(B)/grove/leaf/leaf-interface.jar")
                self.assertIn(output, interface["inputs"])
                self.assertIn(copied["uid"], interface["deps"])
                if mode in ("unlisted", "auto"):
                    self.assertNotIn(output, jar["inputs"])
                    self.assertNotIn(copied["uid"], jar["deps"])
                else:
                    self.assertIn(output, jar["inputs"])
                    self.assertIn(copied["uid"], jar["deps"])
                    self.assertIn(output, jar["cmds"][0]["cmd_args"])
                self.assertEqual(sum(output in n["outputs"] for n in graph["graph"]), 1)
                if mode in ("chain", "reverse-chain"):
                    stage = lib.node_by_output(graph, "$(B)/grove/leaf/stage.yaml")
                    self.assertIn(stage["outputs"][0], copied["inputs"])
                    self.assertIn(stage["uid"], copied["deps"])
                    self.assertIn("$(S)/grove/leaf/layout.yaml", stage["inputs"])
                else:
                    self.assertIn("$(S)/grove/leaf/layout.yaml", copied["inputs"])
                self.assertIn("$(S)/grove/leaf/Main.java", jar["inputs"])
                reader = lib.node_by_output(graph, "$(B)/grove/reader/reader.jar")
                self.assertIn(interface["uid"], reader["deps"])
                self.assertNotIn(output, reader["inputs"])

    def test_explicit_physical_files_resolve_independently_of_srcdir(self):
        for mode in ("physical", "physical-relative", "physical-bare"):
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets)
                source = "$(S)/grove/leaf/layout.yaml" if mode == "physical-bare" else "$(S)/grove/leaf/assets/layout.yaml"
                jar = lib.node_by_output(graph, "$(B)/grove/leaf/leaf.jar")
                self.assertIn(source, jar["inputs"])
                self.assertIn(source, jar["cmds"][0]["cmd_args"])
                interface = lib.node_by_output(graph, "$(B)/grove/leaf/leaf-interface.jar")
                self.assertNotIn(source, interface["inputs"])
                self.assertFalse(any("$(B)/grove/leaf/pack/META-INF/layout.yaml" in n["outputs"] for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
