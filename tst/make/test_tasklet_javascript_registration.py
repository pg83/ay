from tst.make.lib import MakeTest


class TaskletJavascriptRegistrationTest(MakeTest):
    def test_javascript_registration_links_its_runtime(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nTASKLET_REG(answer js e2e::Answer {self.project_name}/answer.h)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("answer.h", "#pragma once\nnamespace e2e { class Answer {}; }\n")
        graph = self.graph()
        self.node(graph, "answer.task.cpp")
        self.assertTrue(any(path.startswith("$(B)/tasklet/v1/runtime/js/") and path.endswith(".a") for node in graph["graph"] for path in node["outputs"]))
