"""Offline runner checks; no hosted credentials or upstream checkout required."""
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import hashlib
import json
import subprocess

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import native_recipe as recipe


class NativeRecipeTests(unittest.TestCase):
    def test_pins_and_refusals(self):
        self.assertEqual(recipe.SCRIPT_SHA256, {
            "schema": "5cd57189f6565ddf651ff149995398a4c9b1971ca34a0093a77c011a41f21d64",
            "data": "c943c7a926122ea0649ddd4bf9b8fb9b12bed23ac9f33da0b489ac83e687d241",
        })
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            script = root / "dump_schema.sh"
            script.write_text("echo should-not-run")
            with patch.object(recipe.harness, "run") as run:
                with self.assertRaises(recipe.harness.RehearsalError):
                    recipe.dump({}, root, root, "schema")
                with self.assertRaises(recipe.harness.RehearsalError):
                    recipe.dump({}, root, root, "role")
                run.assert_not_called()

    def test_real_shell_preserves_tls_passfile_and_scope_without_ambient_secrets(self):
        with tempfile.TemporaryDirectory(prefix="recipe space ") as tmp:
            root = Path(tmp)
            tools = root / "pg bin"
            tools.mkdir()
            fake = tools / "pg_dump"
            fake.write_text('''#!/bin/bash
set -eu
[[ "$PGSSLMODE" == verify-full ]]
[[ "$PGSSLROOTCERT" == "$HOME/ca.pem" ]]
[[ "$PGPASSFILE" == "$HOME/pgpass" ]]
[[ "$PGHOST" == example.invalid ]]
[[ "$PGUSER" == postgres.aaaaaaaaaaaaaaaaaaaa ]]
[[ "$PGPASSWORD" == "" ]]
[[ "$PGOPTIONS" == *default_transaction_read_only=on* ]]
[[ -z "${SUPABASE_ACCESS_TOKEN+x}" ]]
[[ "$EXCLUDED_SCHEMAS" == "" ]]
[[ "$INCLUDED_SCHEMAS" == sparc_rehearsal ]]
if [[ "$1" == schema ]]; then
  [[ "$EXTRA_FLAGS" == --schema=sparc_rehearsal ]]
else
  [[ "$EXTRA_FLAGS" == "" ]]
fi
printf 'synthetic dump ok\\n'
''')
            fake.chmod(0o700)
            cfg = {"pg_bin": str(tools), "host": "example.invalid",
                   "project_ref": "a" * 20, "ca": str(root / "ca.pem"),
                   "pgpass": str(root / "pgpass")}
            for mode in ("schema", "data"):
                # Synthetic scripts isolate runner behavior, not upstream SQL parity.
                body = f'export PGPASSWORD="$PGPASSWORD"\npg_dump {mode}\n'.encode()
                (root / f"dump_{mode}.sh").write_bytes(body)
                with patch.dict(recipe.SCRIPT_SHA256, {mode: hashlib.sha256(body).hexdigest()}), \
                     patch.dict(os.environ, {"SUPABASE_ACCESS_TOKEN": "must-not-inherit", "PGSSLMODE": "disable", "PGPASSWORD": "must-not-inherit"}):
                    self.assertEqual(recipe.dump(cfg, root, root, mode), b"synthetic dump ok\n")

    def test_capture_encrypts_fixed_inventory_with_real_archive_engine(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            sparc = Path(__file__).resolve().parents[1] / "target/debug/sparc"
            identity = root / "key.agekey"
            recipient = subprocess.check_output([str(sparc), "keygen", str(identity)], text=True).strip()
            cfg = {"source": "unused", "project_ref": "a" * 20, "sparc": str(sparc)}
            sql = b"-- PostgreSQL database dump\n-- PostgreSQL database dump complete\n"
            archive = root / "archive"
            with patch.object(recipe.harness, "versions") as versions, \
                 patch.object(recipe.harness, "check") as check, \
                 patch.object(recipe, "dump", return_value=sql) as dump:
                recipe.capture(cfg, root, root, archive, recipient)
                versions.assert_called_once_with(cfg, root)
                self.assertEqual(check.call_count, 2)
                self.assertEqual([c.args[-1] for c in dump.call_args_list], ["schema", "data"])
            subprocess.run([str(sparc), "verify", str(archive), str(identity)], check=True, capture_output=True)
            recovered = root / "recovered"
            subprocess.run([str(sparc), "unpack", str(archive), str(recovered), str(identity)], check=True, capture_output=True)
            self.assertEqual({p.name for p in recovered.iterdir()}, {"schema.sql", "data.sql", "recovery.json"})
            meta = json.loads((recovered / "recovery.json").read_text())
            self.assertEqual(meta["source_ref"], "a" * 20)
            self.assertEqual(meta["recipe"], recipe.RECIPE)
            self.assertEqual(meta["scripts"], recipe.SCRIPT_SHA256)
            for name in ("schema.sql", "data.sql"):
                self.assertEqual((recovered / name).read_bytes(), sql)
                self.assertEqual(meta["sha256"][name], hashlib.sha256(sql).hexdigest())
                self.assertEqual((recovered / name).stat().st_mode & 0o077, 0)
            self.assertEqual(len(list(archive.iterdir())), 4)
            with patch.object(recipe.harness, "check") as check:
                with self.assertRaises(recipe.harness.RehearsalError):
                    recipe.capture(cfg, root, root, archive, recipient)
                check.assert_not_called()

    def test_capture_rejects_incomplete_oversized_and_changed_source_before_pack(self):
        for output in (b"partial", b"x" * (recipe.harness.LIMIT + 1),
                       b"-- PostgreSQL database dump\n-- PostgreSQL database dump complete\n"):
            with self.subTest(size=len(output)), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                with patch.object(recipe.harness, "versions"), \
                     patch.object(recipe.harness, "check", side_effect=[None, recipe.harness.RehearsalError("changed")]), \
                     patch.object(recipe, "dump", return_value=output), \
                     patch.object(recipe.harness, "run") as run:
                    with self.assertRaises(recipe.harness.RehearsalError):
                        recipe.capture({"project_ref": "a" * 20}, root, root, root / "archive", "age1" + "a" * 58)
                    run.assert_not_called()
                    self.assertFalse((root / "archive").exists())

    def make_archive(self, root, change=None):
        sparc = Path(__file__).resolve().parents[1] / "target/debug/sparc"
        identity = root / "key.agekey"
        recipient = subprocess.run([str(sparc), "keygen", str(identity)], check=True, capture_output=True).stdout.decode().strip()
        material = root / "input"
        material.mkdir(mode=0o700)
        payloads = {"schema.sql": b"-- trusted schema marker\n", "data.sql": b"-- trusted data marker\n"}
        meta = {"version": 1, "recipe": recipe.RECIPE, "source_ref": "a" * 20,
                "scripts": recipe.SCRIPT_SHA256,
                "sha256": {k: hashlib.sha256(v).hexdigest() for k, v in payloads.items()}}
        if change:
            change(meta)
        for name, data in payloads.items():
            (material / name).write_bytes(data)
        (material / "recovery.json").write_text(json.dumps(meta))
        archive = root / "archive"
        subprocess.run([str(sparc), "pack", str(material), str(archive), recipient], check=True, capture_output=True)
        return {"sparc": str(sparc), "project_ref": "b" * 20}, archive, identity

    def test_restore_checks_inventory_then_empty_target_and_resets_replication_role(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg, archive, identity = self.make_archive(root)
            with patch.object(recipe.harness, "versions"), patch.object(recipe.harness, "check") as check, \
                 patch.object(recipe.harness, "psql", return_value=b"sparc-recipe-restore-ok\n") as psql:
                recipe.restore(cfg, root, archive, identity)
                check.assert_called_once_with(cfg, root)
                sql = psql.call_args.args[2]
                self.assertTrue(sql.startswith(b"BEGIN;"))
                self.assertLess(sql.index(b"trusted schema marker"), sql.index(b"trusted data marker"))
                self.assertLess(sql.index(b"SET LOCAL session_replication_role=origin"), sql.index(b"-- Target only:"))
                self.assertIn(b"SPARC_GUARD_EMPTY", sql)
                self.assertIn(b"SPARC_GUARD_FIXTURE", sql)
                self.assertIn(b"COMMIT;", sql)

    def test_restore_rejects_bad_metadata_before_database_access(self):
        for change in (lambda m: m.update(source_ref="b" * 20),
                       lambda m: m.update(version=True),
                       lambda m: m.update(scripts={}),
                       lambda m: m["sha256"].update({"schema.sql": "0" * 64})):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                cfg, archive, identity = self.make_archive(root, change)
                with patch.object(recipe.harness, "versions"), patch.object(recipe.harness, "check") as check, \
                     patch.object(recipe.harness, "psql") as psql:
                    with self.assertRaises(recipe.harness.RehearsalError):
                        recipe.restore(cfg, root, archive, identity)
                    check.assert_not_called()
                    psql.assert_not_called()

    def test_restore_refuses_used_target_before_sql_execution(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg, archive, identity = self.make_archive(root)
            with patch.object(recipe.harness, "versions"), \
                 patch.object(recipe.harness, "check", side_effect=recipe.harness.RehearsalError("used target")), \
                 patch.object(recipe.harness, "psql") as psql:
                with self.assertRaises(recipe.harness.RehearsalError):
                    recipe.restore(cfg, root, archive, identity)
                psql.assert_not_called()

    def test_cleanup_is_transactional_restrict_only_and_checks_fixture_first(self):
        sql = recipe.cleanup_sql()
        self.assertTrue(sql.startswith("BEGIN;"))
        self.assertNotIn("CASCADE", sql)
        self.assertLess(sql.index("LOCK TABLE"), sql.index("fixture assertion rejected"))
        self.assertLess(sql.index("fixture assertion rejected"), sql.index("DROP TABLE"))
        self.assertIn("DROP TABLE sparc_rehearsal.notes RESTRICT;", sql)
        self.assertIn("DROP SCHEMA sparc_rehearsal RESTRICT;", sql)
        self.assertLess(sql.index("SPARC_GUARD_EMPTY"), sql.index("COMMIT;"))

    def test_script_failure_is_bounded_and_redacted(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            body = b'echo private-diagnostic >&2; exit 1\n'
            (root / "dump_data.sh").write_bytes(body)
            cfg = {"pg_bin": "/usr/bin", "host": "example.invalid",
                   "project_ref": "a" * 20, "ca": "/unused", "pgpass": "/unused"}
            with patch.dict(recipe.SCRIPT_SHA256, {"data": hashlib.sha256(body).hexdigest()}):
                with self.assertRaises(recipe.harness.RehearsalError) as error:
                    recipe.dump(cfg, root, root, "data")
                self.assertNotIn("private-diagnostic", str(error.exception))


if __name__ == "__main__":
    unittest.main()
