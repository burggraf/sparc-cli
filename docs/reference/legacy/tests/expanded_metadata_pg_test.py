"""Opt-in disposable PG17 test. No TCP, hosted config, or ambient credentials.

SPARC_TEST_PG17_BIN=/path/to/pg17/bin python3 -m unittest discover -s tests -p 'expanded_metadata*_test.py'
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

from expanded_metadata_test import fixture
import expanded_metadata


@unittest.skipUnless(os.environ.get("SPARC_TEST_PG17_BIN"), "opt-in local PostgreSQL 17 test")
class LocalPostgresTest(unittest.TestCase):
    def test_real_sql_roundtrip_and_collisions(self):
        binary = Path(os.environ["SPARC_TEST_PG17_BIN"]).resolve()
        with tempfile.TemporaryDirectory(prefix="sparc-pg17-") as directory:
            root = Path(directory)
            env = {"PATH": "/usr/bin:/bin", "HOME": directory, "LC_ALL": "C",
                   "PGHOST": directory, "PGPORT": "6549", "PGUSER": "bootstrap",
                   "PGDATABASE": "postgres", "PGCONNECT_TIMEOUT": "2"}

            def run(tool, *args, sql=None):
                return subprocess.run([str(binary / tool), *args], input=sql,
                                      stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                      env=env, cwd=root, timeout=30)

            def query(sql, success=True):
                result = run("psql", "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1",
                             "-f", "-", sql=sql.encode())
                self.assertEqual(result.returncode == 0, success, "local SQL status mismatch")
                return result.stdout.decode().strip()

            self.assertRegex(run("postgres", "--version").stdout, rb"PostgreSQL\) 17\.")
            self.assertEqual(run("initdb", "-D", str(root / "data"), "-U", "bootstrap",
                                 "-A", "trust", "--encoding=UTF8", "--no-locale").returncode, 0)
            with (root / "server.log").open("wb") as log:
                server = subprocess.Popen(
                    [str(binary / "postgres"), "-D", str(root / "data"), "-k", directory,
                     "-h", "", "-p", "6549"], env=env, cwd=root,
                    stdin=subprocess.DEVNULL, stdout=log, stderr=log)
                try:
                    # Readiness retries belong to this bounded disposable-server test.
                    for _ in range(50):
                        if run("pg_isready", "-q").returncode == 0:
                            break
                        self.assertIsNone(server.poll(), "local server exited")
                        time.sleep(0.1)
                    else:
                        self.fail("local server readiness timeout")
                    query("CREATE ROLE postgres LOGIN CREATEROLE; ALTER DATABASE postgres OWNER TO postgres;")
                    env["PGUSER"] = "postgres"  # Exercise non-superuser role-creation behavior.
                    value = fixture()
                    roles, history = expanded_metadata.restore_sql(json.dumps(value).encode())
                    query("BEGIN;\n" + roles + history + "COMMIT;")
                    restored = json.loads(query("SELECT jsonb_agg(to_jsonb(h) ORDER BY version) "
                                                "FROM supabase_migrations.schema_migrations h;"))
                    self.assertEqual(restored, value["history"])
                    restored_roles = json.loads(query("""SELECT jsonb_agg(jsonb_build_object(
                        'name', rolname, 'login', rolcanlogin, 'superuser', rolsuper,
                        'createdb', rolcreatedb, 'createrole', rolcreaterole,
                        'replication', rolreplication, 'bypassrls', rolbypassrls,
                        'inherit', rolinherit) ORDER BY rolname) FROM pg_roles
                        WHERE rolname IN ('sparc_rehearsal_reader','sparc_rehearsal_member');"""))
                    self.assertEqual(restored_roles, sorted(value["roles"], key=lambda r: r["name"]))
                    self.assertEqual(query("""SELECT admin_option::text || ',' || inherit_option::text
                        || ',' || set_option::text FROM pg_auth_members
                        WHERE roleid='sparc_rehearsal_reader'::regrole
                        AND member='sparc_rehearsal_member'::regrole;"""), "false,true,true")
                    # A colliding role/history never overwrites the restored state.
                    query("BEGIN;" + roles + history + "COMMIT;", success=False)
                    query("BEGIN;" + history + "COMMIT;", success=False)
                    self.assertEqual(json.loads(query("SELECT jsonb_agg(to_jsonb(h) ORDER BY version) "
                                                      "FROM supabase_migrations.schema_migrations h;")), restored)
                    # Remove only these disposable local roles; history collision must
                    # reject BEFORE recreating either role even without a transaction.
                    query("DROP ROLE sparc_rehearsal_member; DROP ROLE sparc_rehearsal_reader;")
                    query(roles, success=False)
                    self.assertEqual(query("SELECT count(*) FROM pg_roles WHERE rolname IN "
                                           "('sparc_rehearsal_reader','sparc_rehearsal_member');"), "0")
                finally:
                    server.terminate()
                    try:
                        server.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        server.kill()
                        server.wait(timeout=5)


if __name__ == "__main__":
    unittest.main()
