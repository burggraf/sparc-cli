"""Opt-in socket-only PG17 proof for literal native schema selection."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
sys.path.insert(0, str(Path(__file__).resolve().parent))
import application_inventory
import native_schema_selection as selection
from destination_permissions_pg_test import cleanup_owned_cluster


def quote_identifier(value):
    return '"' + value.replace('"', '""') + '"'


def quote_literal(value):
    return "'" + value.replace("'", "''") + "'"


@unittest.skipUnless(os.environ.get('SPARC_TEST_PG17_BIN'), 'opt-in local PostgreSQL 17 test')
class NativeSchemaSelectionPostgresTest(unittest.TestCase):
    def test_literal_schema_round_trip_and_pattern_counterexample(self):
        binary = Path(os.environ['SPARC_TEST_PG17_BIN']).resolve()
        self.assertEqual(binary, Path('/opt/homebrew/opt/postgresql@17/bin').resolve())
        directory = tempfile.mkdtemp(prefix='spnss-', dir='/tmp')
        root = Path(directory)
        server = None
        env = {}
        commands = []
        try:
            root.chmod(0o700)
            socket = root / 'socket'
            socket.mkdir(mode=0o700)
            env = {
                'PATH': '/usr/bin:/bin', 'HOME': directory, 'LC_ALL': 'C', 'TZ': 'UTC',
                'PGHOST': str(socket), 'PGPORT': '6557', 'PGUSER': 'bootstrap',
                'PGDATABASE': 'postgres', 'PGCONNECT_TIMEOUT': '2', 'PGOPTIONS': '-c timezone=UTC',
            }

            def run(tool, *args, sql=None, database=None, ok=True):
                command = [str(binary / tool), *args]
                command_env = dict(env)
                if database is not None:
                    command_env['PGDATABASE'] = database
                commands.append((tool, tuple(args)))
                try:
                    result = subprocess.run(
                        command, input=sql.encode('utf-8') if sql is not None else None,
                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=command_env,
                        cwd=root, timeout=30,
                    )
                except subprocess.TimeoutExpired as error:
                    self.fail('%s timed out: %s' % (tool, (error.stderr or b'')[-2000:].decode(errors='replace')))
                if ok is True:
                    self.assertEqual(result.returncode, 0, '%s failed: %s' %
                                     (tool, result.stderr[-2000:].decode(errors='replace')))
                elif ok is False:
                    self.assertNotEqual(result.returncode, 0, '%s unexpectedly succeeded' % tool)
                self.assertLessEqual(len(result.stdout), 200000)
                self.assertLessEqual(len(result.stderr), 200000)
                return result

            def query(sql, database='postgres'):
                return run('psql', '-X', '-w', '-qAt', '-v', 'ON_ERROR_STOP=1', '-f', '-',
                           sql=sql, database=database).stdout.decode('utf-8')

            for tool in ('pg_dump', 'pg_restore', 'postgres'):
                self.assertRegex(run(tool, '--version').stdout, rb'\(PostgreSQL\) 17\.')
            run('initdb', '-D', str(root / 'data'), '-U', 'bootstrap', '-A', 'trust',
                '--encoding=UTF8', '--no-locale')
            log_path = root / 'server.log'
            with log_path.open('wb') as log:
                server = subprocess.Popen(
                    [str(binary / 'postgres'), '-D', str(root / 'data'), '-k', str(socket),
                     '-p', '6557', '-c', 'listen_addresses='],
                    env=env, cwd=root, stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                )
                for _ in range(50):
                    if run('pg_isready', '-q', ok=None).returncode == 0:
                        break
                    self.assertIsNone(server.poll(), log_path.read_text(errors='replace')[-2000:])
                    time.sleep(.1)
                else:
                    self.fail('local socket-only server readiness timeout: ' +
                              log_path.read_text(errors='replace')[-2000:])

                self.assertEqual(query('SHOW listen_addresses;').strip(), '')
                self.assertEqual(query('SELECT inet_server_addr() IS NULL;').strip(), 't')
                server_version = int(query('SHOW server_version_num;').strip())
                self.assertGreaterEqual(server_version, 170000)
                self.assertLess(server_version, 180000)
                query('CREATE DATABASE literal_source; CREATE DATABASE literal_target; '
                      'CREATE DATABASE wildcard_target;')

                selected = application_inventory.validate_schemas([
                    'app', 'Mixed Case', '"surrounded"', 'quote"inside', '雪', 'dot.name',
                    'back\\slash', 'slash\\"quote', 'star*', 'question?', 'brackets[ab]',
                    'paren(name)', 'pipe|name', 'plus+name', 'dollar$name', 'comma,name',
                    '-option', 'line\nbreak', '雪' * 21,
                ])
                decoys = ['app_extra', 'Mixed Case extra', 'dotXname', 'quoteXinside', 'star-decoy']
                self.assertEqual(len(('雪' * 21).encode('utf-8')), 63)
                markers = {name: 'selected-marker-%02d' % index for index, name in enumerate(selected)}
                source_sql = []
                for name in selected:
                    source_sql += [
                        'CREATE SCHEMA %s' % quote_identifier(name),
                        'CREATE TABLE %s.items (marker text PRIMARY KEY)' % quote_identifier(name),
                        'INSERT INTO %s.items (marker) VALUES (%s)' %
                        (quote_identifier(name), quote_literal(markers[name])),
                    ]
                for name in decoys:
                    source_sql += [
                        'CREATE SCHEMA %s' % quote_identifier(name),
                        'CREATE TABLE %s.items (marker text PRIMARY KEY)' % quote_identifier(name),
                        'INSERT INTO %s.items (marker) VALUES (%s)' %
                        (quote_identifier(name), quote_literal('decoy-marker-' + name)),
                    ]
                query(';\n'.join(source_sql) + ';', database='literal_source')
                query("CREATE TABLE public.preexisting (marker text PRIMARY KEY); "
                      "INSERT INTO public.preexisting VALUES ('preexisting-marker');", database='literal_target')

                def normalized(state):
                    state['schemas'].sort()
                    state['tables'].sort(key=lambda item: (item['schema'], item['name']))
                    state['markers'].sort(key=lambda item: (item['schema'], item['marker']))
                    return state

                def state_sql():
                    marker_rows = [
                        'SELECT %s AS schema, marker FROM %s.items' %
                        (quote_literal(name), quote_identifier(name)) for name in selected
                    ]
                    marker_rows.append("SELECT 'public' AS schema, marker FROM public.preexisting")
                    return '''WITH namespaces AS (
                        SELECT nspname AS name FROM pg_namespace
                        WHERE nspname !~ '^pg_' AND nspname <> 'information_schema'
                    ), tables AS (
                        SELECT n.nspname AS schema, c.relname AS name
                        FROM pg_namespace n JOIN pg_class c ON c.relnamespace = n.oid
                        WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND c.relkind = 'r'
                    ), markers AS (%s)
                    SELECT jsonb_build_object(
                        'schemas', COALESCE((SELECT jsonb_agg(name ORDER BY name) FROM namespaces), '[]'::jsonb),
                        'tables', COALESCE((SELECT jsonb_agg(jsonb_build_object('schema', schema, 'name', name)
                          ORDER BY schema, name) FROM tables), '[]'::jsonb),
                        'markers', COALESCE((SELECT jsonb_agg(jsonb_build_object('schema', schema, 'marker', marker)
                          ORDER BY schema, marker) FROM markers), '[]'::jsonb)
                    )::text;''' % (' UNION ALL '.join(marker_rows))

                archive = root / 'literal.dump'
                selector_args = selection.pg_dump_schema_args(selected)
                self.assertEqual(selector_args[0], '--strict-names')
                run('pg_dump', '-Fc', *selector_args, '-f', str(archive), database='literal_source')
                run('pg_restore', '--single-transaction', '--exit-on-error', '-d', 'literal_target', str(archive))
                observed = normalized(json.loads(query(state_sql(), database='literal_target')))
                expected = normalized({
                    'schemas': ['public', *selected],
                    'tables': [{'schema': 'public', 'name': 'preexisting'}, *[
                        {'schema': name, 'name': 'items'} for name in selected]],
                    'markers': [{'schema': 'public', 'marker': 'preexisting-marker'}, *[
                        {'schema': name, 'marker': markers[name]} for name in selected]],
                })
                self.assertEqual(observed, expected)

                naive_archive = root / 'naive-wildcard.dump'
                run('pg_dump', '-Fc', '--strict-names', '--schema=star*', '-f', str(naive_archive),
                    database='literal_source')
                run('pg_restore', '--single-transaction', '--exit-on-error', '-d', 'wildcard_target',
                    str(naive_archive))
                naive_schemas = json.loads(query('''SELECT COALESCE(jsonb_agg(nspname ORDER BY nspname), '[]'::jsonb)::text
                    FROM pg_namespace WHERE nspname !~ '^pg_' AND nspname <> 'information_schema';''',
                                                   database='wildcard_target'))
                self.assertIn('star*', naive_schemas)
                self.assertIn('star-decoy', naive_schemas)

                strict_archive = root / 'strict-failure.dump'
                run('pg_dump', '-Fc', *selection.pg_dump_schema_args([selected[0], 'missing_schema']),
                    '-f', str(strict_archive), database='literal_source', ok=False)
                failure_index = len(commands)
                self.assertFalse(any(tool == 'pg_restore' for tool, _args in commands[failure_index:]))
        finally:
            if not cleanup_owned_cluster(binary, root, server, env):
                self.fail('owned cluster shutdown unconfirmed; private root quarantined')


if __name__ == '__main__':
    unittest.main()
