import unittest

from tst import lib


MODES = ("glob", "deferred_mixed", "append_new", "append_literal_tail", "nested", "python_embedded", "alias", "append", "append_literal", "call", "deferred", "explicit", "overwritten", "python", "python_bare", "args", "template", "rooted", "empty", "double", "leading")


def fixture(mode):
    body = "DECLARE_IN_DIRS(PAGES *.cpp DIRS .)\n"
    inputs = "${PAGES_FILES}"
    args = ""
    macro = "RUN_PROGRAM(tools/scanner"
    if mode == "deferred_mixed":
        body += "SET(INPUT_FILES \\${PAGES_FILES} ${PAGES_FILES})\n"
        inputs = "\\${INPUT_FILES}"
    if mode == "append_new":
        body += "SET_APPEND(INPUT_FILES ${PAGES_FILES})\n"
        inputs = "${INPUT_FILES}"
    if mode == "alias":
        body += "SET(INPUT_FILES ${PAGES_FILES})\n"
        inputs = "${INPUT_FILES}"
    if mode in ("append", "append_literal"):
        body += "SET(INPUT_FILES " + ("entry.cpp" if mode == "append_literal" else "${PAGES_FILES}") + ")\n"
        body += "SET_APPEND(INPUT_FILES " + ("${PAGES_FILES}" if mode == "append_literal" else "${EMPTY}") + ")\n"
        inputs = "${INPUT_FILES}"
    if mode == "append_literal_tail":
        body += "SET(INPUT_FILES ${PAGES_FILES})\nSET_APPEND(INPUT_FILES entry.cpp)\n"
        inputs = "${INPUT_FILES}"
    if mode == "double":
        body += "SET(INPUT_FILES ${PAGES_FILES} ${PAGES_FILES})\n"
        inputs = "${INPUT_FILES}"
    if mode == "leading":
        body += 'SET(INPUT_FILES " ${PAGES_FILES}")\n'
        inputs = "\\${INPUT_FILES}"
    if mode == "empty":
        body = "DECLARE_IN_DIRS(PAGES *.missing DIRS .)\n"
    if mode == "deferred":
        inputs = "\\${PAGES_FILES}"
    if mode == "explicit":
        inputs = "entry.cpp"
    if mode == "overwritten":
        body += "SET(PAGES_FILES entry.cpp)\n"
    if mode == "nested":
        body += 'SET(EMPTY "")\nSET(PREFIX \\${EMPTY})\nSET(INPUT_FILES ${PREFIX}${PAGES_FILES})\n'
        inputs = "${INPUT_FILES}"
    if mode.startswith("python"):
        macro = "RUN_PYTHON3(gen.py"
        if mode == "python_embedded":
            body += "SET(FILE entry.cpp)\n"
            inputs = "./$FILE"
        if mode == "python_bare":
            inputs = "$PAGES_FILES"
    if mode == "args":
        args = ' --empty "" --separator ";" --and "&&"'
    section = "IN"
    call = macro + args + " " + section + " " + inputs + " OUT report.cpp)\n"
    if mode == "call":
        body += "SET(CALL tools/scanner IN ${PAGES_FILES} OUT report.cpp)\n"
        call = "RUN_PROGRAM(${CALL})\n"
    if mode == "template":
        body = "DECLARE_IN_DIRS(PAGES *.h.in DIRS .)\n"
    files = {
        "grove/reader/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(consumer.cpp)\n" + body + call + "END()\n",
        "grove/reader/consumer.cpp": '#include "entry.cpp"\n',
        "grove/reader/entry.cpp": '#include "api.h"\n',
        "grove/reader/api.h": '#include "deep.h"\n',
        "grove/reader/deep.h": "#pragma once\n",
        "grove/reader/seed.h.in": '#include "api.h"\n',
        "grove/reader/gen.py": "print('generated')\n",
    }
    if mode == "rooted":
        files["grove/reader/ya.make"] = files["grove/reader/ya.make"].replace("DECLARE_IN_DIRS(PAGES *.cpp DIRS .)", "DECLARE_IN_DIRS(PAGES *.cpp|*.h DIRS . RECURSIVE EXCLUDES drop.cpp SRCDIR ${ARCADIA_ROOT}/grove/pages)")
        files["grove/pages/direct.cpp"] = '#include "../reader/api.h"\n'
        files["grove/pages/sub/next.cpp"] = '#include "../../reader/api.h"\n'
        files["grove/pages/drop.cpp"] = '#include "must_not_parse.h"\n'
    lib.tool_program(files, "tools/scanner", "scanner")
    if mode.startswith("contexts-"):
        common = "DECLARE_IN_DIRS(PAGES *.cpp DIRS .)\nSET(INPUT_FILES ${PAGES_FILES})\nRUN_PROGRAM(tools/scanner IN ${INPUT_FILES} OUT report.cpp)\n"
        files["shared/discover.inc"] = common
        for name in ("reader", "other"):
            files["grove/"+name+"/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(consumer.cpp)\nINCLUDE(${ARCADIA_ROOT}/shared/discover.inc)\nEND()\n"
            files["grove/"+name+"/consumer.cpp"] = '#include "entry.cpp"\n'
            files["grove/"+name+"/entry.cpp"] = '#include "api.h"\n'
            files["grove/"+name+"/api.h"] = "#pragma once\n"
        order = "other reader" if mode == "contexts-ba" else "reader other"
        files["grove/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(root.cpp)\nEND()\nRECURSE(" + order + ")\n"
        files["grove/root.cpp"] = "int root;\n"
        return files, "grove", []
    return files, "grove/reader", []


class LateGlobInputScopeTest(unittest.TestCase):
    def test_late_glob_text_links_preserve_declared_inputs_without_scanning_them(self):
        parsed = {"append_literal", "append_literal_tail", "explicit", "overwritten", "python_embedded"}
        for mode in MODES:
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, *flags)
                producer = lib.node_by_output(graph, "$(B)/grove/reader/report.cpp")
                if mode != "empty":
                    expected = "$(S)/grove/reader/seed.h.in" if mode == "template" else "$(S)/grove/pages/direct.cpp" if mode == "rooted" else "$(S)/grove/reader/entry.cpp"
                    self.assertIn(expected, producer["inputs"])
                else:
                    self.assertEqual(producer["inputs"], ["$(B)/tools/scanner/scanner"])
                for header in ("api.h", "deep.h"):
                    self.assertEqual("$(S)/grove/reader/" + header in producer["inputs"], mode in parsed)
                consumer = lib.node_by_output(graph, "$(B)/grove/reader/consumer.cpp.o")
                self.assertIn("$(S)/grove/reader/deep.h", consumer["inputs"])
                compiled = lib.node_by_output(graph, "$(B)/grove/reader/report.cpp.o")
                self.assertIn(producer["uid"], compiled["deps"])
                if mode == "template":
                    self.assertNotIn("$(S)/grove/reader/deep.h", compiled["inputs"])

    def test_shared_lexical_discovery_keeps_each_modules_text_binding(self):
        for mode in ("contexts-ab", "contexts-ba"):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", *flags)
                for name in ("reader", "other"):
                    producer = lib.node_by_output(graph, "$(B)/grove/" + name + "/report.cpp")
                    self.assertCountEqual(producer["inputs"], ["$(B)/tools/scanner/scanner", "$(S)/grove/" + name + "/consumer.cpp", "$(S)/grove/" + name + "/entry.cpp"])

    def test_explicit_and_late_references_to_the_same_source_keep_the_parsed_edge(self):
        files, target, flags = fixture("glob")
        files[target + "/ya.make"] = files[target + "/ya.make"].replace("IN ${PAGES_FILES}", "IN entry.cpp ${PAGES_FILES}")
        graph = lib.make(files, target, *flags)
        producer = lib.node_by_output(graph, "$(B)/grove/reader/report.cpp")
        self.assertIn("$(S)/grove/reader/api.h", producer["inputs"])
        self.assertIn("$(S)/grove/reader/deep.h", producer["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
