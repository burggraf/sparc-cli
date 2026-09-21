"""Opt-in disposable PG17 test for read-only application inventory."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import application_inventory as inventory


@unittest.skipUnless(os.environ.get('SPARC_TEST_PG17_BIN'), 'opt-in local PostgreSQL 17 test')
class ApplicationInventoryPostgresTest(unittest.TestCase):
    def test_catalog_inventory_is_literal_read_only_and_never_ready(self):
        binary = Path(os.environ['SPARC_TEST_PG17_BIN']).resolve()
        with tempfile.TemporaryDirectory(prefix='sparc-inventory-pg17-') as directory:
            root = Path(directory)
            root.chmod(0o700)
            env = {'PATH': '/usr/bin:/bin', 'HOME': directory, 'LC_ALL': 'C', 'TZ': 'UTC',
                   'PGHOST': directory, 'PGPORT': '6549', 'PGUSER': 'bootstrap',
                   'PGDATABASE': 'postgres', 'PGCONNECT_TIMEOUT': '2'}

            def run(tool, *args, sql=None):
                return subprocess.run([str(binary / tool), *args], input=sql.encode() if sql else None,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, cwd=root,
                                      timeout=30)

            def query(sql):
                result = run('psql', '-X', '-w', '-qAt', '-v', 'ON_ERROR_STOP=1', '-f', '-', sql=sql)
                self.assertEqual(result.returncode, 0, result.stderr.decode()[-1000:])
                return result.stdout

            self.assertRegex(run('postgres', '--version').stdout, rb'PostgreSQL\) 17\.')
            self.assertEqual(run('initdb', '-D', str(root / 'data'), '-U', 'bootstrap', '-A', 'trust',
                                 '--encoding=UTF8', '--no-locale').returncode, 0)
            with (root / 'server.log').open('wb') as log:
                server = subprocess.Popen([str(binary / 'postgres'), '-D', str(root / 'data'), '-k', directory,
                                           '-h', '', '-p', '6549'], env=env, cwd=root, stdin=subprocess.DEVNULL,
                                          stdout=log, stderr=log)
                try:
                    for _ in range(50):
                        if run('pg_isready', '-q').returncode == 0:
                            break
                        self.assertIsNone(server.poll())
                        time.sleep(.1)
                    else:
                        self.fail('local server readiness timeout')
                    schema = 'app_*"雪'
                    query('''CREATE ROLE postgres LOGIN CREATEROLE; CREATE ROLE inventory_reader;
                        CREATE SCHEMA owned AUTHORIZATION inventory_reader;
                        CREATE SCHEMA "app_*""雪"; CREATE SCHEMA outside;
                        CREATE SCHEMA "a b"; CREATE SCHEMA """a b""";
                        CREATE EXTENSION hstore WITH SCHEMA "app_*""雪";
                        ALTER EXTENSION hstore ADD SCHEMA "a b";
                        CREATE FUNCTION "a b".extension_probe() RETURNS integer LANGUAGE sql AS 'SELECT 1';
                        ALTER EXTENSION hstore ADD FUNCTION "a b".extension_probe();
                        CREATE EXTENSION file_fdw;
                        CREATE SERVER inventory_file FOREIGN DATA WRAPPER file_fdw;
                        CREATE FOREIGN TABLE "app_*""雪".foreign_items (id integer) SERVER inventory_file OPTIONS (filename '/dev/null', format 'csv');
                        GRANT USAGE ON FOREIGN SERVER inventory_file TO postgres;
                        GRANT USAGE, CREATE ON SCHEMA "app_*""雪" TO postgres;
                        GRANT USAGE, CREATE ON SCHEMA outside TO postgres;
                        ALTER DATABASE postgres OWNER TO postgres;''')
                    env['PGUSER'] = 'postgres'
                    self.assertEqual(query("SELECT '4294967295'::oid::bigint;").strip(), b'4294967295')
                    query('''CREATE TYPE "app_*""雪".state AS ENUM ('draft','published');
                        CREATE TABLE outside.parent (id integer PRIMARY KEY);
                        CREATE TABLE "app_*""雪".items (id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                          parent_id integer REFERENCES outside.parent(id), note text DEFAULT 'not-a-secret',
                          state "app_*""雪".state NOT NULL);
                        CREATE UNLOGGED TABLE "app_*""雪".scratch (id integer);
                        CREATE TABLE "app_*""雪".parted (id integer) PARTITION BY RANGE (id);
                        CREATE TABLE "app_*""雪".parted_1 PARTITION OF "app_*""雪".parted FOR VALUES FROM (0) TO (10);
                        CREATE DOMAIN "app_*""雪".domain_one AS integer CONSTRAINT same_check CHECK (VALUE > 0);
                        CREATE DOMAIN "app_*""雪".domain_two AS integer CONSTRAINT same_check CHECK (VALUE > 0);
                        ALTER TABLE "app_*""雪".items ENABLE ROW LEVEL SECURITY;
                        CREATE POLICY item_read ON "app_*""雪".items FOR SELECT TO inventory_reader USING (true);
                        GRANT SELECT (note) ON "app_*""雪".items TO inventory_reader;
                        CREATE FUNCTION "app_*""雪".secret_trigger() RETURNS trigger LANGUAGE plpgsql AS
                          $$ BEGIN PERFORM 'INVENTORY-SECRET-CANARY'; RETURN NEW; END $$;
                        CREATE TRIGGER item_change BEFORE INSERT ON "app_*""雪".items
                          FOR EACH ROW EXECUTE FUNCTION "app_*""雪".secret_trigger();
                        CREATE MATERIALIZED VIEW "app_*""雪".item_view AS SELECT id FROM "app_*""雪".items;
                        ALTER DEFAULT PRIVILEGES IN SCHEMA "app_*""雪" GRANT SELECT ON TABLES TO inventory_reader;''')

                    def local_psql(_cfg, _work, sql):
                        return query(sql)

                    with patch.object(inventory.h, 'versions') as versions, patch.object(inventory.h, 'psql', side_effect=local_psql):
                        result = inventory.inspect({}, root, [schema, 'owned'])
                        versions.assert_called_once()
                    rendered = str(result)
                    self.assertEqual(result['selected_schemas'], [schema, 'owned'])
                    self.assertNotIn('outside', [item['schema'] for item in result['inventory']['relations']])
                    self.assertNotIn('INVENTORY-SECRET-CANARY', rendered)
                    self.assertFalse(result['execution_supported'])
                    self.assertFalse(result['export_ready'])
                    self.assertFalse(result['restore_verified'])
                    self.assertFalse(result['dependency_analysis_complete'])
                    codes = {finding['code'] for finding in result['findings']}
                    self.assertTrue(any(f['code'] == 'non-postgres-ownership' and f['object'] == {'kind': 'schema', 'name': 'owned'} for f in result['findings']))
                    self.assertTrue({'external-schema-fk', 'column-acl', 'rls-policy-review',
                                     'noninternal-trigger', 'routine-review', 'custom-type-review',
                                     'unlogged-relation', 'materialized-view', 'default-privilege-review',
                                     'foreign-table', 'partitioning', 'extension-owned-object',
                                     'non-postgres-ownership'} <= codes)
                    domains = [x for x in result['inventory']['constraints'] if x['domain'] in ('domain_one', 'domain_two')]
                    self.assertEqual({x['domain'] for x in domains}, {'domain_one', 'domain_two'})
                    extension_kinds = {x['object_kind'] for x in result['inventory']['extensions']}
                    self.assertIn('operator', extension_kinds)
                    extension_functions = [x['identity'] for x in result['inventory']['extensions'] if x['object_kind'] == 'function']
                    self.assertGreater(len(extension_functions), 1)
                    self.assertEqual(len(extension_functions), len(set(extension_functions)))
                    first, second = 'a b', '"a b"'
                    with patch.object(inventory.h, 'versions'), patch.object(inventory.h, 'psql', side_effect=local_psql):
                        both = inventory.inspect({}, root, [first, second])
                        only_first = inventory.inspect({}, root, [first])
                        only_second = inventory.inspect({}, root, [second])
                    probe_identity = '"a b".extension_probe()'
                    for observed in (both, only_first):
                        self.assertTrue(any(x['object_kind'] == 'schema' and x['schema'] == first
                                            for x in observed['inventory']['extensions']))
                        self.assertTrue(any(x['object_kind'] == 'function' and x['schema'] == first
                                            and x['identity'] == probe_identity
                                            for x in observed['inventory']['extensions']))
                    self.assertFalse(any(x['identity'] == probe_identity for x in only_second['inventory']['extensions']))
                    correct_sql = inventory.inspection_sql
                    def mixed_display_sql(schemas):
                        return correct_sql(schemas).replace(
                            "(ns.oid IS NOT NULL AND sn.oid=ns.oid) OR (ns.oid IS NULL AND x.schema=quote_ident(sn.nspname))",
                            "sn.oid=ns.oid OR x.schema=sn.nspname OR x.schema=quote_ident(sn.nspname)")
                    with patch.object(inventory, 'inspection_sql', side_effect=mixed_display_sql), \
                         patch.object(inventory.h, 'versions'), patch.object(inventory.h, 'psql', side_effect=local_psql):
                        mixed_both = inventory.inspect({}, root, [first, second])
                        mixed_second = inventory.inspect({}, root, [second])
                    self.assertTrue(any(x['schema'] == second and x['identity'] == probe_identity
                                        for x in mixed_both['inventory']['extensions']))
                    self.assertTrue(any(x['schema'] == second and x['identity'] == probe_identity
                                        for x in mixed_second['inventory']['extensions']))
                    with patch.object(inventory.h, 'versions'), patch.object(inventory.h, 'psql', side_effect=local_psql):
                        with self.assertRaises(inventory.InspectionError):
                            inventory.inspect({}, root, ['missing_schema'])
                finally:
                    server.terminate()
                    try:
                        server.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        server.kill()
                        server.wait(timeout=5)


if __name__ == '__main__':
    unittest.main()
