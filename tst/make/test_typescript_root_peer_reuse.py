import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptRootPeerReuseTest(MakeTest):
    def package(self, directory, macros=""):
        self.write(f"{directory}/ya.make", f"TS_PACKAGE()\n{macros}\nEND()\n")
        self.write(f"{directory}/package.json", json.dumps({"name": "@e2e/" + directory, "version": "1.0.0", "engines": {"node": "22.13.0"}}))
        self.write(f"{directory}/pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        self.write(f"{directory}/src/value.ts", "export const answer = 42;\n")

    def test_recursed_typescript_package_reuses_its_peer_output(self):
        self.write("ya.make", "RECURSE(shared app)\n")
        self.package("shared")
        self.package("app", f"PEERDIR({PROJECT}/shared)")
        graph = self.graph()
        shared = self.node(graph, "shared/output.tar")
        app = self.node(graph, "app/output.tar")
        self.assertIn(shared["uid"], app["deps"])
        self.assertIn(f"$(B)/{PROJECT}/shared/output.tar", app["inputs"])
