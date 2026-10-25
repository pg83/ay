"""Real metadata peers and failures outside final executor roots."""
import json
import sys
import unittest
from collections import Counter

import lib
from test_exec_make import Workspace, library, plain, program
from test_go_tests import go_world, go_module


def ancestors(graph, ref):
    nodes = {node['uid']: node for node in graph['graph']}
    assert len(nodes) == len(graph['graph'])
    seen = set()
    pending = [ref]
    while pending:
        current = pending.pop()
        if current in seen:
            continue
        seen.add(current)
        node = nodes[current]
        refs = list(node['deps'])
        refs.extend(ref for group in node.get('foreign_deps', {}).values() for ref in group)
        assert all(ref in nodes for ref in refs)
        pending.extend(refs)
    return seen


class SourceExecutorCoverageTest(unittest.TestCase):
    def test_union_go_metadata_peer_builds_its_own_go_dependency(self):
        files = go_world()
        files['bundle/ya.make'] = 'UNION()\nPEERDIR(orchard/parent)\nEND()\n'
        files['parcel/ya.make'] = 'PACKAGE()\nPEERDIR(bundle)\nEND()\n'
        go_module(files, 'orchard/parent', 'import "a.yandex-team.ru/orchard/leaf"\n', macros='PEERDIR(orchard/leaf)\n')
        go_module(files, 'orchard/leaf')
        graph = lib.make(files, 'bundle', 'parcel', '--add-modules-to-results')
        parent = lib.node_by_output(graph, '$(B)/orchard/parent/parent.a')
        leaf = lib.node_by_output(graph, '$(B)/orchard/leaf/leaf.a')
        self.assertIn('$(S)/orchard/parent/parent.go', parent['inputs'])
        self.assertIn('$(S)/orchard/leaf/leaf.go', leaf['inputs'])
        self.assertIn(leaf['outputs'][0], parent['inputs'])
        self.assertEqual(Counter(ref for ref in parent['deps'] if ref == leaf['uid']), Counter([leaf['uid']]))
        self.assertEqual(parent['platform'], leaf['platform'])
        self.assertIn(leaf['uid'], ancestors(graph, parent['uid']))
        self.assertIn(parent['uid'], graph['result'])
        self.assertTrue(all(ref in {n['uid'] for n in graph['graph']} for ref in graph['result']))

    def test_keep_going_reports_failed_nonresult_vcs_after_archive_success(self):
        ws = Workspace(self, {'lib/ya.make': library(['source.cpp']),
                              'lib/source.cpp': 'int source;\n',
                              'app/ya.make': program('probe', ['main.cpp'], 'PEERDIR(lib)\n'),
                              'app/main.cpp': 'int main(){return 0;}\n'})
        graph_result = ws.make('-j0', '-G', targets=('lib', 'app'))
        graph = json.loads(graph_result.stdout)
        archive = lib.node_by_output(graph, '$(B)/lib/liblib.a')
        linked = lib.node_by_output(graph, '$(B)/app/probe')
        vcs = next(node for node in graph['graph'] if any('base64' == arg for cmd in node['cmds'] for arg in cmd['cmd_args']))
        self.assertEqual(vcs['outputs'], ['$(B)/vcs.json'])
        self.assertNotIn(vcs['uid'], ancestors(graph, archive['uid']))
        self.assertIn(vcs['uid'], ancestors(graph, linked['uid']))
        self.assertNotIn(vcs['uid'], graph['result'])
        self.assertCountEqual(graph['result'], [archive['uid'], linked['uid']])
        wrapper = ws.root / 'reject_vcs.py'
        wrapper.write_text('import os,sys\n'
                           'args=sys.argv[1:]\n'
                           'if len(args)==5 and args[1:3]==["fetch","base64"] and args[4].endswith("/vcs.json"):\n'
                           ' sys.exit("intentional standalone vcs failure")\n'
                           'os.execv(args[0],args)\n')
        result = ws.make('-j1', '-k', '--cmd-prefix',
                         f'{vcs["cmds"][0]["cmd_args"][0]}={sys.executable} {wrapper}', targets=('lib', 'app'), check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('intentional standalone vcs failure', result.stderr)
        self.assertIn(f"build failed: {linked['uid']}\n", plain(result.stderr))
        self.assertIn('$(B)/app/probe broken by dep $(B)/vcs.json', plain(result.stderr))
        ws.meta_for('$(B)/lib/liblib.a')
        self.assertFalse((ws.inst / 'lib' / 'liblib.a').exists())
        self.assertFalse((ws.inst / 'app' / 'probe').exists())
        self.assertNotIn('panic:', result.stderr)


if __name__ == '__main__':
    unittest.main()
