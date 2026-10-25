"""Global/direct resource outputs retain their native source action positions."""
from collections import Counter
import unittest

from tst import lib
from tst.make.resource_source_order_cases import CASES
from tst.make.test_python_srcs import python_base

MODULE = 'grove/ledger'
B = '$(B)/' + MODULE + '/'


def fixture(case):
    files = python_base()
    files['tasklet/v1/domain/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + 'END()\n'
    files['build/scripts/gen_tasklet_reg.py'] = ''
    for name in ('cpp_wrapper.h', 'go_wrapper.h', 'py_wrapper.h', 'js_wrapper.h', 'registry.h'):
        files['tasklet/v1/runtime/lib/' + name] = ''
    files.update(case['files'])
    lib.tool_program(files, 'tools/writer', 'writer')
    files.setdefault('other/ya.make', 'LIBRARY(other)\n' + lib.BARE_MODULE + 'SRCS(other.cpp)\nEND()\n')
    files['other/other.cpp'] = ''
    for directory in ('sandbox/bin', 'sandbox/sdk2', 'sandbox/taskbox/worker'):
        files[directory + '/ya.make'] = 'PY23_LIBRARY()\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nEND()\n'
    for path in (MODULE + '/ya.make', 'tools/runner/ya.make', 'other/ya.make'):
        if path in files:
            lines = files[path].splitlines(True)
            lines.insert(1, lib.BARE_MODULE)
            files[path] = ''.join(lines)
    return files


def make(case):
    roots = ['other', MODULE] if case['mode'] == 'root-last' else [MODULE, 'other'] if case['mode'] == 'root-first' else [MODULE]
    return lib.make(fixture(case), roots[-1], *roots[:-1], '-DPIC=' + ('yes' if case['mode'] == 'pic' else 'no'),
                    '--target-platform', 'default-linux-aarch64' if case['mode'] in ('arm', 'arm-host') else 'default-linux-x86_64')


class ResourceSourceOrderTest(unittest.TestCase):
    def test_python_callback_source_results_match_native_order(self):
        from tst.make.python_callback_source_cases import CASES as CALLBACK_CASES
        from tst.make.test_python2 import python2_base

        for case in CALLBACK_CASES:
            with self.subTest(case=case["name"]):
                is_test = case["module"] == "PY2TEST"
                files = python2_base() if is_test else python_base()
                lib.tool_program(files, "tools/py2cc", "py2cc")
                files["library/python/pytest/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
                files.update(case["files"])
                lines = files[MODULE + "/ya.make"].splitlines(True)
                lines.insert(1, lib.BARE_MODULE)
                files[MODULE + "/ya.make"] = "".join(lines)
                graph = lib.make(files, MODULE, "--target-platform", "default-linux-x86_64")
                target = next(n for n in graph["graph"] if n["outputs"][0].startswith(B)
                              and n["kv"]["p"] == ("LD" if is_test else "AR")
                              and (is_test or n["outputs"][0].endswith(".global.a")))
                expected = case["members"]
                members = [a if a.startswith("$(B)/") else "$(B)/" + a
                           for c in target["cmds"] for a in c["cmd_args"] if a.endswith(".o")]
                if is_test:
                    # Native compiler/runtime scaffolding differs; compare all eight
                    # original source callback results, with no change to their order.
                    members = [a for a in members if a in expected]
                self.assertEqual(members, expected)
                for output in expected:
                    node = lib.node_by_output(graph, output)
                    self.assertIn(output, target["inputs"])
                    self.assertIn(node["uid"], target["deps"])
                    for dependency in node["deps"]:
                        self.assertTrue(any(n["uid"] == dependency for n in graph["graph"]))

    def assert_case(self, case):
        graph = make(case)
        byout = {o: n for n in graph['graph'] for o in n['outputs']}
        for group in case['groups']:
            node = byout[group['output']]
            scope = group['output'].rsplit('/', 1)[0] + '/'
            objects = {o: n for o, n in byout.items() if o.startswith(scope) and o.endswith('.o')}
            args = node['cmds'][2]['cmd_args'] if group['kind'] == 'LD' else node['cmds'][0]['cmd_args']
            members = [a for a in args if a in objects or '$(B)/' + a in objects]
            self.assertEqual(members, group['members'])
            expected = [m if m.startswith('$(B)/') else '$(B)/' + m for m in group['members']]
            self.assertEqual(Counter(i for i in node['inputs'] if i in objects), Counter(expected))
            for out in expected:
                producers = [n for n in graph['graph'] if out in n['outputs'] and n['uid'] in node['deps']]
                self.assertTrue(producers, (out, node['deps']))
                producer = producers[0]
                self.assertTrue(set(producer['outputs']) <= set(node['inputs']))
                for dependency in producer['deps']:
                    self.assertTrue(any(n['uid'] == dependency for n in graph['graph']))
            if group['kind'] != 'LD':
                # Assert the complete actual archive command and its ordered members.
                self.assertEqual(args, ['', '$(S)/build/scripts/link_lib.py', '', 'LLVM_AR', 'gnu', '$(B)', 'None', '--', '--',
                                        group['output'], *expected])
        for out, node in byout.items():
            if not out.startswith(B) or not out.endswith('.cpp.o') or B + 'formed.cpp' not in node['inputs']:
                continue
            producer = byout[B + 'formed.cpp']
            self.assertEqual(producer['outputs'], [B + 'receipt.dat', B + 'formed.cpp'])
            self.assertIn(producer['uid'], node['deps'])
            self.assertIn(B + 'receipt.dat', node['inputs'])
        if 'source-shadow' in case['name']:
            packed = next(n for n in graph['graph'] if n['outputs'][0].startswith(B) and '$(S)/grove/ledger/formed.txt' in n['inputs'])
            self.assertNotIn(B + 'formed.txt', packed['inputs'])
            self.assertNotIn(B + 'receipt.dat', packed['inputs'])
        for node in graph['graph']:
            args = node['cmds'][0]['cmd_args']
            if node['outputs'][0].startswith(B) and '$(S)/build/scripts/objcopy.py' in args:
                for tool in ('rescompiler', 'rescompressor'):
                    toolnode = byout[f'$(B)/tools/{tool}/{tool}']
                    self.assertIn(toolnode['outputs'][0], node['inputs'])
                    self.assertIn(toolnode['uid'], node['deps'])
        if case['mode'] == 'arm-host':
            host = byout['$(B)/tools/runner/entry.cpp.pic.o']
            target = byout[B + 'stone.cpp.o']
            self.assertNotEqual(host['platform'], target['platform'])

    def check(self, predicate):
        cases = [c for c in CASES if predicate(c)]
        self.assertTrue(cases)
        for case in cases:
            with self.subTest(case=case['name']):
                self.assert_case(case)

    def test_original_cpp_module_resource_and_python_source_calls(self):
        self.check(lambda c: c['name'].startswith('library-') and not c['mode'])

    def test_python_library_source_resource_positions_and_pending_actions(self):
        self.check(lambda c: c['name'].startswith('py3_library-') and not c['mode'])

    def test_python_program_source_resource_positions_and_pending_actions(self):
        self.check(lambda c: c['name'].startswith('py3_program_bin-'))

    def test_pic_and_both_root_orders(self):
        self.check(lambda c: c['mode'] in ('pic', 'root-first', 'root-last'))

    def test_declared_macro_sources_and_scoped_tag_controls(self):
        self.check(lambda c: c['name'].startswith(('tasklet-declared-src', 'cpp-namespace')))

    def test_arm_and_host_tool_resource_contexts(self):
        self.check(lambda c: c['mode'] in ('arm', 'arm-host', 'host'))


if __name__ == '__main__':
    unittest.main(verbosity=2)
