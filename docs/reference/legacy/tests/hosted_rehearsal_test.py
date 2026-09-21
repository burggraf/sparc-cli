"""Offline-only regression: fake PG processes + real local SPARC encryption.

Build first: cargo build --locked --offline. No PostgreSQL server is contacted.
"""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
RUNNER = ROOT / "scripts/hosted_rehearsal.py"
SPARC = ROOT / "target/debug/sparc"
SPEC = importlib.util.spec_from_file_location("hosted_rehearsal", RUNNER)
runner = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runner)

FAKE = r'''
import json, os, pathlib, sys
home = pathlib.Path(__file__).parent
state = json.loads((home / "state.json").read_text())
name = pathlib.Path(sys.argv[0]).name
if "--version" in sys.argv:
    print(name + " (PostgreSQL) " + state.get("version", "17.9"))
    sys.exit(0)
sql = sys.stdin.read()
entry = {"tool": name, "args": sys.argv[1:], "env": dict(os.environ),
         "sql": sql, "cwd": os.getcwd()}
with (home / "log.jsonl").open("a") as f:
    f.write(json.dumps(entry) + "\n")
if state.get("error"):
    print("postgres://user:SECRET-CANARY@host SECRET-CANARY", file=sys.stderr)
    sys.exit(1)
if name == "pg_dump":
    if state.get("partial"):
        print("-- partial SQL SECRET-CANARY")
        sys.exit(1)
    if state.get("incomplete"):
        print("-- partial SQL")
        sys.exit(0)
    if state.get("oversized"):
        sys.stdout.write("x" * (3 * 1024 * 1024))
        sys.exit(0)
    print((home / "dump.sql").read_text())
    sys.exit(0)
if "SPARC_GUARD_FIXTURE" in sql and "SPARC_GUARD_EMPTY" not in sql and os.environ["PGUSER"] != "postgres." + "a" * 20:
    sys.exit(1)
if "SPARC_GUARD_EMPTY" in sql and (state.get("dirty") or state.get("wrong_target")):
    print("unsafe target SECRET-CANARY", file=sys.stderr)
    sys.exit(1)
if sql.startswith("BEGIN;\n"):
    state["dirty"] = True
    (home / "state.json").write_text(json.dumps(state))
    print("sparc-seed-ok" if "sparc-seed-ok" in sql else "sparc-restore-ok")
else:
    print("sparc-check-ok")
'''


class RehearsalTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sparc-offline-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "fake-pg"
        self.bin.mkdir(mode=0o700)
        self.state()
        for tool in ("psql", "pg_dump"):
            path = self.bin / tool
            path.write_text("#!" + sys.executable + "\n" + FAKE)
            path.chmod(0o700)
        self.dump = "--\n-- PostgreSQL database dump\nSET row_security = off;\n" + runner.SEED + "\n-- PostgreSQL database dump complete\n"
        (self.bin / "dump.sql").write_text(self.dump)
        self.ca = self.root / "ca.pem"
        self.ca.write_text("-----BEGIN CERTIFICATE-----\nsynthetic\n-----END CERTIFICATE-----\n")
        self.source = self.configuration("a" * 20)
        self.target = self.configuration("b" * 20)
        self.identity = self.root / "key.age"
        keygen = subprocess.run([str(SPARC), "keygen", str(self.identity)], capture_output=True, check=True)
        self.recipient = keygen.stdout.decode().strip()
        self.archive = self.root / "archive"

    def configuration(self, ref):
        pgpass = self.root / (ref + ".pgpass")
        host = "aws-0-us-east-1.pooler.supabase.com"
        pgpass.write_text(f"{host}:5432:postgres:postgres.{ref}:SECRET-CANARY\n")
        pgpass.chmod(0o600)
        cfg = {"version": 1, "project_ref": ref, "host": host,
               "pg_bin": str(self.bin), "sparc": str(SPARC),
               "pgpass": str(pgpass), "ca": str(self.ca)}
        path = self.root / (ref + ".json")
        path.write_text(json.dumps(cfg))
        return path

    def state(self, **values):
        (self.bin / "state.json").write_text(json.dumps(values))

    def logs(self):
        path = self.bin / "log.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def invoke(self, *args, ok=True):
        env = dict(os.environ, PGPASSWORD="SECRET-CANARY", SUPABASE_ACCESS_TOKEN="SECRET-CANARY",
                   PGHOST="untrusted", PGSERVICE="untrusted", PGOPTIONS="untrusted", PATH="")
        result = subprocess.run([sys.executable, str(RUNNER), *map(str, args)],
                                env=env, capture_output=True, timeout=30)
        self.assertNotIn(b"SECRET-CANARY", result.stdout + result.stderr)
        self.assertNotIn(b"Traceback", result.stderr)
        self.assertEqual(result.returncode, 0 if ok else 1, result.stderr.decode())
        return result

    def capture(self):
        self.invoke("capture", self.source, self.archive, self.recipient)

    def test_runner_exists_and_rejects_unknown_mode(self):
        with self.assertRaises(runner.RehearsalError):
            runner.main(["not-a-live-command"])
        self.invoke("not-a-live-command", ok=False)

    def test_source_independent_roundtrip_and_reused_target(self):
        self.capture()
        cfg = json.loads(self.source.read_text())
        Path(cfg["pgpass"]).unlink()
        self.source.unlink()
        self.invoke("restore", self.target, self.archive, self.identity)
        logs = self.logs()
        dump = next(log for log in logs if log["tool"] == "pg_dump")
        self.assertIn("--schema=sparc_rehearsal", dump["args"])
        self.assertIn("--no-owner", dump["args"])
        mutations = [log for log in logs if log["sql"].startswith("BEGIN;\n")]
        self.assertEqual(len(mutations), 1)
        self.assertEqual(mutations[0]["env"]["PGUSER"], "postgres." + "b" * 20)
        sql = mutations[0]["sql"]
        self.assertLess(sql.index("SPARC_GUARD_EMPTY"), sql.index("CREATE SCHEMA"))
        self.assertLess(sql.index("SELECT setval"), sql.rindex("SPARC_ASSERT_FIXTURE"))
        self.assertIn("SET LOCAL ROLE authenticated", sql)
        self.assertLess(sql.index("SET row_security = off;"), sql.index("SET LOCAL row_security=on;"))
        self.assertLess(sql.index("SET LOCAL row_security=on;"), sql.index("SET LOCAL ROLE authenticated"))
        for log in logs:
            self.assertEqual(log["env"]["PGSSLMODE"], "verify-full")
            self.assertEqual(log["env"]["PGPORT"], "5432")
            self.assertNotIn("PGPASSWORD", log["env"])
            self.assertNotIn("PGSERVICE", log["env"])
            self.assertNotIn("SUPABASE_ACCESS_TOKEN", log["env"])
            self.assertFalse(Path(log["cwd"]).exists(), "plaintext workdir leaked")
            if "BEGIN READ ONLY" in log["sql"]:
                self.assertNotIn("setval", log["sql"])
                self.assertNotIn("INSERT INTO", log["sql"])
        before = len(self.logs())
        self.invoke("restore", self.target, self.archive, self.identity, ok=False)
        self.assertEqual(len(self.logs()), before + 1)
        self.assertFalse(self.logs()[-1]["sql"].startswith("BEGIN;\n"))

    def test_catalog_char_aggregations_use_explicit_text_casts(self):
        # Real PG17 rejected both original concatenations with SQLSTATE 42725.
        # This protects the SQL contract; fake processes do not bind SQL types.
        for sql, column in ((runner.guard(), "evtenabled"),
                            (runner.assertions(), "c.relkind")):
            with self.subTest(column=column):
                self.assertIn("|| " + column + "::text", sql)
                self.assertNotIn("|| " + column + ",", sql)

    def test_seed_checks_empty_before_mutation(self):
        self.invoke("seed", self.source)
        logs = self.logs()
        self.assertEqual(len(logs), 2)
        self.assertTrue(logs[0]["sql"].startswith("BEGIN READ ONLY"))
        self.assertTrue(logs[1]["sql"].startswith("BEGIN;\n"))
        self.invoke("seed", self.source, ok=False)

    def test_same_source_rejected_before_connection(self):
        self.capture()
        before = len(self.logs())
        self.invoke("restore", self.source, self.archive, self.identity, ok=False)
        self.assertEqual(len(self.logs()), before)

    def test_wrong_capture_source_is_not_a_fixture(self):
        self.invoke("capture", self.target, self.archive, self.recipient, ok=False)
        self.assertFalse(self.archive.exists())
        self.assertEqual(len(self.logs()), 1)
        self.assertEqual(self.logs()[0]["tool"], "psql")

    def test_recovery_contract_rejected_before_connection(self):
        self.capture()
        material = self.root / "material"
        subprocess.run([str(SPARC), "unpack", str(self.archive), str(material), str(self.identity)],
                       capture_output=True, check=True)
        original = json.loads((material / "recovery.json").read_text())
        cases = [dict(original, version=True), dict(original, source_ref="bad"),
                 dict(original, sha256="0" * 64), dict(original, password="SECRET-CANARY")]
        before = len(self.logs())
        for index, metadata in enumerate(cases):
            (material / "recovery.json").write_text(json.dumps(metadata))
            archive = self.root / ("bad-archive-" + str(index))
            subprocess.run([str(SPARC), "pack", str(material), str(archive), self.recipient],
                           capture_output=True, check=True)
            self.invoke("restore", self.target, archive, self.identity, ok=False)
            self.assertEqual(len(self.logs()), before)

    def test_unsafe_target_rejected_before_mutation(self):
        self.capture()
        for failure in ("dirty", "wrong_target", "error"):
            with self.subTest(failure=failure):
                self.state(**{failure: True})
                self.invoke("restore", self.target, self.archive, self.identity, ok=False)
                self.assertFalse(any(log["sql"].startswith("BEGIN;\n") for log in self.logs()))

    def test_partial_incomplete_oversized_export_never_published(self):
        for failure in ("partial", "incomplete", "oversized", "error"):
            with self.subTest(failure=failure):
                self.state(**{failure: True})
                self.invoke("capture", self.source, self.archive, self.recipient, ok=False)
                self.assertFalse(self.archive.exists())
                self.assertTrue(all(not Path(log["cwd"]).exists() for log in self.logs()))

    def test_archive_no_clobber_and_wrong_identity(self):
        self.capture()
        original = {p.name: p.read_bytes() for p in self.archive.iterdir()}
        before = len(self.logs())
        self.invoke("capture", self.source, self.archive, self.recipient, ok=False)
        self.assertEqual(original, {p.name: p.read_bytes() for p in self.archive.iterdir()})
        wrong = self.root / "wrong.age"
        subprocess.run([str(SPARC), "keygen", str(wrong)], capture_output=True, check=True)
        self.invoke("restore", self.target, self.archive, wrong, ok=False)
        self.assertEqual(len(self.logs()), before)

    def test_malformed_config_and_credential_mixups(self):
        valid = json.loads(self.target.read_text())
        cases = ["[]", "{", "null", " " * 16385,
                 json.dumps(valid)[:-1] + ',"version":1}',
                 json.dumps(dict(valid, version=True)),
                 json.dumps(dict(valid, password="SECRET-CANARY")),
                 json.dumps(dict(valid, host="evil.example")),
                 json.dumps(dict(valid, host=123)),
                 json.dumps(dict(valid, project_ref="a" * 20)),
                 json.dumps(dict(valid, pgpass=json.loads(self.source.read_text())["pgpass"])),
                 json.dumps(dict(valid, pg_bin="relative")),
                 json.dumps(dict(valid, ca=[]))]
        for content in cases:
            with self.subTest(content=content[:30]):
                self.target.write_text(content)
                self.invoke("seed", self.target, ok=False)
                self.assertEqual(self.logs(), [])

    def test_unsafe_passfiles_ca_versions_and_symlinks(self):
        cfg = json.loads(self.target.read_text())
        passfile = Path(cfg["pgpass"])
        original = passfile.read_text()
        for content in ("*:*:*:*:SECRET-CANARY\n", original * 2, original + "# comment\n"):
            passfile.write_text(content)
            self.invoke("seed", self.target, ok=False)
        passfile.write_text(original)
        passfile.chmod(0o644)
        self.invoke("seed", self.target, ok=False)
        passfile.chmod(0o600)
        self.ca.write_text("not a certificate")
        self.invoke("seed", self.target, ok=False)
        self.ca.write_text("-----BEGIN CERTIFICATE-----\nsynthetic\n-----END CERTIFICATE-----")
        self.state(version="18.1")
        self.invoke("seed", self.target, ok=False)
        self.assertEqual(self.logs(), [])
        alias = self.root / "alias.json"
        alias.symlink_to(self.target)
        self.invoke("seed", alias, ok=False)

    def test_timeout_error_is_redacted(self):
        with mock.patch.object(runner.subprocess, "run", side_effect=subprocess.TimeoutExpired("SECRET-CANARY", 60)):
            with self.assertRaises(runner.RehearsalError) as caught:
                runner.run(["unused-fake-tool"], self.root)
        self.assertNotIn("SECRET-CANARY", str(caught.exception))
        self.assertIn("quarantine", str(caught.exception))

    def test_archive_tampering_and_bounds_precede_connection(self):
        self.capture()
        before = len(self.logs())
        extra = self.archive / "extra.age"
        extra.write_text("unexpected")
        self.invoke("restore", self.target, self.archive, self.identity, ok=False)
        extra.unlink()
        payload = self.archive / "00000000.age"
        payload.write_bytes(b"tampered")
        self.invoke("restore", self.target, self.archive, self.identity, ok=False)
        with payload.open("wb") as handle:
            handle.truncate(runner.LIMIT + 1)
        self.invoke("restore", self.target, self.archive, self.identity, ok=False)
        self.assertEqual(len(self.logs()), before)


if __name__ == "__main__":
    unittest.main()
