import base64
import hashlib
import unittest

from tst import lib
from tst.make.test_yql import udf_fixture


class YqlNodeContentTest(unittest.TestCase):
    def test_python_udf_config_peers_precede_python_plugin_peers(self):
        for declaration, main, sqlite in (
            ('YQL_PYTHON3_UDF', 'library/python/runtime_py3/main', 'contrib/tools/python3/Modules/_sqlite'),
            ('YQL_PYTHON_UDF', 'library/python/runtime/main', 'contrib/tools/python/src/Modules/_sqlite'),
        ):
            with self.subTest(declaration=declaration):
                files = udf_fixture()
                peers = [
                    'yql/essentials/public/udf', 'yql/essentials/public/udf/support',
                    'build/platform/python/ldflags', 'library/python/type_info', main, sqlite,
                ]
                for peer in peers:
                    files[f'{peer}/ya.make'] = (
                        f'LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL {peer}/include)\n'
                        'SRCS(local.cpp GLOBAL global.cpp)\nEND()\n'
                    )
                    files[f'{peer}/include/marker.h'] = ''
                    files[f'{peer}/local.cpp'] = ''
                    files[f'{peer}/global.cpp'] = ''
                files['udf/ya.make'] = f'{declaration}(named)\n{lib.BARE_MODULE}UDF_NO_PROBE()\nSRCS(body.cpp)\nEND()\n'
                files['udf/body.cpp'] = ''
                graph = lib.make(files, 'udf', '--target-platform', 'default-linux-x86_64')
                compile_args = lib.node_by_output(graph, '$(B)/udf/body.cpp.pic.o')['cmds'][0]['cmd_args']
                self.assertEqual(
                    [arg for arg in compile_args if arg.endswith('/include') and arg.startswith('-I$(S)/')],
                    [f'-I$(S)/{peer}/include' for peer in peers],
                )
                link = lib.node_by_output(graph, '$(B)/udf/libnamed.so')
                args = next(cmd['cmd_args'] for cmd in link['cmds'] if '$(S)/build/scripts/link_dyn_lib.py' in cmd['cmd_args'])
                archives = [arg for arg in args if any(arg.startswith(peer + '/') for peer in peers) and arg.endswith('.a')]
                self.assertEqual(
                    archives,
                    [next(output.removeprefix('$(B)/') for node in graph['graph'] for output in node['outputs']
                          if output.startswith(f'$(B)/{peer}/lib') and output.endswith(suffix)
                          and (suffix == '.global.a' or not output.endswith('.global.a')))
                     for suffix in ('.global.a', '.a') for peer in peers],
                )

    def test_native_python_binding_resource_hash_uses_native_module_tag(self):
        files = udf_fixture()
        files['library/python/runtime/main/ya.make'] = f'LIBRARY()\n{lib.BARE_MODULE}END()\n'
        files['binding/ya.make'] = f'PY23_NATIVE_LIBRARY()\n{lib.BARE_MODULE}RESOURCE(payload.txt payload)\nEND()\n'
        files['binding/payload.txt'] = 'payload\n'
        for declaration, tag in (('YQL_PYTHON3_UDF', 'PY3_NATIVE'), ('YQL_PYTHON_UDF', 'PY2_NATIVE')):
            with self.subTest(tag=tag):
                files['udf/ya.make'] = f'{declaration}(named)\n{lib.BARE_MODULE}UDF_NO_PROBE()\nDISABLE(PYTHON_SQLITE3)\nPEERDIR(binding)\nEND()\n'
                graph = lib.make(files, 'udf', '--target-platform', 'default-linux-x86_64')
                seed = ','.join(sorted(['payload.txt', base64.b64encode(b'payload').decode(), '$S/binding'])) + tag
                digest = hashlib.md5(seed.encode()).hexdigest()[:26]
                resource = lib.node_by_output(graph, f'$(B)/binding/objcopy_{digest}.o')
                archive = next(node for node in graph['graph']
                               if any(output.startswith('$(B)/binding/lib') and output.endswith('.global.a')
                                      for output in node['outputs']))
                self.assertIn(resource['uid'], archive['deps'])
                self.assertIn(resource['outputs'][0], archive['inputs'])

    def test_udf_probe_follows_split_debug_commands(self):
        files = udf_fixture()
        files['udf/ya.make'] = f'YQL_UDF(named)\n{lib.BARE_MODULE}SPLIT_DWARF()\nSRCS(body.cpp)\nEND()\n'
        files['udf/body.cpp'] = ''
        graph = lib.make(files, 'udf', '--target-platform', 'default-linux-x86_64')
        link = lib.node_by_output(graph, '$(B)/udf/libnamed.so')
        self.assertEqual(link['cmds'][-1]['cmd_args'], [
            '$(B)/yql/essentials/tools/udf_probe/udf_probe', '$(B)/udf/libnamed.so',
        ])
        self.assertEqual([cmd['cmd_args'][1] for cmd in link['cmds'][-4:-1]], [
            '--only-keep-debug', '--strip-debug', '--remove-section=.gnu_debuglink',
        ])


if __name__ == '__main__':
    unittest.main(verbosity=2)
