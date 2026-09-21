"""Offline v2 contract/real age checks, never a database connection."""
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
from expanded_metadata_test import fixture
import hosted_rehearsal as h
import expanded_rehearsal as v2

SPARC = Path(__file__).resolve().parents[1] / "target/debug/sparc"
CATALOG = json.dumps({k: ('' if k == 'view' else []) for k in v2.CATALOG_KEYS}).encode()
DUMP = b"-- PostgreSQL database dump\n-- PostgreSQL database dump complete\n"


class ExpandedRehearsalTests(unittest.TestCase):
    def test_fixed_metadata_and_sql_boundaries(self):
        self.assertEqual(v2.fixture_metadata(), fixture())
        raw = json.dumps(fixture()).encode()
        self.assertEqual(v2.validate_metadata(raw), fixture())
        bad = copy.deepcopy(fixture())
        bad['history'][0]['name'] = 'changed'
        with self.assertRaises(h.RehearsalError):
            v2.validate_metadata(json.dumps(bad).encode())
        sql = v2.source_extension_sql()
        self.assertTrue(sql.startswith('BEGIN;'))
        self.assertLess(sql.index('SPARC_ASSERT_FIXTURE'), sql.index('CREATE ROLE'))
        self.assertLess(sql.index('expanded history collision'), sql.index('CREATE ROLE'))
        self.assertIn('security_invoker=true', sql)
        self.assertNotIn('SELECT 1/0;', sql)
        self.assertNotIn('DROP ', sql)
        self.assertIn(h.EXTENSIONS, v2.guard())
        self.assertIn(h.TRIGGERS, v2.guard())
        self.assertNotIn('INSERT ', v2.readonly_sql())
        self.assertNotIn('setval', v2.readonly_sql())
        self.assertTrue(v2.readonly_sql().startswith('BEGIN READ ONLY;'))

    def archive(self, root, change=None):
        identity = root / 'identity'
        recipient = subprocess.check_output([str(SPARC), 'keygen', str(identity)], text=True).strip()
        cfg = {'project_ref': 'a'*20, 'sparc': str(SPARC)}
        archive = root / 'archive'
        with patch.object(h, 'versions'), patch.object(v2, 'check'), \
             patch.object(v2, 'capture_metadata', return_value=json.dumps(fixture()).encode()), \
             patch.object(v2, 'capture_catalog', return_value=CATALOG), \
             patch.object(v2.native, 'dump', return_value=DUMP):
            v2.capture(cfg, root, root, archive, recipient, identity)
        if change:
            material = root / 'changed'
            subprocess.run([str(SPARC), 'unpack', str(archive), str(material), str(identity)], check=True, capture_output=True)
            change(material)
            archive = root / 'changed-archive'
            subprocess.run([str(SPARC), 'pack', str(material), str(archive), recipient], check=True, capture_output=True)
        return dict(cfg, project_ref='b'*20), archive, identity

    def test_capture_verified_restore_destination_only_fenced_and_ordered(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg, archive, identity = self.archive(root)
            self.assertEqual(len(list(archive.iterdir())), 6)
            fence = root / 'attempt'
            with patch.object(h, 'versions'), patch.object(v2, 'check') as check, \
                 patch.object(h, 'psql', return_value=b'sparc-expanded-restore-ok\n') as psql, \
                 patch.object(v2, 'capture_catalog', return_value=CATALOG):
                v2.restore(cfg, root, archive, identity, fence)
                self.assertEqual(check.call_count, 2)
                sql = psql.call_args.args[2]
                self.assertLess(sql.index(b'SPARC_GUARD_EMPTY'), sql.index(b'CREATE ROLE'))
                self.assertLess(sql.index(b'CREATE ROLE'), sql.index(DUMP))
                self.assertLess(sql.index(b'SET LOCAL session_replication_role=origin'), sql.index(b'CREATE SCHEMA supabase_migrations'))
                self.assertLess(sql.index(b'CREATE SCHEMA supabase_migrations'), sql.index(b'-- SPARC_ASSERT_EXPANDED'))
                self.assertIn(b"SELECT setval('sparc_rehearsal.note_id',42,true)", sql)
                self.assertTrue(fence.exists())
                before = psql.call_count
                with self.assertRaises((h.RehearsalError, FileExistsError)):
                    v2.restore(cfg, root / 'retry', archive, identity, fence)
                self.assertEqual(psql.call_count, before)

    def test_invalid_contract_hash_inventory_and_same_source_never_connect(self):
        def metadata(material, change):
            path = material / 'recovery.json'
            value = json.loads(path.read_bytes())
            change(value)
            path.write_text(json.dumps(value))
        changes = [lambda p: metadata(p, lambda m: m.update(version=True)),
                   lambda p: metadata(p, lambda m: m.update(source_ref='b'*20)),
                   lambda p: metadata(p, lambda m: m.update(scripts={})),
                   lambda p: (p / 'schema.sql').write_bytes(b'changed'),
                   lambda p: (p / 'extra').write_bytes(b'unknown')]
        for change in changes:
            with self.subTest(change=changes.index(change)), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                cfg, archive, identity = self.archive(root, change)
                with patch.object(h, 'versions') as versions, patch.object(h, 'psql') as psql:
                    with self.assertRaises(h.RehearsalError):
                        v2.restore(cfg, root, archive, identity, root / 'fence')
                    versions.assert_not_called()
                    psql.assert_not_called()

    def test_target_refusal_and_mutation_failure_fence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg, archive, identity = self.archive(root)
            with patch.object(h, 'versions'), patch.object(v2, 'check', side_effect=h.RehearsalError('used target')), \
                 patch.object(h, 'psql') as psql:
                with self.assertRaises(h.RehearsalError):
                    v2.restore(cfg, root, archive, identity, root / 'fence')
                psql.assert_not_called()
                self.assertFalse((root / 'fence').exists())
            work = root / 'failed-attempt'
            work.mkdir(mode=0o700)
            with patch.object(h, 'versions'), patch.object(v2, 'check'), \
                 patch.object(h, 'psql', side_effect=h.RehearsalError('stop and quarantine')):
                with self.assertRaisesRegex(h.RehearsalError, 'quarantine'):
                    v2.restore(cfg, work, archive, identity, root / 'fence')
            self.assertTrue((root / 'fence').exists())

    def test_catalog_validator_is_bounded_and_redacted(self):
        for raw in (b'{', b' ' * 65537, b'NaN', b'{"SECRET-CANARY":1}',
                    CATALOG.replace(b'"view": ""', b'"view": "\\ud800"'),
                    CATALOG.replace(b'"view": ""', b'"view": "\\u0000"'),
                    CATALOG.replace(b'"view": ""', b'"view": "", "view": "SECRET-CANARY"')):
            with self.subTest(raw=raw[:20]), self.assertRaisesRegex(h.RehearsalError, '^invalid expanded catalog$'):
                v2.validate_catalog(raw)

    def test_capture_refuses_clobber_and_partial_dump(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cfg, archive, identity = self.archive(root)
            with patch.object(v2, 'check') as check:
                with self.assertRaises(h.RehearsalError):
                    v2.capture(cfg, root, root, archive, 'age1'+'a'*58, identity)
                check.assert_not_called()
            with patch.object(h, 'versions'), patch.object(v2, 'check'), \
                 patch.object(v2, 'capture_metadata', return_value=json.dumps(fixture()).encode()), \
             patch.object(v2, 'capture_catalog', return_value=CATALOG), \
                 patch.object(v2.native, 'dump', return_value=b'partial'), patch.object(h, 'run') as run:
                with self.assertRaises(h.RehearsalError):
                    v2.capture(cfg, root, root, root / 'new', 'age1'+'a'*58, identity)
                run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
