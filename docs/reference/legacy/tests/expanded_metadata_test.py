"""Offline contract tests only: no hosted connections or credentials."""
import copy
import importlib.util
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import hosted_rehearsal as harness


def fixture():
    attributes = dict(login=False, superuser=False, createdb=False, createrole=False,
                      replication=False, bypassrls=False, inherit=True)
    return {"version": 1,
            "roles": [dict(name=name, **attributes) for name in
                      ("sparc_rehearsal_reader", "sparc_rehearsal_member")],
            "memberships": [{"role": "sparc_rehearsal_reader",
                             "member": "sparc_rehearsal_member", "admin": False,
                             "inherit": True, "set": True}],
            "history": [
                {"version": "202609200001", "name": None, "statements": None},
                {"version": "202609200002", "name": "", "statements": []},
                {"version": "202609200003", "name": "雪 ' \\ \n",
                 "statements": ["SELECT 1/0;", None, "\\! false\n' $x$ \\"]}]}


class ExpandedMetadataTests(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(importlib.util.find_spec("expanded_metadata"),
                             "expanded metadata implementation missing")
        import expanded_metadata
        self.module = expanded_metadata

    def encode(self, value):
        return json.dumps(value, ensure_ascii=True).encode()

    def test_sql_encodes_history_as_data_and_guards_collisions(self):
        self.assertTrue(hasattr(self.module, "restore_sql"), "SQL generation missing")
        value = fixture()
        roles, history = self.module.restore_sql(self.encode(value))
        self.assertLess(roles.index("pg_roles"), roles.index("CREATE ROLE"))
        self.assertIn("pg_namespace", roles)
        self.assertLess(roles.index("pg_namespace"), roles.index("CREATE ROLE"))
        self.assertLess(history.index("pg_namespace"), history.index("CREATE SCHEMA"))
        self.assertNotIn("IF NOT EXISTS", roles + history)
        self.assertNotIn("ALTER ROLE", roles)
        self.assertNotIn("CASCADE", roles + history)
        self.assertNotIn("COMMIT", roles + history)
        self.assertIn("WITH ADMIN FALSE", roles)
        self.assertIn("WITH INHERIT TRUE", roles)
        self.assertIn("WITH SET TRUE", roles)
        self.assertIn("WITH ORDINALITY", history)
        self.assertIn("ORDER BY ordinal", history)
        self.assertNotIn("SELECT 1/0", history)
        self.assertNotIn("\\\\! false", history)
        import re
        payload = re.search(r"decode\('([0-9a-f]+)', 'hex'\)", history).group(1)
        self.assertEqual(json.loads(bytes.fromhex(payload)), value["history"])

    def test_sql_revalidates_input(self):
        self.assertTrue(hasattr(self.module, "restore_sql"), "SQL generation missing")
        value = fixture()
        value["roles"][0]["name"] = "postgres"
        with self.assertRaisesRegex(harness.RehearsalError, "^invalid expanded metadata$"):
            self.module.restore_sql(self.encode(value))

    def test_round_trip_preserves_null_empty_and_order(self):
        value = fixture()
        self.assertEqual(self.module.validate(self.encode(value)), value)

    def test_rejects_unsafe_roles_and_memberships(self):
        changes = [lambda v: v["roles"][0].update(name="postgres"),
                   lambda v: v["roles"][0].update(login=True),
                   lambda v: v["roles"][0].update(superuser=True),
                   lambda v: v["roles"][0].update(password="do-not-echo"),
                   lambda v: v["roles"][0].update(inherit=1),
                   lambda v: v["roles"].append(copy.deepcopy(v["roles"][0])),
                   lambda v: v["memberships"][0].update(member="authenticated"),
                   lambda v: v["memberships"][0].update(admin=True),
                   lambda v: v["memberships"][0].update(set=False)]
        for change in changes:
            with self.subTest(change=changes.index(change)):
                value = fixture()
                change(value)
                with self.assertRaisesRegex(harness.RehearsalError, "^invalid expanded metadata$"):
                    self.module.validate(self.encode(value))

    def test_rejects_invalid_history_and_fields(self):
        changes = [lambda v: v.update(version=True), lambda v: v.update(extra=0),
                   lambda v: v["history"].append(copy.deepcopy(v["history"][0])),
                   lambda v: v["history"].reverse(),
                   lambda v: v["history"][0].update(version="'; DROP ROLE postgres"),
                   lambda v: v["history"][0].update(name=7),
                   lambda v: v["history"][0].update(name="\x00"),
                   lambda v: v["history"][0].update(name="\ud800"),
                   lambda v: v["history"][0].update(statements="SELECT 1"),
                   lambda v: v["history"][0].update(statements=[{}]),
                   lambda v: v["history"][0].update(statements=[False]),
                   lambda v: v["history"][0].update(statements=["x"] * 101),
                   lambda v: v["history"][0].update(other="secret")]
        for change in changes:
            with self.subTest(change=changes.index(change)):
                value = fixture()
                change(value)
                with self.assertRaisesRegex(harness.RehearsalError, "^invalid expanded metadata$"):
                    self.module.validate(self.encode(value))

    def test_rejects_duplicate_fields_invalid_encoding_and_bounds(self):
        valid = self.encode(fixture())
        bad = [b'{"version":1,' + valid[1:], b'\xff', b'NaN', b'Infinity', b'[]',
               b'{', b' ' * 65537, b'[' * 1100 + b']' * 1100,
               valid.replace(b'"login": false', b'"login": false, "login": false', 1)]
        for data in bad:
            with self.subTest(size=len(data)):
                with self.assertRaisesRegex(harness.RehearsalError, "^invalid expanded metadata$"):
                    self.module.validate(data)


if __name__ == "__main__":
    unittest.main()
