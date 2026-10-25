import unittest

from tst import lib
from tst.make.test_python_proto import args, python_base


def fixture(body):
    files = {
        p.replace('app/', 'reader/'): text.replace('PEERDIR(proto)', 'PEERDIR(schema)')
        for p, text in python_base().items()
    }
    files.update({
        'schema/ya.make': 'PROTO_LIBRARY()\n' + body + '\nEXCLUDE_TAGS(CPP_PROTO)\nEND()\n',
        'schema/wire.proto': 'syntax = "proto3";\n',
        'schema/ordinary.py': '',
        'schema/note.dat': 'note',
    })
    return files


class ProtoAuxSourceOrderTest(unittest.TestCase):
    def check_order(self, body, expected, generated=False):
        files = fixture(body)
        if generated:
            files['schema/seed.proto'] = files.pop('schema/wire.proto')
        graph = lib.make(files, 'reader', '--sandboxing')
        archive = lib.node_by_output(graph, '$(B)/schema/libpy3schema.global.a')
        members = [a for a in args(archive) if a.endswith('.o')]
        labels = []
        for output in members:
            node = lib.node_by_output(graph, output)
            self.assertIn(node['uid'], archive['deps'])
            words = args(node)
            labels.append('proto' if '_raw.auxcpp' in output else
                          'namespace' if any('py/namespace/' in a for a in words) else
                          'resource' if '$(S)/schema/note.dat' in node['inputs'] else
                          'proto' if any('wire_pb2' in a for a in words) else 'python')
        self.assertEqual(labels, expected)
        pb = lib.node_by_output(graph, '$(B)/schema/wire__intpy3___pb2.py')
        source = '$(B)/schema/wire.proto' if generated else '$(S)/schema/wire.proto'
        self.assertIn(source, pb['inputs'])
        if generated:
            self.assertIn(lib.node_by_output(graph, source)['uid'], pb['deps'])
        bytecode = next(n for n in graph['graph'] if any(o.endswith('.yapyc3') for o in n['outputs'])
                        and pb['outputs'][0] in n['inputs'])
        self.assertIn(pb['uid'], bytecode['deps'])
        packed = next(n for n in graph['graph'] if bytecode['outputs'][0] in n['inputs']
                      and any(o.endswith(('.auxcpp', '.o')) for o in n['outputs']))
        self.assertIn(bytecode['uid'], packed['deps'])
        self.assertIn(pb['outputs'][0], packed['inputs'])

    def test_proto_source_call_keeps_its_position_among_python_calls(self):
        for before in (True, False):
            with self.subTest(before=before):
                body = ('SRCS(wire.proto)\nPY_SRCS(ordinary.py)' if before else
                        'PY_SRCS(ordinary.py)\nSRCS(wire.proto)')
                ordinary = ['namespace', 'python']
                self.check_order(body, ['proto'] + ordinary if before else ordinary + ['proto'])

    def test_resource_priority_precedes_both_source_calls(self):
        for first in (True, False):
            with self.subTest(first=first):
                body = ('RESOURCE(note.dat note)\nSRCS(wire.proto)\nPY_SRCS(ordinary.py)' if first else
                        'PY_SRCS(ordinary.py)\nSRCS(wire.proto)\nRESOURCE(note.dat note)')
                self.check_order(body, ['resource', 'proto', 'namespace', 'python'] if first else
                                 ['resource', 'namespace', 'python', 'proto'])

    def test_recursive_proto_python_call_follows_ordinary_rows_in_same_call(self):
        for sources in ('wire.proto ordinary.py', 'ordinary.py wire.proto', 'wire.proto=wire.api ordinary.py'):
            with self.subTest(sources=sources):
                self.check_order('PY_SRCS(' + sources + ')', ['namespace', 'python', 'proto'])

    def test_explicit_module_alias_omits_namespace_without_moving_proto(self):
        self.check_order('PY_SRCS(ordinary.py=renamed)\nSRCS(wire.proto)', ['python', 'proto'])

    def test_generated_proto_aliases_keep_producer_and_source_call_order(self):
        for body, expected in (
            ('COPY_FILE(seed.proto wire.proto)\nSRCS(wire.proto)\nPY_SRCS(ordinary.py)', ['proto', 'namespace', 'python']),
            ('PY_SRCS(ordinary.py)\nSRCS(wire.proto)\nCOPY_FILE(seed.proto wire.proto)', ['namespace', 'python', 'proto']),
            ('COPY_FILE(seed.proto wire.proto)\nSRCS(${ARCADIA_BUILD_ROOT}/schema/wire.proto)\nPY_SRCS(ordinary.py)', ['proto', 'namespace', 'python']),
        ):
            with self.subTest(body=body):
                self.check_order(body, expected, generated=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
