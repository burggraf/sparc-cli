"""Opt-in owned socket-only PG17 coverage for direct dependency observation."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tests'))
import application_dependencies as dependencies
from destination_permissions_pg_test import cleanup_owned_cluster


@unittest.skipUnless(os.environ.get('SPARC_TEST_PG17_BIN'), 'opt-in local PostgreSQL 17 test')
class ApplicationDependencyPostgresTests(unittest.TestCase):
    def test_direct_catalog_edges_are_literal_read_only_and_bounded(self):
        binary = Path(os.environ['SPARC_TEST_PG17_BIN']).resolve()
        self.assertEqual(binary, Path('/opt/homebrew/opt/postgresql@17/bin').resolve())
        root = Path(tempfile.mkdtemp(prefix='sparc-dependencies-', dir='/tmp')); root.chmod(0o700)
        socket = root / 'socket'; socket.mkdir(mode=0o700)
        env = {'PATH':'/usr/bin:/bin','HOME':str(root),'LC_ALL':'C','TZ':'UTC','PGHOST':str(socket),'PGPORT':'6561',
               'PGUSER':'bootstrap','PGDATABASE':'postgres','PGCONNECT_TIMEOUT':'2','PGOPTIONS':'-c timezone=UTC'}
        server = None
        try:
            def run(tool, *args, sql=None, ok=True):
                result = subprocess.run([str(binary / tool), *args], input=sql.encode() if sql else None,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=root, env=env, timeout=30)
                if ok: self.assertEqual(result.returncode, 0, result.stderr.decode(errors='replace')[-2000:])
                return result
            def query(sql): return run('psql', '-X','-w','-qAt','-v','ON_ERROR_STOP=1','-f','-',sql=sql).stdout
            self.assertRegex(run('postgres','--version').stdout, rb'PostgreSQL\) 17\.')
            run('initdb','-D',str(root/'data'),'-U','bootstrap','-A','trust','--encoding=UTF8','--no-locale')
            with (root/'server.log').open('wb') as log:
                server = subprocess.Popen([str(binary/'postgres'),'-D',str(root/'data'),'-k',str(socket),'-h','','-p','6561'], cwd=root, env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
            for _ in range(50):
                if run('pg_isready','-q',ok=False).returncode == 0: break
                self.assertIsNone(server.poll()); time.sleep(.1)
            else: self.fail('owned local socket-only PG17 readiness timeout')
            query('''CREATE SCHEMA "a b"; CREATE SCHEMA """a b"""; CREATE SCHEMA auth; CREATE EXTENSION hstore;
              CREATE FUNCTION pg_catalog.sparc_hstore_probe() RETURNS integer LANGUAGE sql RETURN 1;
              ALTER EXTENSION hstore ADD FUNCTION pg_catalog.sparc_hstore_probe();
              CREATE TYPE """a b""".state AS ENUM ('one'); CREATE TYPE """a b""".pair AS (left_value integer, right_value integer);
              CREATE DOMAIN """a b""".positive AS integer CHECK (VALUE>0); CREATE DOMAIN """a b"""._positive AS integer CHECK (VALUE>0);
              CREATE SEQUENCE """a b""".numbers; CREATE TABLE """a b""".parent(id integer PRIMARY KEY);
              CREATE TABLE auth.users(id integer PRIMARY KEY); CREATE FUNCTION """a b""".helper() RETURNS boolean LANGUAGE sql RETURN true;
              CREATE TABLE "a b".items(id integer PRIMARY KEY, parent_id integer CONSTRAINT same_fk REFERENCES """a b""".parent(id), auth_id integer REFERENCES auth.users(id),
                state """a b""".state, checked """a b""".positive, underscored """a b"""._positive, paired """a b""".pair, n integer DEFAULT nextval('"""a b""".numbers'), note text DEFAULT 'EXPRESSION-CANARY');
              CREATE TABLE "a b".items_two(id integer PRIMARY KEY, parent_id integer CONSTRAINT same_fk REFERENCES """a b""".parent(id));
              INSERT INTO "a b".items(id,n,note) VALUES(1,1,'ROW-CANARY');
              CREATE VIEW "a b".v AS SELECT id FROM """a b""".parent; CREATE VIEW "a b".v_ctid AS SELECT ctid FROM """a b""".parent;
              CREATE FUNCTION "a b".parsed() RETURNS integer LANGUAGE sql RETURN (SELECT count(*)::integer FROM """a b""".parent);
              CREATE FUNCTION "a b".parsed(value integer) RETURNS integer LANGUAGE sql RETURN (SELECT count(*)::integer FROM """a b""".parent WHERE id=value);
              CREATE FUNCTION "a b".catalog_extension_ref() RETURNS integer LANGUAGE sql RETURN pg_catalog.sparc_hstore_probe();
              CREATE FUNCTION "a b".string_body() RETURNS integer LANGUAGE sql AS $$SELECT count(*)::integer FROM """a b""".parent$$;
              ALTER TABLE "a b".items ENABLE ROW LEVEL SECURITY;
              CREATE POLICY p ON "a b".items USING ("""a b""".helper());
              CREATE FUNCTION """a b""".trig() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW; END$$;
              CREATE TRIGGER t BEFORE INSERT ON "a b".items FOR EACH ROW EXECUTE FUNCTION """a b""".trig();
              CREATE TABLE """a b""".parted(id integer) PARTITION BY RANGE(id); CREATE INDEX parted_idx ON """a b""".parted(id);
              CREATE TABLE "a b".part PARTITION OF """a b""".parted FOR VALUES FROM (0) TO (10);
              CREATE TABLE "a b".hstores(value hstore); ALTER EXTENSION hstore ADD TABLE "a b".hstores;
              CREATE EXTENSION file_fdw; CREATE SERVER option_canary FOREIGN DATA WRAPPER file_fdw;
              CREATE FOREIGN TABLE "a b".option_items(value text) SERVER option_canary OPTIONS (filename 'OPTION-CANARY', format 'csv');''')
            def snapshot():
                return query('''SELECT md5(
                  (SELECT coalesce(string_agg(note, ',' ORDER BY id),'') FROM "a b".items) ||
                  (SELECT coalesce(string_agg(classid::text || ':' || objid::text || ':' || objsubid::text || ':' || refclassid::text || ':' || refobjid::text || ':' || refobjsubid::text || ':' || deptype::text, ',' ORDER BY classid,objid,objsubid,refclassid,refobjid,refobjsubid,deptype), '') FROM pg_depend) ||
                  (SELECT coalesce(string_agg(c.relname || ':' || c.relkind::text || ':' || coalesce(f.srvname,'') || ':' || coalesce(ft.ftoptions::text,''), ',' ORDER BY c.oid), '') FROM pg_class c LEFT JOIN pg_foreign_table ft ON ft.ftrelid=c.oid LEFT JOIN pg_foreign_server f ON f.oid=ft.ftserver WHERE c.relnamespace IN (SELECT oid FROM pg_namespace WHERE nspname IN ('a b','"a b"'))) ||
                  (SELECT coalesce(string_agg(srvname || ':' || coalesce(srvoptions::text,''), ',' ORDER BY srvname), '') FROM pg_foreign_server));''')
            internal_trigger = json.loads(query('''SELECT json_build_object('name',t.tgname,'constraint_schema',n.nspname,'constraint_name',k.conname,'owner_kind','relation','owner',r.relname)
              FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_depend d ON d.classid='pg_trigger'::regclass AND d.objid=t.oid
              JOIN pg_constraint k ON d.refclassid='pg_constraint'::regclass AND k.oid=d.refobjid JOIN pg_namespace n ON n.oid=k.connamespace JOIN pg_class r ON r.oid=k.conrelid
              WHERE c.relnamespace=(SELECT oid FROM pg_namespace WHERE nspname='a b') AND c.relname='items' AND t.tgisinternal ORDER BY t.tgname LIMIT 1;'''))
            def local_psql(_cfg, _work, sql): return query(sql)
            before = snapshot()
            with patch.object(dependencies.h, 'versions'), patch.object(dependencies.h, 'psql', side_effect=local_psql):
                one = dependencies.observe({}, root, ['a b'])
                both = dependencies.observe({}, root, ['a b','"a b"'])
            self.assertEqual(before, snapshot())
            self.assertEqual(one['response']['transaction'], {'read_only':'on','isolation':'repeatable read','search_path':'pg_catalog'})
            rendered = str(one); self.assertNotIn('ROW-CANARY', rendered); self.assertNotIn('EXPRESSION-CANARY', rendered); self.assertNotIn('SELECT count', rendered); self.assertNotIn('OPTION-CANARY', rendered)
            edges = one['response']['edges']
            def exact(**identity):
                matches = [item for item in edges if all(item[key] == value for key, value in identity.items())]
                self.assertEqual(len(matches), 1, (identity, [item for item in edges if item['source_kind'] == identity.get('source_kind')]))
                return matches[0]
            fk_one = exact(source_kind='constraint', source_schema='a b', source_name='same_fk', source_owner_kind='relation', source_owner='items', target_kind='column', target_schema='"a b"', target_name='parent', target_subname='id', dependency_type='n')
            fk_two = exact(source_kind='constraint', source_schema='a b', source_name='same_fk', source_owner_kind='relation', source_owner='items_two', target_kind='column', target_schema='"a b"', target_name='parent', target_subname='id', dependency_type='n')
            enum_edge = exact(source_kind='column', source_schema='a b', source_name='items', source_subname='state', target_kind='type', target_schema='"a b"', target_name='state', dependency_type='n')
            domain_edge = exact(source_kind='column', source_schema='a b', source_name='items', source_subname='checked', target_kind='type', target_schema='"a b"', target_name='positive', dependency_type='n')
            exact(source_kind='column', source_schema='a b', source_name='items', source_subname='underscored', target_kind='type', target_schema='"a b"', target_name='_positive', dependency_type='n')
            exact(source_kind='column', source_schema='a b', source_name='items', source_subname='paired', target_kind='type', target_schema='"a b"', target_name='pair', dependency_type='n')
            exact(source_kind='default', source_schema='a b', source_name='items', source_subname='n', target_kind='relation', target_schema='"a b"', target_name='numbers', dependency_type='n')
            exact(source_kind='rewrite', source_schema='a b', source_name='v', source_subname='_RETURN', target_kind='column', target_schema='"a b"', target_name='parent', target_subname='id', dependency_type='n')
            exact(source_kind='rewrite', source_schema='a b', source_name='v_ctid', source_subname='_RETURN', target_kind='column', target_schema='"a b"', target_name='parent', target_subname='ctid', dependency_type='n')
            parsed_zero = exact(source_kind='routine', source_schema='a b', source_name='parsed', source_arguments='', target_kind='relation', target_schema='"a b"', target_name='parent', dependency_type='n')
            parsed_one = exact(source_kind='routine', source_schema='a b', source_name='parsed', source_arguments='value integer', target_kind='column', target_schema='"a b"', target_name='parent', target_subname='id', dependency_type='n')
            exact(source_kind='policy', source_schema='a b', source_name='items', source_subname='p', target_kind='routine', target_schema='"a b"', target_name='helper', target_arguments='', dependency_type='n')
            exact(source_kind='trigger', source_schema='a b', source_name='items', source_subname='t', target_kind='routine', target_schema='"a b"', target_name='trig', target_arguments='', dependency_type='n')
            exact(source_kind='trigger', source_schema='a b', source_name='items', source_subname=internal_trigger['name'], target_kind='constraint', target_schema=internal_trigger['constraint_schema'], target_name=internal_trigger['constraint_name'], target_owner_kind=internal_trigger['owner_kind'], target_owner=internal_trigger['owner'], dependency_type='i')
            exact(source_kind='relation', source_schema='a b', source_name='part', target_kind='relation', target_schema='"a b"', target_name='parted', dependency_type='inheritance')
            exact(source_kind='relation', source_schema='a b', source_name='part_id_idx', target_kind='relation', target_schema='"a b"', target_name='parted_idx', dependency_type='P')
            exact(source_kind='relation', source_schema='a b', source_name='part_id_idx', target_kind='relation', target_schema='a b', target_name='part', dependency_type='S')
            exact(source_kind='constraint', source_schema='a b', source_owner='items', target_kind='column', target_schema='auth', target_name='users', target_subname='id', dependency_type='n')
            self.assertFalse(any(e['source_kind']=='routine' and e['source_name']=='string_body' and e['target_schema']=='"a b"' for e in edges))
            exact(source_kind='routine', source_schema='a b', source_name='catalog_extension_ref', source_arguments='', target_kind='routine', target_schema='pg_catalog', target_name='sparc_hstore_probe', target_arguments='', target_extension='hstore', dependency_type='n')
            self.assertTrue(any(m['side']=='source' and m['schema']=='a b' and m['name']=='hstores' and m['extension']=='hstore' for m in one['response']['memberships']))
            self.assertTrue(any(e['target_kind']=='unresolved' and e['target_local_class_id'] is not None for e in edges))
            self.assertIn('protected-external', {f['code'] for f in one['findings']})
            self.assertTrue(any(f['code']=='external' for f in one['findings']))
            self.assertTrue(any(f['code']=='extension-requirement' and f.get('edge', {}).get('source_name')=='hstores' for f in one['findings']))
            both_edges = both['response']['edges']
            self.assertTrue(any(e['source_kind']=='type' and e['source_schema']=='"a b"' and e['source_name']=='_positive' for e in both_edges))
            self.assertTrue(any(e['source_kind']=='type' and e['source_schema']=='"a b"' and e['source_name']=='pair' for e in both_edges))
            for known in (fk_one, fk_two, enum_edge, domain_edge, parsed_zero, parsed_one):
                identity = {key: known[key] for key in ('source_kind','source_schema','source_name','source_owner_kind','source_owner','source_subname','source_arguments','target_kind','target_schema','target_name','target_owner_kind','target_owner','target_subname','target_arguments','dependency_type')}
                self.assertTrue(any(f['code']=='external' and all(f['edge'][key] == value for key, value in identity.items()) for f in one['findings'] if 'edge' in f), identity)
                self.assertTrue(any(all(candidate[key] == value for key, value in identity.items()) for candidate in both_edges), identity)
                self.assertTrue(any(f['code']=='internal' and all(f['edge'][key] == value for key, value in identity.items()) for f in both['findings'] if 'edge' in f), identity)
            self.assertFalse(both['dependency_analysis_complete'])
            with patch.object(dependencies.h, 'versions'), patch.object(dependencies.h, 'psql', side_effect=local_psql):
                with self.assertRaises(dependencies.DependencyInspectionError): dependencies.observe({}, root, ['missing schema'])
        finally:
            if not cleanup_owned_cluster(binary, root, server, env): self.fail('owned local cluster shutdown unconfirmed; quarantined temporary directory')


if __name__ == '__main__':
    unittest.main()
