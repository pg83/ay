import unittest

from tst import lib
from tst.make.test_proto_cfgproto import cfgproto_files


MODULE = "ochre/parcel"


class ProtoSourceResolutionTest(unittest.TestCase):
    def check_source(self, token, directories, present, selected):
        files = cfgproto_files()
        files[f"{MODULE}/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}"
            + (f"SRCDIR({' '.join(directories)})\n" if directories else "")
            + f"SRCS({token})\nEND()\n"
        )
        for directory in present:
            files[f"{directory}/schema.cfgproto"] = (
                f'option (NProtoConfig.Include) = "{directory}/chosen.h";\n'
                "message Schema {}\n"
            )
            files[f"{directory}/chosen.h"] = '#include "detail.h"\n'
            files[f"{directory}/detail.h"] = "// selected configuration\n"
        graph = lib.make(files, MODULE)
        generated = lib.node_by_output(graph, f"$(B)/{selected}/schema.cfgproto.pb.h")
        self.assertIn(f"$(S)/{selected}/schema.cfgproto", generated["inputs"])
        self.assertIn(f"{selected}/schema.cfgproto", generated["cmds"][0]["cmd_args"])
        compiled = next(n for n in graph["graph"] if n["kv"].get("p") == "CC"
                        and generated["outputs"][0] in n["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(f"$(S)/{selected}/chosen.h", compiled["inputs"])
        self.assertIn(f"$(S)/{selected}/detail.h", compiled["inputs"])
        for directory in set(present) - {selected}:
            self.assertNotIn(f"$(S)/{directory}/schema.cfgproto", generated["inputs"])
            self.assertNotIn(f"$(S)/{directory}/chosen.h", compiled["inputs"])
        archive = lib.node_by_output(graph, f"$(B)/{MODULE}/libochre-parcel.a")
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(compiled["outputs"][0], archive["inputs"])

    def test_source_directories_use_last_present_candidate_before_module(self):
        for present, selected in (
            ([MODULE, "ochre/low", "ochre/high"], "ochre/high"),
            ([MODULE, "ochre/low"], "ochre/low"),
            ([MODULE], MODULE),
        ):
            with self.subTest(present=present):
                self.check_source("schema.cfgproto", ["ochre/low", "ochre/high"], present, selected)

    def test_relative_components_resolve_before_generated_outputs_and_header_scans(self):
        for token in ("./schema.cfgproto", "nested/../schema.cfgproto"):
            with self.subTest(token=token):
                self.check_source(token, ["ochre/low", "ochre/high"],
                                  [MODULE, "ochre/low"], "ochre/low")

    def test_relative_components_fall_back_to_the_module(self):
        self.check_source("./schema.cfgproto", [], [MODULE], MODULE)


if __name__ == "__main__":
    unittest.main(verbosity=2)
