"""Offline destination permission contract tests; no database or credentials."""
import copy
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import destination_permissions as permissions


def acl(grantee='reader', grant_option=False, grantor='owner', privilege='SELECT'):
    tagged = {'kind': 'public'} if grantee is None else {'kind': 'role', 'name': grantee}
    return {'grantor': grantor, 'grantee': tagged, 'privilege': privilege, 'grant_option': grant_option}


def contract():
    schemas, creators = ['public', 'app'], ['owner']
    return {
        'version': 1, 'server_version_num': 170009,
        'selection': {'schemas': ['app', 'public'], 'creating_roles': creators},
        'database': {'name': 'postgres', 'owner': 'owner', 'acl': [acl(None, privilege='CONNECT')],
                     'settings_present': False},
        'schemas': [
            {'name': 'app', 'present': False, 'owner': None, 'acl': []},
            {'name': 'public', 'present': True, 'owner': 'pg_database_owner',
             'acl': [acl(None, privilege='USAGE')]},
        ],
        'roles': [
            {'name': 'owner', 'superuser': False, 'inherit': True, 'createrole': False, 'createdb': False,
             'login': True, 'replication': False, 'bypassrls': False, 'settings_present': False},
            {'name': 'PUBLIC', 'superuser': False, 'inherit': True, 'createrole': False, 'createdb': False,
             'login': False, 'replication': False, 'bypassrls': False, 'settings_present': False},
            {'name': 'pg_database_owner', 'superuser': False, 'inherit': True, 'createrole': False, 'createdb': False,
             'login': False, 'replication': False, 'bypassrls': False, 'settings_present': False},
            {'name': 'reader', 'superuser': False, 'inherit': True, 'createrole': False, 'createdb': False,
             'login': True, 'replication': False, 'bypassrls': False, 'settings_present': False},
        ],
        'memberships': [{'role': 'reader', 'member': 'owner', 'grantor': 'owner',
                         'admin': False, 'inherit': True, 'set': True}],
        'default_acls': [{'creator': 'owner', 'schema': None, 'object_type': 'r', 'acl': []},
                         {'creator': 'owner', 'schema': 'public', 'object_type': 'r', 'acl': [acl()]}],
    }


class DestinationPermissionTests(unittest.TestCase):
    def validate(self, value=None):
        value = contract() if value is None else value
        return permissions.validate_response(json.dumps(value).encode(), ['public', 'app'], ['owner'])

    def test_literal_selection_is_hex_and_invalid_input_never_uses_transport(self):
        sql = permissions.observation_sql(['snow 雪', 'public'], ['owner name'])
        encoded = json.dumps({'schemas': ['public', 'snow 雪'], 'creating_roles': ['owner name']},
                             ensure_ascii=False, separators=(',', ':')).encode().hex()
        self.assertIn(encoded, sql)
        self.assertNotIn('LIKE', sql)
        self.assertIn('REPEATABLE READ READ ONLY', sql)
        self.assertIn('SET LOCAL search_path = pg_catalog', sql)
        with patch.object(permissions.h, 'versions') as versions, patch.object(permissions.h, 'psql') as psql:
            with self.assertRaises(permissions.DestinationPermissionError):
                permissions.observe({}, '.', ['public', 'public'], ['owner'])
        versions.assert_not_called(); psql.assert_not_called()

    def test_malformed_duplicate_oversized_and_scope_fail_closed(self):
        good = json.dumps(contract()).encode()
        bad = [b'{"version":1,"version":1}', b'\xff', b'NaN', good + b' ' * (permissions.LIMIT - len(good) + 1)]
        for raw in bad:
            with self.subTest(raw=raw[:20]):
                with self.assertRaises(permissions.DestinationPermissionError):
                    permissions.validate_response(raw, ['public', 'app'], ['owner'])
        value = contract(); value['selection']['schemas'] = ['public']
        with self.assertRaises(permissions.DestinationPermissionError): self.validate(value)
        value = contract(); value['default_acls'].append(copy.deepcopy(value['default_acls'][0]))
        with self.assertRaises(permissions.DestinationPermissionError): self.validate(value)
        value = contract(); value['roles'] *= 5001
        with self.assertRaises(permissions.DestinationPermissionError): self.validate(value)

    def test_order_is_canonical_but_acl_identity_and_public_tag_are_semantic(self):
        expected = self.validate()
        observed = copy.deepcopy(expected)
        for field in ('schemas', 'roles', 'memberships', 'default_acls'):
            observed[field].reverse()
        self.assertTrue(permissions.compare(expected, observed, ['app', 'public'], ['owner'])['prerequisite_catalog_matches'])
        observed = copy.deepcopy(expected)
        observed['schemas'][1]['acl'][0]['grantee'] = {'kind': 'role', 'name': 'PUBLIC'}
        report = permissions.compare(expected, observed, ['app', 'public'], ['owner'])
        self.assertFalse(report['prerequisite_catalog_matches'])
        self.assertIn('schemas', report['mismatch_categories'])

    def test_each_bounded_family_and_required_absence_mismatch(self):
        expected = self.validate()
        mutations = {
            'database': lambda x: x['database'].update(owner='reader'),
            'schemas': lambda x: x['schemas'][1]['acl'][0].update(grant_option=True),
            'roles': lambda x: x['roles'][0].update(bypassrls=True),
            'memberships': lambda x: x['memberships'][0].update(set=False),
            'default-acls': lambda x: x['default_acls'][0]['acl'].append(acl('reader')),
        }
        for category, mutate in mutations.items():
            with self.subTest(category=category):
                observed = copy.deepcopy(expected); mutate(observed)
                self.assertIn(category, permissions.compare(expected, observed, ['app', 'public'], ['owner'])['mismatch_categories'])
        observed = copy.deepcopy(expected); observed['schemas'][0].update(present=True, owner='owner')
        self.assertIn('schemas', permissions.compare(expected, observed, ['app', 'public'], ['owner'])['mismatch_categories'])
        observed = copy.deepcopy(expected); observed['schemas'][1].update(present=False, owner=None, acl=[])
        observed['default_acls'] = [item for item in observed['default_acls'] if item['schema'] != 'public']
        report = permissions.compare(expected, observed, ['app', 'public'], ['owner'])
        self.assertIn('required-schema-missing', report['mismatch_categories'])
        expected = copy.deepcopy(expected)
        expected['selection']['creating_roles'] = ['spare']
        expected['default_acls'] = []
        expected['roles'].append({'name': 'spare', 'superuser': False, 'inherit': True, 'createrole': False,
                                  'createdb': False, 'login': False, 'replication': False,
                                  'bypassrls': False, 'settings_present': False})
        observed = copy.deepcopy(expected); observed['roles'] = [x for x in observed['roles'] if x['name'] != 'spare']
        report = permissions.compare(expected, observed, ['app', 'public'], ['spare'])
        self.assertIn('selected-creator-missing', report['mismatch_categories'])

    def test_default_scope_empty_and_grant_details_do_not_normalize_away(self):
        expected = self.validate()
        for mutate in (
            lambda x: x['default_acls'].pop(0),
            lambda x: x['default_acls'].append({'creator': 'owner', 'schema': None, 'object_type': 'S', 'acl': []}),
            lambda x: x['default_acls'][1]['acl'][0].update(grantor='reader'),
            lambda x: x['default_acls'][1]['acl'][0].update(grant_option=True),
        ):
            observed = copy.deepcopy(expected); mutate(observed)
            self.assertIn('default-acls', permissions.compare(expected, observed, ['app', 'public'], ['owner'])['mismatch_categories'])

    def test_rejects_unresolved_identities_and_validates_membership_grantors(self):
        for mutate in (
            lambda x: x['database'].update(owner='missing'),
            lambda x: x['database']['acl'][0].update(grantor='missing'),
            lambda x: x['schemas'][1].update(owner='missing'),
            lambda x: x['schemas'][1]['acl'][0].update(grantee={'kind': 'role', 'name': 'missing'}),
            lambda x: x['memberships'][0].update(grantor='missing'),
            lambda x: x['default_acls'][1].update(creator='missing'),
            lambda x: x['default_acls'][1].update(schema='app'),
        ):
            with self.subTest(mutate=mutate):
                value = contract(); mutate(value)
                with self.assertRaises(permissions.DestinationPermissionError): self.validate(value)
        value = contract()
        second = dict(value['memberships'][0], grantor='reader')
        value['memberships'].append(second)
        self.assertEqual(len(self.validate(value)['memberships']), 2)
        value['memberships'].append(copy.deepcopy(second))
        with self.assertRaises(permissions.DestinationPermissionError): self.validate(value)

    def test_rejects_context_invalid_privileges_and_dictionary_coercions_without_mutation(self):
        for mutate in (
            lambda x: x['database']['acl'][0].update(privilege='BANANA'),
            lambda x: x['schemas'][1]['acl'][0].update(privilege='SELECT'),
            lambda x: x['default_acls'][1]['acl'][0].update(privilege='EXECUTE'),
        ):
            value = contract(); mutate(value)
            with self.assertRaises(permissions.DestinationPermissionError): self.validate(value)
        expected = self.validate()
        observed = copy.deepcopy(expected)
        for value in (expected, observed):
            for family in ('schemas', 'roles', 'memberships', 'default_acls'):
                value[family].reverse()
            value['database']['acl'].reverse()
            for schema in value['schemas']: schema['acl'].reverse()
            for default in value['default_acls']: default['acl'].reverse()
        expected_before, observed_before = copy.deepcopy(expected), copy.deepcopy(observed)
        self.assertTrue(permissions.compare(expected, observed, ['app', 'public'], ['owner'])['prerequisite_catalog_matches'])
        self.assertEqual(expected, expected_before)
        self.assertEqual(observed, observed_before)
        for bad in (
            tuple(contract().items()),
            dict(contract(), schemas=tuple(contract()['schemas'])),
            {1: 'coerced-key'},
            {'loop': None},
        ):
            if bad == {'loop': None}: bad['loop'] = bad
            with self.subTest(kind=type(bad)):
                with self.assertRaises(permissions.DestinationPermissionError):
                    permissions.validate_expected(bad, ['app', 'public'], ['owner'])
        oversized = contract(); oversized['database']['name'] = 'x' * (permissions.LIMIT + 1)
        with self.assertRaises(permissions.DestinationPermissionError):
            permissions.compare(self.validate(), oversized, ['app', 'public'], ['owner'])

    def test_report_never_claims_readiness(self):
        report = permissions.compare(self.validate(), self.validate(), ['app', 'public'], ['owner'])
        self.assertTrue(report['prerequisite_catalog_matches'])
        self.assertFalse(report['export_ready']); self.assertFalse(report['restore_verified']); self.assertFalse(report['execution_supported'])
        self.assertIn('expected-contract-provenance-and-review', report['mandatory_unknowns'])


if __name__ == '__main__':
    unittest.main()
