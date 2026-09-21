"""Opt-in socket-only PG17 semantics; NO hosted/provider-baseline proof.

Production SQL is unchanged except managed-provider guard calls are removed by
local-only mocks: a stock PG installation cannot emulate Supabase services.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import expanded_rehearsal as v2
import hosted_rehearsal as h

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / '.sparc-local/supabase-cli-v2.117.0/apps/cli-go/pkg/migration/scripts'


@unittest.skipUnless(os.environ.get('SPARC_TEST_PG17_BIN'), 'opt-in local PostgreSQL 17 test')
class ExpandedPostgresTest(unittest.TestCase):
    def test_extension_negative_mutations_and_encrypted_native_roundtrip(self):
        binary = Path(os.environ['SPARC_TEST_PG17_BIN']).resolve()
        with tempfile.TemporaryDirectory(prefix='sparc-v2-pg17-') as directory:
            root = Path(directory)
            env = dict(PATH='/usr/bin:/bin', HOME=directory, LC_ALL='C', TZ='UTC',
                       PGHOST=directory, PGPORT='6549', PGUSER='supabase_admin',
                       PGDATABASE='postgres', PGCONNECT_TIMEOUT='2', PGOPTIONS='-c timezone=UTC')

            def run(tool, *args, sql=None):
                return subprocess.run([str(binary / tool), *args], input=sql,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                      env=env, cwd=root, timeout=30)

            def query(sql, success=True):
                result = run('psql', '-X', '-w', '-qAt', '-v', 'ON_ERROR_STOP=1', '-f', '-',
                             sql=sql.encode() if isinstance(sql, str) else sql)
                self.assertEqual(result.returncode == 0, success,
                                 'local-only SQL status: ' + result.stderr.decode()[-2000:])
                return result.stdout

            self.assertRegex(run('postgres', '--version').stdout, rb'PostgreSQL\) 17\.')
            self.assertEqual(run('initdb', '-D', str(root / 'data'), '-U', 'supabase_admin',
                                 '-A', 'trust', '--encoding=UTF8', '--no-locale').returncode, 0)
            with (root / 'server.log').open('wb') as log:
                server = subprocess.Popen([str(binary / 'postgres'), '-D', str(root / 'data'),
                                           '-k', directory, '-h', '', '-p', '6549'],
                                          env=env, cwd=root, stdin=subprocess.DEVNULL,
                                          stdout=log, stderr=log)
                try:
                    for _ in range(50):
                        if run('pg_isready', '-q').returncode == 0:
                            break
                        self.assertIsNone(server.poll())
                        time.sleep(.1)
                    else:
                        self.fail('local server readiness timeout')
                    query('CREATE ROLE postgres LOGIN CREATEROLE BYPASSRLS; '
                          'ALTER DATABASE postgres OWNER TO postgres; '
                          'CREATE ROLE authenticated; CREATE ROLE anon; '
                          'GRANT authenticated TO postgres; '
                          'GRANT SET ON PARAMETER session_replication_role TO postgres;')
                    env['PGUSER'] = 'postgres'
                    query('BEGIN;' + h.SEED + h.assertions() + 'COMMIT;')
                    # Provider guards remain strict in production; stock local PG
                    # proves fixture semantics only, not service drift detection.
                    with patch.object(h, 'guard', return_value=''), patch.object(v2, 'guard', return_value=''):
                        # Source refuses old-fixture collisions before adding anything.
                        query('CREATE ROLE sparc_rehearsal_reader;')
                        query(v2.source_extension_sql(), success=False)
                        query('DROP ROLE sparc_rehearsal_reader; CREATE SCHEMA supabase_migrations;')
                        query(v2.source_extension_sql(), success=False)
                        self.assertEqual(query("SELECT count(*) FROM pg_roles WHERE rolname LIKE 'sparc_rehearsal_%';").strip(), b'0')
                        query('DROP SCHEMA supabase_migrations RESTRICT;')
                        # Failure while the temporary fourth membership is active
                        # rolls back ALL extension objects/history/roles.
                        failing_seed = v2.source_extension_sql().replace(v2.PROBE_DISABLE, 'SELECT 1/0;' + v2.PROBE_DISABLE)
                        query(failing_seed, success=False)
                        query('BEGIN READ ONLY;' + h.assertions() + 'ROLLBACK;')
                        self.assertEqual(query("SELECT count(*) FROM pg_roles WHERE rolname LIKE 'sparc_rehearsal_%';").strip(), b'0')
                        query(v2.source_extension_sql())
                        query(v2.readonly_sql())
                        query('BEGIN;' + v2.PROBE_ENABLE + 'SELECT 1/0; COMMIT;', success=False)
                        query(v2.readonly_sql())
                        self.assertEqual(query("SELECT count(*) FROM pg_auth_members WHERE roleid IN ('sparc_rehearsal_reader'::regrole,'sparc_rehearsal_member'::regrole);").strip(), b'3')
                        # A column grant bypasses table-level privilege checks.
                        # Prove real member UPDATE access, then roll everything back.
                        query("BEGIN; GRANT UPDATE(label) ON sparc_rehearsal.recovery_cases TO PUBLIC;"
                              + v2.PROBE_ENABLE
                              + "SET LOCAL ROLE sparc_rehearsal_member; "
                                "WITH changed AS (UPDATE sparc_rehearsal.recovery_cases SET label=label "
                                "WHERE id=1 RETURNING id) SELECT 1/((count(*)=1)::int) FROM changed; "
                                "RESET ROLE; ROLLBACK;")
                        query(v2.readonly_sql())
                        for mutation in [
                            'GRANT UPDATE(label) ON sparc_rehearsal.recovery_cases TO PUBLIC;',
                            'GRANT SELECT(body) ON sparc_rehearsal.notes TO PUBLIC;',
                            'GRANT SELECT(version) ON supabase_migrations.schema_migrations TO PUBLIC;',
                            "ALTER TYPE sparc_rehearsal.note_state ADD VALUE 'other';",
                            "CREATE OR REPLACE FUNCTION sparc_rehearsal.label_length(text) RETURNS integer LANGUAGE sql IMMUTABLE STRICT AS 'SELECT 99';",
                            'DROP INDEX sparc_rehearsal.recovery_cases_published;',
                            "DROP INDEX sparc_rehearsal.recovery_cases_published; CREATE INDEX recovery_cases_published ON sparc_rehearsal.recovery_cases(id) WHERE state='draft';",
                            'ALTER VIEW sparc_rehearsal.published_cases SET (security_invoker=false);',
                            'REVOKE SELECT ON sparc_rehearsal.recovery_cases FROM postgres;',
                            'ALTER ROLE sparc_rehearsal_member CREATEDB;',
                            "ALTER POLICY owner_read ON sparc_rehearsal.notes USING (true);",
                            'GRANT INSERT ON sparc_rehearsal.recovery_cases TO sparc_rehearsal_reader;',
                            'GRANT sparc_rehearsal_reader TO sparc_rehearsal_member WITH ADMIN TRUE;',
                            "UPDATE supabase_migrations.schema_migrations SET name='changed';",
                            'CREATE TABLE supabase_migrations.seed_files(path text PRIMARY KEY,hash text NOT NULL);',
                            'ALTER DEFAULT PRIVILEGES IN SCHEMA sparc_rehearsal GRANT INSERT ON TABLES TO sparc_rehearsal_reader;',
                        ]:
                            with self.subTest(mutation=mutation):
                                query('BEGIN; SET LOCAL search_path=pg_catalog;' + mutation + v2.assertions() + 'ROLLBACK;', success=False)
                                query(v2.readonly_sql())
                        query('BEGIN; SET LOCAL search_path=pg_catalog;' + v2.PROBE + v2.assertions() + 'ROLLBACK;')
                        # Execute the real pinned scripts and real encrypted archive
                        # engine through a local-only sanitized transport adapter.
                        original_run = h.run
                        def local_run(argv, work, cfg=None, sql=None):
                            if cfg is None:
                                return original_run(argv, work, sql=sql)
                            result = subprocess.run(argv, input=sql.encode() if isinstance(sql, str) else sql,
                                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                                    env=env, cwd=work, timeout=60, preexec_fn=h.child_limit)
                            self.assertEqual(result.returncode, 0, result.stderr.decode()[-2000:])
                            self.assertLess(len(result.stdout), h.LIMIT)
                            return result.stdout
                        identity = root / 'identity'
                        sparc = ROOT / 'target/debug/sparc'
                        recipient = original_run([str(sparc), 'keygen', str(identity)], root).decode().strip()
                        cfg = dict(project_ref='a'*20, pg_bin=str(binary), sparc=str(sparc))
                        capture_work = root / 'capture'
                        capture_work.mkdir(mode=0o700)
                        with patch.object(h, 'run', side_effect=local_run):
                            v2.capture(cfg, capture_work, SCRIPTS, root / 'archive', recipient, identity)
                            # Reuse this disposable local database (not hosted
                            # isolation proof); remove ONLY local fixture
                            # objects/roles before exercising destination creation.
                            query('DROP VIEW sparc_rehearsal.published_cases RESTRICT; '
                                  'DROP FUNCTION sparc_rehearsal.label_length(text) RESTRICT; '
                                  'DROP TABLE sparc_rehearsal.recovery_cases RESTRICT; '
                                  'DROP TYPE sparc_rehearsal.note_state RESTRICT; '
                                  'DROP TABLE sparc_rehearsal.notes RESTRICT; '
                                  'DROP TABLE sparc_rehearsal.owners RESTRICT; '
                                  'DROP TABLE sparc_rehearsal.empty_table RESTRICT; '
                                  'ALTER DEFAULT PRIVILEGES IN SCHEMA sparc_rehearsal REVOKE SELECT ON TABLES FROM sparc_rehearsal_reader; '
                                  'DROP SCHEMA sparc_rehearsal RESTRICT; '
                                  'DROP TABLE supabase_migrations.schema_migrations RESTRICT; '
                                  'DROP SCHEMA supabase_migrations RESTRICT; '
                                  'DROP ROLE sparc_rehearsal_member; DROP ROLE sparc_rehearsal_reader;')
                            restore_work = root / 'restore'
                            restore_work.mkdir(mode=0o700)
                            v2.restore(dict(cfg, project_ref='b'*20), restore_work,
                                       root / 'archive', identity, root / 'fence')
                            query(v2.readonly_sql())
                finally:
                    server.terminate()
                    try:
                        server.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        server.kill()
                        server.wait(timeout=5)


if __name__ == '__main__':
    unittest.main()
