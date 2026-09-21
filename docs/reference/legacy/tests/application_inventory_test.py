"""Unit tests for application database discovery; no database or credentials."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import application_inventory as inventory


class ApplicationInventoryTest(unittest.TestCase):
    def test_selection_is_literal_validated_and_sorted(self):
        names = ['雪"_*', 'plain']
        self.assertEqual(inventory.validate_schemas(names), ['plain', '雪"_*'])
        sql = inventory.inspection_sql(names)
        self.assertIn(json.dumps(['plain', '雪"_*'], ensure_ascii=False, separators=(',', ':')).encode().hex(), sql)
        self.assertNotIn('LIKE', sql)
        for bad in ([], ['public', 'public'], ['pg_catalog'], ['auth'], ['bad\x00name'], ["\ud800"]):
            with self.subTest(bad=bad):
                with self.assertRaises(inventory.InspectionError):
                    inventory.validate_schemas(bad)

    def test_exact_selection_and_raw_response_boundaries(self):
        names = [f's{i}' for i in range(31)] + ['雪' * 21]
        self.assertEqual(len(inventory.validate_schemas(names)), 32)
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_schemas(names + ['extra'])
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_schemas(['雪' * 22])
        raw = json.dumps(inventory.empty_response(['plain'])).encode()
        self.assertEqual(inventory.validate_response(raw + b' ' * (inventory.LIMIT - len(raw)), ['plain'])['version'], 1)
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(raw + b' ' * (inventory.LIMIT - len(raw) + 1), ['plain'])

    def test_invalid_json_encoding_and_depth_are_rejected(self):
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(b'\xff', ['plain'])
        raw = inventory.empty_response(['plain'])
        nested = []
        for _ in range(33):
            nested = [nested]
        raw['policies'] = nested
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])

    def test_invalid_selection_never_runs_tools_or_database(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory).chmod(0o700)
            with patch.object(inventory.h, 'versions') as versions, patch.object(inventory.h, 'psql') as psql:
                with self.assertRaises(inventory.InspectionError):
                    inventory.inspect({}, directory, ['public', 'public'])
            versions.assert_not_called()
            psql.assert_not_called()

    def test_response_rejects_duplicates_wrong_fields_and_overflow(self):
        good = inventory.empty_response(['plain'])
        text = json.dumps(good)
        self.assertEqual(inventory.validate_response(text.encode(), ['plain']), good)
        for bad in (
            b'{"version":1,"version":1}',
            b'{"version":1}',
            text.replace('"version": 1', '"version": true').encode(),
            json.dumps(dict(good, extra=[])).encode(),
            json.dumps(dict(good, schemas=[])).encode(),
            json.dumps(dict(good, relations=[{}] * 10001)).encode(),
        ):
            with self.subTest(bad=bad[:30]):
                with self.assertRaises(inventory.InspectionError):
                    inventory.validate_response(bad, ['plain'])

    def test_response_requires_pg17_and_rejects_conflicting_object_identities(self):
        raw = inventory.empty_response(['plain'])
        raw['server_version_num'] = 160999
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])
        raw = inventory.empty_response(['plain'])
        relation = {'schema': 'plain', 'name': 'same', 'kind': 'r', 'owner': 'postgres',
                    'persistence': 'p', 'partitioned': False, 'rls': False, 'force_rls': False}
        raw['relations'] = [relation, dict(relation, owner='other')]
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])

    def test_routine_and_index_identity_conflicts_and_unsigned_oid_bounds_are_rejected(self):
        raw = inventory.empty_response(['plain'])
        routine = {'schema': 'plain', 'name': 'same', 'kind': 'f', 'argument_oids': [23], 'language': 'sql',
                   'owner': 'postgres', 'security_definer': False, 'sql_body': False}
        raw['routines'] = [routine, dict(routine, kind='p')]
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])
        raw = inventory.empty_response(['plain'])
        index = {'schema': 'plain', 'relation': 'one', 'name': 'same', 'valid': True, 'ready': True,
                 'unique': False, 'expression': False, 'predicate': False}
        raw['indexes'] = [index, dict(index, relation='two')]
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])
        raw = inventory.empty_response(['plain'])
        raw['routines'] = [dict(routine, argument_oids=[4294967295])]
        self.assertEqual(inventory.validate_response(json.dumps(raw).encode(), ['plain'])['routines'][0]['argument_oids'], [4294967295])
        raw['routines'][0]['argument_oids'] = [4294967296]
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])

    def test_response_rejects_invalid_catalog_flag_values(self):
        raw = inventory.empty_response(['plain'])
        raw['relations'] = [{'schema': 'plain', 'name': 'bad', 'kind': 'not-a-kind', 'owner': 'postgres',
                             'persistence': 'p', 'partitioned': False, 'rls': False, 'force_rls': False}]
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])

    def test_unknown_relation_kind_is_retained_as_blocker(self):
        raw = inventory.empty_response(['plain'])
        raw['relations'] = [{'schema': 'plain', 'name': 'odd', 'kind': 'z', 'owner': 'postgres',
                             'persistence': 'p', 'partitioned': False, 'rls': False, 'force_rls': False}]
        assessment = inventory.assess(raw, ['plain'])
        self.assertTrue(any(f['code'] == 'unknown-relation-kind' for f in assessment['findings']))

    def test_overloads_domains_and_extension_schema_members_have_distinct_identities(self):
        raw = inventory.empty_response(['plain'])
        routine = {'schema': 'plain', 'name': 'overload', 'kind': 'f', 'argument_oids': [23], 'language': 'sql',
                   'owner': 'postgres', 'security_definer': False, 'sql_body': False}
        raw['routines'] = [routine, dict(routine, argument_oids=[25])]
        constraint = {'schema': 'plain', 'relation': None, 'domain': 'one', 'name': 'same', 'type': 'c',
                      'validated': True, 'deferrable': False, 'deferred': False, 'foreign_schema': None}
        raw['constraints'] = [constraint, dict(constraint, domain='two')]
        member = {'object_kind': 'schema', 'schema': 'plain', 'name': 'plain', 'identity': 'plain', 'extension': 'one'}
        raw['extensions'] = [member, {'object_kind': 'collation', 'schema': 'plain', 'name': 'c',
                                      'identity': 'plain.c', 'extension': 'one'}]
        self.assertEqual({x['object_kind'] for x in inventory.validate_response(json.dumps(raw).encode(), ['plain'])['extensions']}, {'schema', 'collation'})
        raw['extensions'].append(dict(member, extension='two'))
        with self.assertRaises(inventory.InspectionError):
            inventory.validate_response(json.dumps(raw).encode(), ['plain'])

    def test_owner_and_security_definer_have_distinct_structured_findings(self):
        raw = inventory.empty_response(['plain'])
        raw['schemas'][0]['owner'] = 'other'
        raw['routines'] = [{'schema': 'plain', 'name': 'same', 'kind': 'f', 'argument_oids': [23], 'language': 'sql',
                            'owner': 'other', 'security_definer': True, 'sql_body': False}]
        raw['types'] = [{'schema': 'plain', 'name': 'custom', 'kind': 'd', 'owner': 'other'}]
        codes = [finding['code'] for finding in inventory.assess(raw, ['plain'])['findings']]
        self.assertIn('security-definer-routine-review', codes)
        self.assertGreaterEqual(codes.count('non-postgres-ownership'), 3)

    def test_assessment_is_never_ready_and_keeps_blockers(self):
        raw = inventory.empty_response(['plain'])
        raw['relations'] = [{'schema': 'plain', 'name': 'remote', 'kind': 'f', 'owner': 'other',
                             'persistence': 'p', 'partitioned': False, 'rls': False, 'force_rls': False}]
        raw['routines'] = [{'schema': 'plain', 'name': 'secret_fn', 'kind': 'f', 'argument_oids': [], 'language': 'sql',
                            'owner': 'postgres', 'security_definer': False, 'sql_body': True}]
        assessment = inventory.assess(raw, ['plain'])
        self.assertEqual(assessment['execution_supported'], False)
        self.assertEqual(assessment['export_ready'], False)
        self.assertEqual(assessment['restore_verified'], False)
        self.assertEqual(assessment['dependency_analysis_complete'], False)
        self.assertEqual(assessment['routine_argument_oids_portable_identities'], False)
        self.assertIn('unobserved-privilege-graphs', assessment['unknowns'])
        self.assertTrue(any(f['code'] == 'foreign-table' for f in assessment['findings']))
        self.assertTrue(any(f['code'] == 'routine-review' for f in assessment['findings']))

    def test_private_path_failures_and_transport_error_are_redacted_before_or_at_transport(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            link = root / 'link'
            link.symlink_to(root, target_is_directory=True)
            with patch.object(inventory.h, 'versions') as versions:
                with self.assertRaises(inventory.InspectionError):
                    inventory.inspect({}, link, ['plain'])
            versions.assert_not_called()
            public = root / 'public'
            public.mkdir(mode=0o755)
            public.chmod(0o755)
            with patch.object(inventory.h, 'versions') as versions:
                with self.assertRaises(inventory.InspectionError):
                    inventory.inspect_to_file({}, root, ['plain'], public / 'out.json')
            versions.assert_not_called()
            output_link = root / 'output-link.json'
            output_link.symlink_to(root / 'missing-target.json')
            with patch.object(inventory.h, 'versions') as versions:
                with self.assertRaises(inventory.InspectionError):
                    inventory.inspect_to_file({}, root, ['plain'], output_link)
            versions.assert_not_called()
            with patch.object(inventory.h, 'versions', side_effect=inventory.h.RehearsalError('secret supplied value')):
                with self.assertRaisesRegex(inventory.InspectionError, '^inspection transport failed$'):
                    inventory.inspect({}, root, ['plain'])

    def test_persistence_creates_new_private_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            output = root / 'inventory.json'
            response = json.dumps(inventory.empty_response(['plain'])).encode()
            with patch.object(inventory.h, 'versions'), patch.object(inventory.h, 'psql', return_value=response):
                result = inventory.inspect_to_file({}, root, ['plain'], output)
            self.assertFalse(result['execution_supported'])
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertEqual(json.loads(output.read_text())['selected_schemas'], ['plain'])

    def test_invalid_output_and_nonprivate_output_fail_without_database(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            root.chmod(0o700)
            output = root / 'existing.json'
            output.write_text('{}')
            output.chmod(0o600)
            with patch.object(inventory.h, 'versions') as versions:
                with self.assertRaises(inventory.InspectionError):
                    inventory.inspect_to_file({}, root, ['plain'], output)
            versions.assert_not_called()


if __name__ == '__main__':
    unittest.main()
