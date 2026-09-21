"""Opt-in synthetic socket-only PG17 test for destination permission observation."""
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import destination_permissions as permissions


def stop_owned_cluster(binary, root, server, env, runner=subprocess.run):
    """Stop only the cluster rooted at ``root``; callers quarantine it if this fails."""
    def control(mode, seconds):
        try:
            result = runner([str(binary / 'pg_ctl'), '-D', str(root / 'data'), '-m', mode,
                             '-w', '-t', str(seconds), 'stop'], stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, env=env, cwd=root, timeout=seconds + 2)
        except (OSError, subprocess.TimeoutExpired):
            return False
        return result.returncode == 0
    stopped = control('fast', 10)
    if not stopped: stopped = control('immediate', 5)
    if not stopped: return False
    try:
        server.wait(timeout=5)
    except subprocess.TimeoutExpired:
        if not control('immediate', 5): return False
        try:
            server.wait(timeout=5)
        except subprocess.TimeoutExpired:
            return False
    try:
        ready = runner([str(binary / 'pg_isready'), '-q'], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                       env=env, cwd=root, timeout=5)
    except (OSError, subprocess.TimeoutExpired):
        return False
    return server.poll() is not None and not (root / 'data' / 'postmaster.pid').exists() and ready.returncode != 0


def cleanup_owned_cluster(binary, root, server, env, runner=subprocess.run):
    """Remove only a known-stopped owned cluster; otherwise leave it quarantined."""
    if server is None or stop_owned_cluster(binary, root, server, env, runner):
        shutil.rmtree(root)
        return True
    return False


@unittest.skipUnless(os.environ.get('SPARC_TEST_PG17_BIN'), 'opt-in local PostgreSQL 17 test')
class DestinationPermissionPostgresTest(unittest.TestCase):
    def test_catalog_contract_refuses_permission_drift_without_reading_application_data(self):
        binary = Path(os.environ['SPARC_TEST_PG17_BIN']).resolve()
        self.assertEqual(binary, Path('/opt/homebrew/opt/postgresql@17/bin').resolve())
        directory = tempfile.mkdtemp(prefix='spdp-', dir='/tmp')
        root = Path(directory)
        server = None
        env = {}
        try:
            root.chmod(0o700)
            socket = root / 'socket'; socket.mkdir(mode=0o700)
            env = {'PATH': '/usr/bin:/bin', 'HOME': directory, 'LC_ALL': 'C', 'TZ': 'UTC',
                   'PGHOST': str(socket), 'PGPORT': '6555', 'PGUSER': 'bootstrap',
                   'PGDATABASE': 'postgres', 'PGCONNECT_TIMEOUT': '2', 'PGOPTIONS': '-c timezone=UTC'}

            def run(tool, *args, sql=None, ok=True):
                try:
                    result = subprocess.run([str(binary / tool), *args], input=sql.encode() if sql else None,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, cwd=root,
                                            timeout=30)
                except subprocess.TimeoutExpired as error:
                    self.fail('%s timed out: %s' % (tool, (error.stderr or b'')[-2000:].decode(errors='replace')))
                if ok is True: self.assertEqual(result.returncode, 0, result.stderr[-2000:].decode(errors='replace'))
                self.assertLessEqual(len(result.stdout), 200000); self.assertLessEqual(len(result.stderr), 200000)
                return result

            def query(sql):
                return run('psql', '-X', '-w', '-qAt', '-v', 'ON_ERROR_STOP=1', '-f', '-', sql=sql).stdout

            def entry(grantor, kind, privilege, grant_option=False, name=None):
                grantee = {'kind': kind}
                if name is not None: grantee['name'] = name
                return {'grantor': grantor, 'grantee': grantee, 'privilege': privilege,
                        'grant_option': grant_option}

            def expected_contract():
                standard = ('pg_checkpoint', 'pg_create_subscription', 'pg_database_owner',
                            'pg_execute_server_program', 'pg_maintain', 'pg_monitor', 'pg_read_all_data',
                            'pg_read_all_settings', 'pg_read_all_stats', 'pg_read_server_files',
                            'pg_signal_backend', 'pg_stat_scan_tables', 'pg_use_reserved_connections',
                            'pg_write_all_data', 'pg_write_server_files')
                def role(name, login=False, superuser=False, createrole=False, createdb=False,
                         replication=False, bypassrls=False):
                    return {'name': name, 'superuser': superuser, 'inherit': True, 'createrole': createrole,
                            'createdb': createdb, 'login': login, 'replication': replication,
                            'bypassrls': bypassrls, 'settings_present': False}
                roles = [role(name) for name in standard]
                roles += [role('bootstrap', True, True, True, True, True, True), role('creator 雪', True),
                          role('reader'), role('PUBLIC')]
                return {
                    'version': 1, 'server_version_num': 170000 + int(run('postgres', '--version').stdout.split(b'17.')[1].split()[0]),
                    'selection': {'schemas': ['app space 雪', 'public'], 'creating_roles': ['creator 雪']},
                    'database': {'name': 'postgres', 'owner': 'bootstrap', 'settings_present': False,
                                 'acl': [entry('bootstrap', 'public', 'CONNECT'),
                                         entry('bootstrap', 'role', 'CREATE', name='bootstrap'),
                                         entry('bootstrap', 'role', 'CONNECT', name='bootstrap'),
                                         entry('bootstrap', 'public', 'TEMPORARY'),
                                         entry('bootstrap', 'role', 'TEMPORARY', name='bootstrap')]},
                    'schemas': [
                        {'name': 'app space 雪', 'present': True, 'owner': 'creator 雪',
                         'acl': [entry('creator 雪', 'role', 'CREATE', name='creator 雪'),
                                 entry('creator 雪', 'role', 'USAGE', name='creator 雪')]},
                        {'name': 'public', 'present': True, 'owner': 'pg_database_owner',
                         'acl': [entry('pg_database_owner', 'role', 'CREATE', name='pg_database_owner'),
                                 entry('pg_database_owner', 'role', 'USAGE', name='pg_database_owner'),
                                 entry('pg_database_owner', 'public', 'USAGE')]},
                    ],
                    'roles': roles,
                    'memberships': [
                        {'role': 'pg_read_all_settings', 'member': 'pg_monitor', 'grantor': 'bootstrap',
                         'admin': False, 'inherit': True, 'set': True},
                        {'role': 'pg_read_all_stats', 'member': 'pg_monitor', 'grantor': 'bootstrap',
                         'admin': False, 'inherit': True, 'set': True},
                        {'role': 'pg_stat_scan_tables', 'member': 'pg_monitor', 'grantor': 'bootstrap',
                         'admin': False, 'inherit': True, 'set': True},
                        {'role': 'reader', 'member': 'creator 雪', 'grantor': 'bootstrap',
                         'admin': False, 'inherit': True, 'set': True},
                    ],
                    'default_acls': [
                        {'creator': 'creator 雪', 'schema': None, 'object_type': 'r', 'acl': []},
                        {'creator': 'creator 雪', 'schema': 'app space 雪', 'object_type': 'r',
                         'acl': [entry('creator 雪', 'role', 'SELECT', False, 'reader')]},
                    ],
                }

            self.assertRegex(run('postgres', '--version').stdout, rb'PostgreSQL\) 17\.')
            run('initdb', '-D', str(root / 'data'), '-U', 'bootstrap', '-A', 'trust', '--encoding=UTF8', '--no-locale')
            log_path = root / 'server.log'
            with log_path.open('wb') as log:
                server = subprocess.Popen([str(binary / 'postgres'), '-D', str(root / 'data'), '-k', str(socket),
                                           '-p', '6555', '-c', 'listen_addresses='], env=env, cwd=root,
                                          stdin=subprocess.DEVNULL, stdout=log, stderr=log)
                for _ in range(50):
                    if run('pg_isready', '-q', ok=None).returncode == 0: break
                    self.assertIsNone(server.poll(), log_path.read_text(errors='replace')[-2000:]); time.sleep(.1)
                else: self.fail('local socket-only server readiness timeout')
                query('''CREATE ROLE "creator 雪" LOGIN; CREATE ROLE reader; CREATE ROLE "PUBLIC";
                    CREATE SCHEMA "app space 雪" AUTHORIZATION "creator 雪";
                    CREATE SCHEMA "app space 雪x" AUTHORIZATION "creator 雪";
                    CREATE TABLE "app space 雪".items (note text DEFAULT 'EXPRESSION-CANARY');
                    INSERT INTO "app space 雪".items VALUES ('ROW-CANARY');
                    CREATE FUNCTION "app space 雪".body_probe() RETURNS text LANGUAGE sql AS
                      $$SELECT 'ROUTINE-BODY-CANARY'::text$$;
                    CREATE TABLE "app space 雪x".decoy (note text DEFAULT 'DECOY-CANARY');
                    GRANT reader TO "creator 雪";
                    ALTER DEFAULT PRIVILEGES FOR ROLE "creator 雪" REVOKE ALL ON TABLES FROM "creator 雪";
                    ALTER DEFAULT PRIVILEGES FOR ROLE "creator 雪" IN SCHEMA "app space 雪"
                      GRANT SELECT ON TABLES TO reader;
                    ALTER DEFAULT PRIVILEGES FOR ROLE "creator 雪" IN SCHEMA "app space 雪x"
                      GRANT INSERT ON TABLES TO reader;
                ''')
                schemas, creators = ['public', 'app space 雪'], ['creator 雪']
                expected = expected_contract()  # independently specified stock-PG17 plus synthetic fixture contract
                def fixture_snapshot():
                    return query('''SELECT md5(
                      COALESCE((SELECT string_agg(note, ',' ORDER BY note) FROM "app space 雪".items), '') ||
                      COALESCE((SELECT string_agg(prosrc, ',' ORDER BY oid) FROM pg_proc
                        WHERE pronamespace = '"app space 雪"'::regnamespace), '') ||
                      COALESCE((SELECT string_agg(defaclrole::regrole::text || ':' || defaclobjtype::text || ':' ||
                        COALESCE(defaclacl::text, ''), ',' ORDER BY defaclrole, defaclnamespace, defaclobjtype)
                        FROM pg_default_acl), '') ||
                      COALESCE((SELECT string_agg(rolname || ':' || rolcanlogin::text, ',' ORDER BY rolname)
                        FROM pg_roles), ''));''')
                def local_psql(_cfg, _work, sql): return query(sql)
                before = fixture_snapshot()
                sql = permissions.observation_sql(schemas, creators)
                self.assertIn("current_setting('transaction_read_only')", sql)
                self.assertIn("current_setting('transaction_isolation')", sql)
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    observed = permissions.observe({}, root, schemas, creators)
                self.assertEqual(before, fixture_snapshot())
                self.assertEqual(permissions.compare(expected, observed, schemas, creators)['mismatch_categories'], [])
                rendered = str(observed)
                self.assertNotIn('ROW-CANARY', rendered); self.assertNotIn('ROUTINE-BODY-CANARY', rendered)
                self.assertNotIn('EXPRESSION-CANARY', rendered); self.assertNotIn('DECOY-CANARY', rendered)
                self.assertNotIn('app space 雪x', rendered)
                self.assertFalse(permissions.compare(expected, observed, schemas, creators)['export_ready'])

                query('ALTER DEFAULT PRIVILEGES FOR ROLE "creator 雪" GRANT SELECT ON TABLES TO "PUBLIC";')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    global_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('default-acls', permissions.compare(expected, global_drift, schemas, creators)['mismatch_categories'])
                query('ALTER DEFAULT PRIVILEGES FOR ROLE "creator 雪" REVOKE SELECT ON TABLES FROM "PUBLIC";')

                query('ALTER DEFAULT PRIVILEGES FOR ROLE "creator 雪" IN SCHEMA "app space 雪" GRANT SELECT ON TABLES TO reader WITH GRANT OPTION;')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    grant_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('default-acls', permissions.compare(expected, grant_drift, schemas, creators)['mismatch_categories'])
                query('REVOKE reader FROM "creator 雪"; GRANT reader TO "creator 雪" WITH SET FALSE;')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    membership_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('memberships', permissions.compare(expected, membership_drift, schemas, creators)['mismatch_categories'])
                query('GRANT CREATE ON SCHEMA public TO reader;')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    public_acl_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('schemas', permissions.compare(expected, public_acl_drift, schemas, creators)['mismatch_categories'])
                query('ALTER SCHEMA public OWNER TO bootstrap;')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    public_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('schemas', permissions.compare(expected, public_drift, schemas, creators)['mismatch_categories'])
                query('ALTER SCHEMA public OWNER TO pg_database_owner; ALTER DATABASE postgres OWNER TO "creator 雪";')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    database_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('database', permissions.compare(expected, database_drift, schemas, creators)['mismatch_categories'])
                query('ALTER ROLE "creator 雪" SET statement_timeout = 9876;')
                with patch.object(permissions.h, 'versions'), patch.object(permissions.h, 'psql', side_effect=local_psql):
                    setting_drift = permissions.observe({}, root, schemas, creators)
                self.assertIn('roles', permissions.compare(expected, setting_drift, schemas, creators)['mismatch_categories'])
                self.assertNotIn('9876', str(setting_drift))
        finally:
            if not cleanup_owned_cluster(binary, root, server, env):
                self.fail('owned local cluster shutdown unconfirmed; quarantined temporary directory')


    def test_cluster_shutdown_fallback_requires_confirmed_exit(self):
        with tempfile.TemporaryDirectory(prefix='spdp-stop-', dir='/tmp') as directory:
            root = Path(directory); (root / 'data').mkdir()
            calls = []
            class Result:
                def __init__(self, returncode): self.returncode = returncode
            class StoppedServer:
                def wait(self, timeout): self.timeout = timeout
                def poll(self): return 0
            def runner(argv, **_kwargs):
                calls.append(argv)
                return Result(1 if len(calls) == 1 else (0 if len(calls) == 2 else 2))
            self.assertTrue(stop_owned_cluster(Path('/owned/bin'), root, StoppedServer(), {}, runner))
            self.assertEqual([call[4] for call in calls[:2]], ['fast', 'immediate'])
            class LiveServer:
                def wait(self, timeout): raise subprocess.TimeoutExpired('pg_ctl', timeout)
                def poll(self): return None
            live_calls = []
            def live_runner(argv, **_kwargs):
                live_calls.append(argv); return Result(0)
            self.assertFalse(stop_owned_cluster(Path('/owned/bin'), root, LiveServer(), {}, live_runner))
            self.assertEqual([call[4] for call in live_calls[:2]], ['fast', 'immediate'])

    def test_lifecycle_quarantines_sentinel_after_simulated_stop_failure(self):
        root = Path(tempfile.mkdtemp(prefix='spdp-quarantine-', dir='/tmp'))
        try:
            (root / 'data').mkdir()
            sentinel = root / 'sentinel'; sentinel.write_text('synthetic-no-server')
            class Result:
                returncode = 1
            class SyntheticServer:
                def wait(self, timeout): self.timeout = timeout
                def poll(self): return None
            def failed_runner(_argv, **_kwargs): return Result()
            self.assertFalse(cleanup_owned_cluster(Path('/owned/bin'), root, SyntheticServer(), {}, failed_runner))
            self.assertTrue(root.is_dir())
            self.assertEqual(sentinel.read_text(), 'synthetic-no-server')
        finally:
            # This fixture never starts a process, so its explicit teardown cannot remove live state.
            if root.exists(): shutil.rmtree(root)


if __name__ == '__main__':
    unittest.main()
