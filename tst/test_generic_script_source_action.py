import base64
import unittest

import lib
from test_python_srcs import python_base
from test_python2 import python2_base

MODULE='grove/checks'
BASE=lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\n'
CASES=[('peer',),('local',),('direct',),('physical',),('py_first',),('producer_last',),('no_srcdir',),('py2',),('peer_first',),('consumer_first',),('chunked',),('no_srcdir_plain',)]
def fixture(mode):
 files=python2_base() if mode=='py2' else python_base()
 for path in ('library/cpp/resource','library/cpp/malloc/jemalloc'):
  files.setdefault(path+'/ya.make','LIBRARY()\n'+lib.BARE_MODULE+'END()\n')
 lib.tool_program(files,'tools/pack','pack')
 files['grove/pack/ya.make']='GEN_LIBRARY()\nRUN_PROGRAM(tools/pack IN seed.txt OUT_NOAUTO payload.tar)\nEND()\n'
 files['grove/pack/seed.txt']='graph-only tar producer seed\n'
 files['taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make']=lib.resource_lib('USRV_UNPACK_TAR',1)
 for p in ('taxi','taxi/uservices','taxi/uservices/arc-gen/unpack-tar/usrv-build-lib'):files[p+'/linters.make.inc']='# native fixture no project lints\n'
 generated=[f'plugin{i}.py' for i in range(12 if mode=='chunked' else 2)]
 physical=[f'local{i}.py' for i in range(11 if mode=='chunked' else 2)]
 for n in physical:files[MODULE+'/'+n]='# physical sibling\n'
 outs=['config.json']+generated
 if mode=='local':
  producer='RUN_PROGRAM(tools/pack IN seed.txt OUT_NOAUTO payload.tar)\nUSRV_BUILD(FROM ${ARCADIA_BUILD_ROOT}/grove/checks/payload.tar '+' '.join(outs)+')\n'
 elif mode=='direct':producer='RUN_PROGRAM(tools/pack IN seed.txt OUT_NOAUTO '+' '.join(outs)+')\n'
 elif mode=='physical':
  producer=''
  for n in outs:files[MODULE+'/'+n]='# physical control\n'
 else:producer='PEERDIR(grove/pack)\nUSRV_BUILD(FROM ${ARCADIA_BUILD_ROOT}/grove/pack/payload.tar '+' '.join(outs)+')\n'
 resource='RESOURCE(config.json fixture/data)\n'
 paths=generated+physical
 srcdir='SRCDIR(${ARCADIA_BUILD_ROOT}/grove/checks)\n'
 if mode=='no_srcdir':
  srcdir='';paths=['${ARCADIA_BUILD_ROOT}/grove/checks/'+n for n in generated]+physical
 if mode in ('physical','no_srcdir_plain'):srcdir=''
 py='PY_SRCS('+' '.join(paths)+')\n'
 source=(py+resource) if mode=='py_first' else (resource+py)
 body=(source+producer) if mode=='producer_last' else (producer+source)
 files[MODULE+'/ya.make']=('PY2_LIBRARY' if mode=='py2' else 'PY3_LIBRARY')+'()\n'+BASE+srcdir+body+'END()\n'
 files[MODULE+'/seed.txt']='graph-only direct input\n'
 roots=['grove/pack',MODULE] if mode=='peer_first' else [MODULE,'grove/pack'] if mode=='consumer_first' else [MODULE]
 return files,roots

def observations(g):
 nodes=g['graph'];byoutput={o:n for n in nodes for o in n.get('outputs',[])}
 ar=next(n for n in nodes if any(o.startswith(('$(B)/'+MODULE+'/','$(BUILD_ROOT)/'+MODULE+'/')) and o.endswith('.global.a') for o in n.get('outputs',[])))
 args=ar['cmds'][0]['cmd_args'];members=[x for x in args if x.endswith('.o')]
 roles=[]
 for member in members:
  target=member if member.startswith('$(') else '$(B)/'+member
  n=byoutput.get(target) or byoutput.get(target.replace('$(B)','$(BUILD_ROOT)'))
  assert n,(target,ar['outputs'])
  a=n['cmds'][0]['cmd_args']
  if 'fixture/data' in a or base64.b64encode(b'fixture/data').decode() in a:kind='explicit'
  elif any(x.endswith(('.yapyc','.yapyc3')) for x in n['inputs']):kind='python-files'
  else:kind='namespace'
  roles.append(dict(output=n['outputs'][0],role=kind,uid=n['uid'],inputs=n['inputs']))
 return dict(archive=ar['outputs'],archive_uid=ar['uid'],members=roles)


class GenericScriptSourceActionTest(unittest.TestCase):
    def graph(self, mode, expected):
        files, roots = fixture(mode)
        graph = lib.make(files, roots[-1], *roots[:-1], "--target-platform", "default-linux-x86_64")
        ids = {n['uid'] for n in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertLessEqual(set(graph['result']), ids)
        self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
        obs = observations(graph)
        self.assertEqual([m['role'] for m in obs['members']], expected)
        archive = lib.node_by_output(graph, obs['archive'][0])
        byid = {n['uid']: n for n in graph['graph']}
        for member in obs['members']:
            self.assertIn(member['uid'], archive['deps'])
        explicit = next(byid[m['uid']] for m in obs['members'] if m['role'] == 'explicit')
        config = '$(B)/' + MODULE + '/config.json'
        if mode == 'physical':
            self.assertIn('$(S)/' + MODULE + '/config.json', explicit['inputs'])
        else:
            producer = lib.node_by_output(graph, config)
            self.assertIn(config, explicit['inputs'])
            self.assertIn(producer['uid'], explicit['deps'])
            self.assertEqual(sum(config in n['outputs'] for n in graph['graph']), 1)
            if mode != 'direct':
                tar = '$(B)/' + (MODULE if mode == 'local' else 'grove/pack') + '/payload.tar'
                unpack = producer
                packed = lib.node_by_output(graph, tar)
                self.assertIn(tar, unpack['inputs'])
                self.assertIn(packed['uid'], unpack['deps'])
                for source in ('plugin0.py', 'plugin1.py'):
                    generated = lib.node_by_output(graph, '$(B)/' + MODULE + '/' + source)
                    bytecode = next(n for n in graph['graph'] if n['outputs'][0].endswith(('.yapyc','.yapyc3')) and generated['outputs'][0] in n['inputs'])
                    self.assertIn(generated['uid'], bytecode['deps'])
        return graph

    def test_local_and_peer_tar_actions_precede_dependent_resources(self):
        for mode in ('peer', 'local', 'py_first', 'no_srcdir_plain'):
            with self.subTest(mode=mode):
                self.graph(mode, ['explicit', 'namespace', 'python-files'])

    def test_producer_declared_later_preserves_pending_action_order(self):
        self.graph('producer_last', ['namespace', 'python-files', 'explicit'])

    def test_direct_run_program_and_physical_controls_keep_their_order(self):
        for mode in ('direct', 'physical'):
            with self.subTest(mode=mode):
                self.graph(mode, ['explicit', 'namespace', 'python-files'])

    def test_selected_peer_and_consumer_roots_preserve_source_ownership(self):
        for mode in ('peer_first', 'consumer_first'):
            with self.subTest(mode=mode):
                self.graph(mode, ['explicit', 'namespace', 'python-files'])

    def test_python2_and_chunked_python3_resources_keep_native_member_order(self):
        self.graph('py2', ['explicit', 'python-files'])
        self.graph('chunked', ['explicit', 'namespace'] + ['python-files'] * 4)


if __name__ == '__main__':
    unittest.main()
