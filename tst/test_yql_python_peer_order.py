import unittest

import lib
from test_yql_python_main_config import files as main_files


HEADER = (
    'yql/essentials/public/udf',
    'yql/essentials/public/udf/support',
    'library/python/type_info',
)
BODY = ('orchard/shared', 'orchard/left', 'orchard/right')
BINDINGS = 'yql/essentials/udfs/common/python/bindings'
PYTHON_UDF = 'yql/essentials/udfs/common/python/python_udf'
ENTRY = 'cedar/entry/main'
LIBRA = ('quality/user_sessions/libra_arc/noyql', 'yql/udfs/quality/libra/module')


def fixture(case):
    py3 = not case.startswith('py2')
    sources = main_files('python3' if py3 else 'internal')
    main = 'library/python/runtime_py3/main' if py3 else 'library/python/runtime/main'
    sqlite = 'contrib/tools/python3/Modules/_sqlite' if py3 else 'contrib/tools/python/src/Modules/_sqlite'
    entry = 'yql/essentials/udfs/common/python/main_py3' if py3 else ENTRY
    markers = (*HEADER, *BODY, main, sqlite, BINDINGS, PYTHON_UDF, entry)
    if case == 'py3_libra':
        markers += LIBRA
        sources[LIBRA[1] + '/module.h'] = ''
    for index, peer in enumerate(markers):
        peers = ''
        if peer in ('orchard/left', main):
            peers = 'PEERDIR(orchard/shared)\n'
        if peer == PYTHON_UDF:
            peers = f'PEERDIR({BINDINGS})\n'
        sources[peer + '/ya.make'] = (
            f'LIBRARY()\n{lib.BARE_MODULE}{peers}SRCS(part.cpp GLOBAL marker.cpp)\nEND()\n'
        )
        sources[peer + '/part.cpp'] = f'int ordinary_{index};\n'
        sources[peer + '/marker.cpp'] = f'int global_{index};\n'
    register = 'REGISTER_YQL_PYTHON_UDF()\n'
    peers = 'PEERDIR(orchard/left orchard/right)\n'
    body = register + peers if case.endswith('after') else peers + register
    if case in ('py3_twice', 'py3_libra'):
        body = ('REGISTER_YQL_PYTHON_UDF(NAME CustomPythonFirst)\n'
                'PEERDIR(orchard/left)\n'
                'REGISTER_YQL_PYTHON_UDF(NAME CustomPythonSecond)\n'
                'PEERDIR(orchard/right)\n')
    if case == 'py3_libra':
        body = body.replace('NAME CustomPythonFirst)', 'NAME CustomPythonFirst ADD_LIBRA_MODULES yes)')
    if case == 'py3_no_register':
        body = peers
    if case == 'py3_system':
        body = 'SET(USE_ARCADIA_PYTHON no)\n' + register + peers
    if case == 'py3_no_sqlite':
        body = 'DISABLE(PYTHON_SQLITE3)\n' + register + peers
    kind = 'YQL_PYTHON3_UDF' if py3 else 'YQL_PYTHON_UDF'
    sources['grove/ya.make'] = (
        f'{kind}(pollen)\n{lib.BARE_MODULE}UDF_NO_PROBE()\n'
        'SET(YQL_PYTHON_DIR cedar/entry)\n' + body + 'SRCS(body.cpp)\nEND()\n'
    )
    expected = [*HEADER, *BODY]
    if case != 'py3_system':
        expected.append(main)
        if case != 'py3_no_sqlite':
            expected.append(sqlite)
    if case != 'py3_no_register':
        expected.extend((BINDINGS, PYTHON_UDF))
        if case == 'py3_libra':
            expected.extend(LIBRA)
        if case != 'py3_system':
            expected.append(entry)
    if case == 'py3_libra':
        old, new = 'orchard/shared', 'library/cpp/nectar/shared'
        sources = {key.replace(old, new): value.replace(old, new) for key, value in sources.items()}
        markers = tuple(peer.replace(old, new) for peer in markers)
        expected = [peer.replace(old, new) for peer in expected]
        sources['quality/user_sessions/linters.make.inc'] = '# Minimal fixture ownership boundary.\n'
    return sources, markers, expected


def marker_archives(graph, markers, dependencies=None):
    return {
        peer: next(node for node in graph['graph']
                   if (dependencies is None or node['uid'] in dependencies)
                   and any(out.startswith('$(B)/' + peer + '/') and out.endswith('.global.a')
                           for out in node['outputs']))
        for peer in markers
        if any(out.startswith('$(B)/' + peer + '/') and out.endswith('.global.a')
               for node in graph['graph'] for out in node['outputs'])
    }


class YqlPythonPeerOrderTest(unittest.TestCase):
    def check_case(self, case, *roots, owner_first=False):
        sources, markers, expected = fixture(case)
        targets = ('grove', *roots) if owner_first else (*roots, 'grove')
        graph = lib.make(sources, targets[-1], *targets[:-1], opensource=case != 'py3_libra')
        link = lib.node_by_output(graph, '$(B)/grove/libpollen.so')
        args = next(cmd['cmd_args'] for cmd in link['cmds']
                    if '$(S)/build/scripts/link_dyn_lib.py' in cmd['cmd_args'])
        archives = marker_archives(graph, markers, link['deps'])
        by_output = {node['outputs'][0][5:]: peer for peer, node in archives.items()}
        self.assertEqual([by_output[arg] for arg in args if arg in by_output], expected)
        regular = {out[5:]: peer for peer in markers for node in graph['graph']
                   for out in node['outputs']
                   if out.startswith('$(B)/' + peer + '/') and out.endswith('.a') and not out.endswith('.global.a')}
        self.assertEqual([regular[arg] for arg in args if arg in regular], expected)
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        for node in graph['graph']:
            self.assertLessEqual(set(node['deps']), ids)
        for peer in expected:
            archive = archives[peer]
            self.assertIn(archive['outputs'][0], link['inputs'])
            self.assertIn(archive['uid'], link['deps'])
            source = '$(S)/' + peer + '/marker.cpp'
            producer = next(node for node in graph['graph'] if source in node['inputs'] and node['uid'] in archive['deps']
                            and node.get('kv', {}).get('p') == 'CC')
            self.assertIn(producer['uid'], archive['deps'])
        return graph

    def test_explicit_peers_precede_plugins_in_both_source_orders_and_python_versions(self):
        for case in ('py2_before', 'py2_after', 'py3_before', 'py3_after'):
            with self.subTest(case=case):
                self.check_case(case)

    def test_two_registration_calls_keep_both_real_generated_sources(self):
        graph = self.check_case('py3_twice')
        for name in ('CustomPythonFirst', 'CustomPythonSecond'):
            source = f'$(B)/grove/{name}.yql_python_udf.cpp'
            script = lib.node_by_output(graph, source)
            compiler = next(node for node in graph['graph']
                            if source in node['inputs'] and node['kv']['p'] == 'CC')
            self.assertIn(script['uid'], compiler['deps'])

    def test_registration_specific_libra_peers_follow_first_plugin_call(self):
        self.check_case('py3_libra')

    def test_optional_runtime_sqlite_and_registration_keep_header_peers(self):
        for case in ('py3_no_register', 'py3_no_sqlite', 'py3_system'):
            with self.subTest(case=case):
                self.check_case(case)

    def test_explicit_peer_root_keeps_consumer_archive_order(self):
        for owner_first in (False, True):
            with self.subTest(owner_first=owner_first):
                self.check_case('py3_after', 'orchard/right', owner_first=owner_first)


if __name__ == '__main__':
    unittest.main()
