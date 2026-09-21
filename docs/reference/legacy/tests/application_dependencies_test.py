"""Offline contract tests for bounded application dependency observation."""
import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import application_dependencies as dependencies


def edge(**changes):
    value = {
        'source_kind':'relation','source_schema':'app','source_name':'items','source_owner_kind':None,'source_owner':None,'source_subname':None,'source_arguments':None,
        'target_kind':'relation','target_schema':'outside','target_name':'parent','target_owner_kind':None,'target_owner':None,'target_subname':None,'target_arguments':None,
        'dependency_type':'n','source_extension':None,'target_extension':None,'unresolved':False,
        'target_local_class_id':None,'target_local_object_id':None,'target_local_subobject_id':None,
    }
    value.update(changes)
    return value


def raw(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode()


class ApplicationDependencyTests(unittest.TestCase):
    def response(self): return dependencies.empty_response(['app'])
    def validate(self, value=None): return dependencies.validate_response(raw(self.response() if value is None else value), ['app'])

    def test_literal_selection_is_hex_and_invalid_selection_skips_transport(self):
        sql = dependencies.observation_sql(['a b', '雪"'])
        encoded = json.dumps(['a b', '雪"'], ensure_ascii=False, separators=(',', ':')).encode().hex()
        self.assertIn(encoded, sql); self.assertNotIn('LIKE', sql)
        self.assertIn('REPEATABLE READ READ ONLY', sql); self.assertIn('SET LOCAL search_path = pg_catalog', sql)
        self.assertNotIn('WHERE NOT t.tgisinternal', sql)
        with tempfile.TemporaryDirectory() as work, patch.object(dependencies.h, 'versions') as versions, patch.object(dependencies.h, 'psql') as psql:
            Path(work).chmod(0o700)
            with self.assertRaises(dependencies.DependencyInspectionError): dependencies.observe({}, work, ['auth'])
        versions.assert_not_called(); psql.assert_not_called()

    def test_response_rejects_duplicate_keys_types_size_and_absent_schemas(self):
        good = raw(self.response())
        for bad in (b'{"version":1,"version":1}', b'\xff', b'NaN', good + b' ' * (dependencies.LIMIT - len(good) + 1)):
            with self.subTest(bad=bad[:20]):
                with self.assertRaises(dependencies.DependencyInspectionError): dependencies.validate_response(bad, ['app'])
        for mutate in (lambda x: x.update(schemas=[]), lambda x: x['transaction'].update(read_only='off'), lambda x: x.update(edges=[edge()] * 10001)):
            value = self.response(); mutate(value)
            with self.subTest(mutate=mutate):
                with self.assertRaises(dependencies.DependencyInspectionError): self.validate(value)

    def test_kind_specific_identities_and_local_only_unresolved_addresses(self):
        value = self.response(); value['edges'] = [edge(source_kind='column', source_subname='id'), edge(source_kind='routine', source_name='f', source_arguments='integer', target_name='other')]
        self.assertEqual(len(self.validate(value)['edges']), 2)
        unknown_one = edge(target_kind='unresolved', target_schema=None, target_name=None, unresolved=True, target_local_class_id=2617, target_local_object_id=1, target_local_subobject_id=0)
        unknown_two = dict(unknown_one, target_local_object_id=2)
        value = self.response(); value['edges'] = [unknown_one, unknown_two]
        self.assertEqual(len(self.validate(value)['edges']), 2)
        value = self.response(); value['edges'] = [dict(unknown_one, target_local_subobject_id=-1)]
        with self.assertRaises(dependencies.DependencyInspectionError): self.validate(value)
        for mutate in (
            lambda x: x['edges'][0].update(source_schema='outside'),
            lambda x: x['edges'][0].update(source_kind='column', source_subname=None),
            lambda x: x['edges'][0].update(source_kind='routine', source_arguments=None),
            lambda x: x['edges'][0].update(target_kind='column', target_subname=None),
            lambda x: x['edges'][0].update(target_kind='unresolved'),
            lambda x: x['edges'][0].update(unresolved=True),
            lambda x: x['edges'][0].update(source_name='x\x00'),
            lambda x: x['edges'][0].update(target_schema=None),
        ):
            bad = self.response(); bad['edges'] = [edge()]; mutate(bad)
            with self.subTest(mutate=mutate):
                with self.assertRaises(dependencies.DependencyInspectionError): self.validate(bad)

    def test_constraint_owner_and_extension_membership_consistency(self):
        constraint = edge(source_kind='constraint', source_name='same_fk', source_owner_kind='relation', source_owner='left_one', target_kind='constraint', target_name='same_fk', target_owner_kind='relation', target_owner='right_one')
        other = dict(constraint, source_owner='left_two', target_owner='right_two')
        value = self.response(); value['edges'] = [constraint, other]
        self.assertEqual(len(self.validate(value)['edges']), 2)
        extension_edge = edge(source_extension='hstore')
        cross_side = edge(source_name='other', target_schema='app', target_name='items', target_extension='hstore')
        source_member = {'side':'source','kind':'relation','schema':'app','name':'items','owner_kind':None,'owner':None,'subname':None,'arguments':None,'local_class_id':None,'local_object_id':None,'local_subobject_id':None,'extension':'hstore'}
        target_member = dict(source_member, side='target')
        value = self.response(); value['edges'] = [extension_edge, cross_side]; value['memberships'] = [source_member, target_member]
        self.assertEqual(self.validate(value)['memberships'], [source_member, target_member])
        bad = self.response(); bad['edges'] = [copy.deepcopy(constraint)]
        bad['edges'][0].update(source_owner=None)
        with self.assertRaises(dependencies.DependencyInspectionError): self.validate(bad)
        for mutate in (
            lambda x: x.update(memberships=[]),
            lambda x: x['memberships'][0].update(schema='outside'),
            lambda x: (x['edges'][1].update(target_extension='other'), x['memberships'][1].update(extension='other')),
            lambda x: (x['edges'][1].update(target_extension=None), x['memberships'].pop(1)),
        ):
            bad = self.response(); bad['edges'] = [copy.deepcopy(extension_edge), copy.deepcopy(cross_side)]; bad['memberships'] = [copy.deepcopy(source_member), copy.deepcopy(target_member)]; mutate(bad)
            with self.subTest(mutate=mutate):
                with self.assertRaises(dependencies.DependencyInspectionError): self.validate(bad)

    def test_assessment_bytes_only_is_deterministic_and_never_ready(self):
        value = self.response()
        value['edges'] = [edge(target_schema='app'), edge(target_schema='auth', target_name='users'), edge(target_schema='pg_catalog', target_name='int4'), edge(target_schema='app', target_name='hstore', target_extension='hstore'), edge(target_kind='unresolved', target_schema=None, target_name=None, unresolved=True, target_local_class_id=2617, target_local_object_id=1, target_local_subobject_id=0)]
        target_member = {'side':'target','kind':'relation','schema':'app','name':'hstore','owner_kind':None,'owner':None,'subname':None,'arguments':None,'local_class_id':None,'local_object_id':None,'local_subobject_id':None,'extension':'hstore'}
        value['memberships'] = [target_member]
        report = dependencies.assess(raw(value), ['app']); codes = [item['code'] for item in report['findings']]
        self.assertEqual(sorted(codes), codes)
        self.assertTrue({'internal','protected-external','system-prerequisite','extension-requirement','unresolved'} <= set(codes))
        self.assertFalse(report['execution_supported']); self.assertFalse(report['export_ready']); self.assertFalse(report['restore_verified']); self.assertFalse(report['dependency_analysis_complete'])
        self.assertIn('dynamic-and-string-bodied-routine-dependencies', report['mandatory_unknowns'])
        with self.assertRaises(dependencies.DependencyInspectionError): dependencies.assess(value, ['app'])
        self.assertEqual(value['edges'][0]['target_schema'], 'app')

    def test_missing_selected_schema_fails_before_assessment(self):
        value = self.response(); value['schemas'] = []
        with self.assertRaises(dependencies.DependencyInspectionError): dependencies.assess(raw(value), ['app'])


if __name__ == '__main__': unittest.main()
