import unittest

from tst import lib
from tst.make.test_proto_py2_context import proto_world


def fixture(case='default'):
    files = proto_world()
    peers = 'bundle left' if case == 'declaration_order' else 'left bundle'
    files.update({
        'orchard/ya.make': 'PACKAGE()\nFILES(own.txt)\nPEERDIR(' + peers + ')\nEND()\n',
        'orchard/own.txt': 'own\n',
        'bundle/ya.make': 'UNION()\nFILES(late.txt)\nPEERDIR(right)\nEND()\n',
        'bundle/late.txt': 'late\n',
    })
    for module in ('left', 'right', 'shared'):
        body = 'SRCS(local.cpp)\n' if module != 'shared' else ''
        if case != 'no_globals':
            body += 'RESOURCE(payload.txt left_payload)\n' if case == 'resource' and module == 'left' else 'SRCS(GLOBAL global.cpp)\n'
        if module != 'shared':
            body += 'PEERDIR(shared)\n'
        files[module + '/ya.make'] = 'LIBRARY()\n' + lib.BARE_MODULE + body + 'END()\n'
        files[module + '/local.cpp'] = 'int local;\n'
        files[module + '/global.cpp'] = 'int global;\n'
        files[module + '/payload.txt'] = 'payload\n'
    return files


def package(graph):
    return lib.node_by_output(graph, '$(B)/orchard/orchard.final.pkg.fake')


def copy_args(node):
    return next(c['cmd_args'] for c in node['cmds'] if any(a.endswith('/copy_to_dir.py') for a in c['cmd_args']))


class PackageCopyChannelsTest(unittest.TestCase):
    def check(self, case='default', roots=('orchard',)):
        graph = lib.make(fixture(case), *roots, '--sandboxing', '--target-platform', 'default-linux-x86_64')
        node = package(graph)
        order = ('right', 'left') if case == 'declaration_order' else ('left', 'right')
        globals_ = [] if case == 'no_globals' else [
            '$(B)/shared/libshared.global.a',
            *('$(B)/' + name + '/lib' + name + '.global.a' for name in order),
        ]
        late = ['$(B)/bundle/late.txt', '$(B)/orchard/own.txt']
        ordinary = ['$(B)/' + name + '/lib' + name + '.a' for name in order]
        copied = globals_ + late + ordinary
        self.assertEqual(copy_args(node)[7:-1], copied)
        self.assertEqual(node['outputs'], ['$(B)/orchard/orchard.final.pkg.fake', '$(B)/orchard/own.txt'] +
                         ['$(B)/orchard/' + path.removeprefix('$(B)/') for path in copied])
        for path in globals_ + ordinary + ['$(B)/orchard/own.txt']:
            self.assertIn(path, node['inputs'])
        self.assertNotIn('$(B)/bundle/late.txt', node['inputs'])
        by_uid = {producer['uid']: producer for producer in graph['graph']}
        reachable = set()
        pending = node['deps'][:]
        while pending:
            uid = pending.pop()
            if uid not in reachable:
                reachable.add(uid)
                pending.extend(by_uid[uid]['deps'])
        late_producers = [producer for producer in graph['graph'] if '$(B)/bundle/late.txt' in producer['outputs']]
        self.assertTrue(late_producers)
        self.assertTrue(all(producer['uid'] in reachable for producer in late_producers))
        for path in globals_ + ordinary:
            producers = [producer for producer in graph['graph']
                         if path in producer['outputs'] and producer['uid'] in node['deps']]
            self.assertTrue(producers)
            self.assertTrue(all(producer['inputs'] for producer in producers))
        self.assertIn(node['uid'], graph['result'])
        return node

    def test_native_global_late_and_ordinary_channels(self):
        for case in ('default', 'declaration_order'):
            with self.subTest(case=case):
                self.check(case)

    def test_no_globals_keeps_late_then_ordinary_order(self):
        self.check('no_globals')

    def test_resource_global_uses_the_same_native_channel(self):
        graph = lib.make(fixture('resource'), 'orchard', '--sandboxing', '--target-platform', 'default-linux-x86_64')
        node = package(graph)
        prefix = ['$(B)/shared/libshared.global.a', '$(B)/left/libleft.global.a', '$(B)/right/libright.global.a']
        self.assertEqual(copy_args(node)[7:10], prefix)
        archive = lib.node_by_output(graph, prefix[1])
        objects = [n for n in graph['graph'] if '$(S)/left/payload.txt' in n['inputs'] and n['outputs'][0] in archive['inputs']]
        self.assertTrue(objects)
        for producer in objects:
            self.assertIn(producer['uid'], archive['deps'])
        self.assertIn(archive['uid'], node['deps'])
        self.assertIn(prefix[1], node['inputs'])

    def test_root_order_keeps_package_commands_and_outputs(self):
        first = self.check(roots=('orchard', 'left'))
        second = self.check(roots=('left', 'orchard'))
        self.assertEqual(first['cmds'], second['cmds'])
        self.assertEqual(first['outputs'], second['outputs'])


if __name__ == '__main__':
    unittest.main()
