"""LDFLAGS scalar binding precedes final command argument construction."""
import unittest

from tst import lib
from tst.make.test_link_global_values import fixture, command_with, LINKER


def files_for(seed, clone, own=""):
    files = fixture(peers="seed clone", own=False)
    for path, text in (("seed", seed), ("clone", clone)):
        files[path + "/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}{text}\nSRCS(unit.cpp)\nEND()\n"
        )
    if own:
        files["app/ya.make"] = files["app/ya.make"].replace("END()", own + "\nEND()")
    return files


def link_values(node):
    args = command_with(node)
    return args[args.index("-Wl,--gdb-index") + 1:args.index("-nodefaultlibs")]


class LinkValueBindingTest(unittest.TestCase):
    def check(self, seed, clone, expected, own=""):
        graph = lib.make(files_for(seed, clone, own), "app")
        node = lib.node_by_output(graph, "$(B)/app/app")
        self.assertEqual(link_values(node), LINKER + expected)
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))
        for name in ("seed", "clone"):
            archive = lib.node_by_output(graph, f"$(B)/{name}/lib{name}.a")
            self.assertIn(archive["uid"], node["deps"])
            self.assertIn(archive["outputs"][0], node["inputs"])
            obj = lib.node_by_output(graph, f"$(B)/{name}/unit.cpp.o")
            self.assertEqual(obj["inputs"][0], f"$(S)/{name}/unit.cpp")
            self.assertIn(obj["uid"], archive["deps"])
        return graph, node

    def test_quoted_macro_argument_and_split_words_bind_identical_values(self):
        for seed in ('LDFLAGS("-Wl,seed with space")',
                     'LDFLAGS("-Wl,seed  with   space")',
                     'SET(FLAGS -Wl,seed with space)\nLDFLAGS($FLAGS)',
                     'SET(FLAGS "-Wl,seed with space")\nLDFLAGS(${FLAGS})'):
            with self.subTest(seed=seed):
                self.check(seed, 'LDFLAGS(-Wl,seed with space)', ['-Wl,seed', 'with', 'space'])

    def test_nested_quotes_are_parsed_only_after_the_scalar_assignment(self):
        self.check(r'LDFLAGS("\"-Wl,seed with space\"")',
                   r'LDFLAGS("\"-Wl,seed with space\"")', ['-Wl,seed with space'])
        self.check(r'LDFLAGS("\"-Wl,seed\"")', 'LDFLAGS(-Wl,seed)', ['-Wl,seed'])
        self.check('LDFLAGS("-Wl,seed" "-Wl,shared")', 'LDFLAGS(-Wl,seed -Wl,shared)',
                   ['-Wl,seed', '-Wl,shared'])

    def test_scalar_escaping_precedes_raw_and_quoted_command_escaping(self):
        for seed, expected in (
            (r'LDFLAGS("-Wl,seed\ with\ space")', ['-Wl,seed', 'with', 'space']),
            (r'LDFLAGS("-Wl,seed\\ with\\ space")', ['-Wl,seed', 'with', 'space']),
            (r'LDFLAGS("\"-Wl,seed\\q with space\"")', ['-Wl,seedq with space']),
            (r'LDFLAGS(-Wl,"seed\\q with space")', [r'-Wl,seed\q with space']),
            (r'LDFLAGS(-Wl,seed\ with\ space)', ['-Wl,seed', 'with', 'space']),
        ):
            with self.subTest(seed=seed):
                self.check(seed, seed, expected)

    def test_owner_value_uses_the_same_rendering_and_is_appended_after_peers(self):
        self.check('LDFLAGS("-Wl,seed with space")', 'LDFLAGS(-Wl,seed with space)',
                   ['-Wl,seed', 'with', 'space'] * 2, own='LDFLAGS("-Wl,seed with space")')

    def test_source_aliases_bind_at_the_macro_call_before_later_assignments(self):
        for after_end in (False, True):
            with self.subTest(after_end=after_end):
                files = files_for('SET(VALUE -Wl,seed)\nLDFLAGS($VALUE)', 'LDFLAGS(-Wl,seed)')
                change = 'SET(VALUE -Wl,tail)\n'
                if after_end:
                    files['seed/ya.make'] += change
                else:
                    files['seed/ya.make'] = files['seed/ya.make'].replace('END()', change + 'END()')
                graph = lib.make(files, 'app')
                self.assertEqual(link_values(lib.node_by_output(graph, '$(B)/app/app')), LINKER + ['-Wl,seed'])

    def test_resource_placeholder_retains_its_actual_fetched_producer(self):
        declaration = 'DECLARE_EXTERNAL_RESOURCE(BOX sbr:73)\n'
        graph, node = self.check(
            declaration + 'LDFLAGS("-L${BOX_RESOURCE_GLOBAL}/lib with space")',
            declaration + 'LDFLAGS(-L${BOX_RESOURCE_GLOBAL}/lib with space)',
            ['-L$(B)/resources/BOX/lib', 'with', 'space'],
        )
        resource = lib.node_by_output(graph, '$(B)/resources/BOX')
        self.assertIn('sbr:73', resource["cmds"][0]["cmd_args"])
        self.assertIn('-L$(B)/resources/BOX/lib', command_with(node))


if __name__ == "__main__":
    unittest.main()
