"""Bounded, read-only PG17 destination permission prerequisite comparison.

A matching caller-supplied contract is not restore authorization.
"""
import json

import application_inventory as inventory
import hosted_rehearsal as h

LIMIT = 2 * 1024 * 1024
MAX_RECORDS = 10000
VERSION = 1
UNKNOWN = (
    'dependency-closure', 'object-collisions', 'archive-provenance',
    'expected-contract-provenance-and-review', 'role-and-session-configuration-values',
    'effective-privilege-behavior', 'provider-baseline-compatibility',
    'snapshot-concurrency', 'managed-services',
)
ROLE_FLAGS = ('superuser', 'inherit', 'createrole', 'createdb', 'login', 'replication', 'bypassrls')
OBJECT_TYPES = ('r', 'S', 'f', 'T', 'n')
DATABASE_PRIVILEGES = frozenset(('CREATE', 'CONNECT', 'TEMPORARY'))
NAMESPACE_PRIVILEGES = frozenset(('CREATE', 'USAGE'))
DEFAULT_PRIVILEGES = {
    'r': frozenset(('DELETE', 'INSERT', 'REFERENCES', 'SELECT', 'TRIGGER', 'TRUNCATE', 'UPDATE', 'MAINTAIN')),
    'S': frozenset(('SELECT', 'UPDATE', 'USAGE')),
    'f': frozenset(('EXECUTE',)),
    'T': frozenset(('USAGE',)),
    'n': NAMESPACE_PRIVILEGES,
}


class DestinationPermissionError(Exception):
    """Fixed, non-sensitive validation error."""


def require(condition, message='invalid destination permission contract'):
    if not condition:
        raise DestinationPermissionError(message)


def _name(value):
    require(type(value) is str and '\x00' not in value)
    try:
        require(0 < len(value.encode('utf-8')) <= 63)
    except UnicodeError:
        raise DestinationPermissionError('invalid destination permission contract') from None
    return value


def validate_schemas(schemas):
    try:
        return inventory.validate_schemas(schemas)
    except inventory.InspectionError:
        raise DestinationPermissionError('invalid schema selection') from None


def validate_creating_roles(roles):
    require(type(roles) is list and 1 <= len(roles) <= 32, 'invalid creating role selection')
    names = [_name(role) for role in roles]
    require(len(set(names)) == len(names), 'invalid creating role selection')
    return sorted(names)


def _selection(schemas, creating_roles):
    return {'schemas': validate_schemas(schemas), 'creating_roles': validate_creating_roles(creating_roles)}


def _selection_hex(schemas, creating_roles):
    return json.dumps(_selection(schemas, creating_roles), ensure_ascii=False,
                      separators=(',', ':')).encode('utf-8').hex()


def observation_sql(schemas, creating_roles):
    """Fixed PG17 catalog-only query; all selected names are a JSON hex datum."""
    selected = _selection_hex(schemas, creating_roles)
    return f'''BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path = pg_catalog;
DO $check$ BEGIN
 IF current_setting('transaction_read_only') <> 'on'
    OR current_setting('transaction_isolation') <> 'repeatable read' THEN
  RAISE EXCEPTION 'invalid transaction properties';
 END IF;
END $check$;
WITH input AS (
 SELECT convert_from(decode('{selected}','hex'),'UTF8')::jsonb AS value
), selected_schemas AS (
 SELECT j.value AS name FROM input, jsonb_array_elements_text(input.value->'schemas') AS j(value)
), selected_creators AS (
 SELECT j.value AS name FROM input, jsonb_array_elements_text(input.value->'creating_roles') AS j(value)
), result AS (
 SELECT jsonb_build_object(
  'version', 1,
  'server_version_num', current_setting('server_version_num')::int,
  'selection', (SELECT value FROM input),
  'database', (SELECT jsonb_build_object(
   'name', d.datname, 'owner', owner.rolname,
   'acl', COALESCE((SELECT jsonb_agg(jsonb_build_object('grantor', grantor.rolname,
       'grantee', CASE WHEN x.grantee = 0 THEN jsonb_build_object('kind','public')
                       ELSE jsonb_build_object('kind','role','name', grantee.rolname) END,
       'privilege', x.privilege_type, 'grant_option', x.is_grantable)
       ORDER BY grantor.rolname, x.grantee, x.privilege_type, x.is_grantable)
    FROM aclexplode(COALESCE(d.datacl, acldefault('d'::"char", d.datdba))) x
    LEFT JOIN pg_roles grantor ON grantor.oid=x.grantor
    LEFT JOIN pg_roles grantee ON grantee.oid=x.grantee), '[]'::jsonb),
   'settings_present', EXISTS(SELECT FROM pg_db_role_setting s WHERE s.setdatabase=d.oid AND s.setrole=0))
   FROM pg_database d LEFT JOIN pg_roles owner ON owner.oid=d.datdba WHERE d.datname=current_database()),
  'schemas', COALESCE((SELECT jsonb_agg(jsonb_build_object('name', s.name,
    'present', n.oid IS NOT NULL, 'owner', owner.rolname,
    'acl', CASE WHEN n.oid IS NULL THEN '[]'::jsonb ELSE COALESCE((SELECT jsonb_agg(jsonb_build_object(
       'grantor', grantor.rolname, 'grantee', CASE WHEN x.grantee=0 THEN jsonb_build_object('kind','public')
         ELSE jsonb_build_object('kind','role','name',grantee.rolname) END,
       'privilege',x.privilege_type,'grant_option',x.is_grantable)
       ORDER BY grantor.rolname,x.grantee,x.privilege_type,x.is_grantable)
       FROM aclexplode(COALESCE(n.nspacl,acldefault('n'::"char",n.nspowner))) x
       LEFT JOIN pg_roles grantor ON grantor.oid=x.grantor LEFT JOIN pg_roles grantee ON grantee.oid=x.grantee), '[]'::jsonb) END)
    ORDER BY s.name) FROM selected_schemas s LEFT JOIN pg_namespace n ON n.nspname=s.name
    LEFT JOIN pg_roles owner ON owner.oid=n.nspowner), '[]'::jsonb),
  'roles', COALESCE((SELECT jsonb_agg(jsonb_build_object('name',r.rolname,
    'superuser',r.rolsuper,'inherit',r.rolinherit,'createrole',r.rolcreaterole,'createdb',r.rolcreatedb,
    'login',r.rolcanlogin,'replication',r.rolreplication,'bypassrls',r.rolbypassrls,
    'settings_present',EXISTS(SELECT FROM pg_db_role_setting s WHERE s.setrole=r.oid)) ORDER BY r.rolname)
    FROM pg_roles r), '[]'::jsonb),
  'memberships', COALESCE((SELECT jsonb_agg(jsonb_build_object('role',role.rolname,'member',member.rolname,
    'grantor',grantor.rolname,'admin',m.admin_option,'inherit',m.inherit_option,'set',m.set_option)
    ORDER BY role.rolname,member.rolname,grantor.rolname) FROM pg_auth_members m
    LEFT JOIN pg_roles role ON role.oid=m.roleid LEFT JOIN pg_roles member ON member.oid=m.member
    LEFT JOIN pg_roles grantor ON grantor.oid=m.grantor), '[]'::jsonb),
  'default_acls', COALESCE((SELECT jsonb_agg(jsonb_build_object('creator',creator.rolname,
    'schema',CASE WHEN d.defaclnamespace=0 THEN NULL ELSE n.nspname END,'object_type',d.defaclobjtype::text,
    'acl',COALESCE((SELECT jsonb_agg(jsonb_build_object('grantor',grantor.rolname,
      'grantee',CASE WHEN x.grantee=0 THEN jsonb_build_object('kind','public')
        ELSE jsonb_build_object('kind','role','name',grantee.rolname) END,
      'privilege',x.privilege_type,'grant_option',x.is_grantable)
      ORDER BY grantor.rolname,x.grantee,x.privilege_type,x.is_grantable) FROM aclexplode(d.defaclacl) x
      LEFT JOIN pg_roles grantor ON grantor.oid=x.grantor LEFT JOIN pg_roles grantee ON grantee.oid=x.grantee),'[]'::jsonb))
    ORDER BY creator.rolname,n.nspname NULLS FIRST,d.defaclobjtype
    ) FROM pg_default_acl d JOIN pg_roles creator ON creator.oid=d.defaclrole
    LEFT JOIN pg_namespace n ON n.oid=d.defaclnamespace JOIN selected_creators c ON c.name=creator.rolname
    LEFT JOIN selected_schemas s ON s.name=n.nspname
    WHERE d.defaclnamespace=0 OR s.name IS NOT NULL), '[]'::jsonb)
 ) AS value
)
SELECT value::text FROM result;
ROLLBACK;
'''


def _pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise DestinationPermissionError('invalid destination permission response')
        result[key] = value
    return result


def _parse(raw):
    require(type(raw) is bytes and len(raw) <= LIMIT, 'invalid destination permission response')
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=_pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (UnicodeError, ValueError, RecursionError):
        raise DestinationPermissionError('invalid destination permission response') from None


def _acl(entry, privileges):
    require(type(entry) is dict and set(entry) == {'grantor', 'grantee', 'privilege', 'grant_option'})
    _name(entry['grantor']); require(type(entry['privilege']) is str and entry['privilege'] in privileges)
    require(type(entry['grant_option']) is bool)
    grantee = entry['grantee']; require(type(grantee) is dict and 'kind' in grantee)
    if grantee.get('kind') == 'public':
        require(set(grantee) == {'kind'})
    else:
        require(set(grantee) == {'kind', 'name'} and grantee['kind'] == 'role')
        _name(grantee['name'])
    return entry


def _sort_unique(items, identity):
    seen = set()
    for item in items:
        key = identity(item)
        require(key not in seen, 'invalid destination permission response')
        seen.add(key)
    return sorted(items, key=identity)


def _acl_list(value, privileges):
    require(type(value) is list and len(value) <= MAX_RECORDS)
    for entry in value: _acl(entry, privileges)
    return _sort_unique(value, lambda x: (x['grantor'], x['grantee']['kind'], x['grantee'].get('name', ''),
                                          x['privilege']))


def _acl_roles(entries, role_names):
    for entry in entries:
        require(entry['grantor'] in role_names)
        if entry['grantee']['kind'] == 'role': require(entry['grantee']['name'] in role_names)


def _dictionary_raw(value):
    def walk(item, depth=0):
        require(depth <= 32, 'invalid destination permission response')
        if type(item) is dict:
            for key, child in item.items():
                require(type(key) is str, 'invalid destination permission response')
                walk(child, depth + 1)
        elif type(item) is list:
            for child in item: walk(child, depth + 1)
        else:
            require(type(item) in (str, int, bool) or item is None, 'invalid destination permission response')
    walk(value)
    try:
        raw = json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode('utf-8')
    except (TypeError, UnicodeError, ValueError, RecursionError):
        raise DestinationPermissionError('invalid destination permission response') from None
    require(len(raw) <= LIMIT, 'invalid destination permission response')
    return raw


def _data(value, schemas, creating_roles, expected=False):
    if type(value) is bytes:
        return _contract(_parse(value), schemas, creating_roles, expected)
    require(type(value) is dict, 'invalid destination permission response')
    return _contract(_parse(_dictionary_raw(value)), schemas, creating_roles, expected)


def _contract(value, schemas, creating_roles, expected=False):
    require(type(value) is dict and set(value) == {'version', 'server_version_num', 'selection', 'database',
                                                    'schemas', 'roles', 'memberships', 'default_acls'},
            'invalid destination permission response')
    require(type(value['version']) is int and value['version'] == VERSION)
    require(type(value['server_version_num']) is int and 170000 <= value['server_version_num'] < 180000)
    selection = _selection(schemas, creating_roles)
    require(value['selection'] == selection, 'invalid destination permission response')
    database = value['database']; require(type(database) is dict and set(database) == {'name','owner','acl','settings_present'})
    _name(database['name']); _name(database['owner']); require(type(database['settings_present']) is bool)
    database['acl'] = _acl_list(database['acl'], DATABASE_PRIVILEGES)
    require(type(value['schemas']) is list and len(value['schemas']) == len(selection['schemas']))
    schema_names = set()
    for item in value['schemas']:
        require(type(item) is dict and set(item) == {'name','present','owner','acl'})
        _name(item['name']); require(item['name'] in selection['schemas'] and item['name'] not in schema_names)
        schema_names.add(item['name']); require(type(item['present']) is bool)
        require((type(item['owner']) is str) if item['present'] else item['owner'] is None)
        if item['present']: _name(item['owner'])
        item['acl'] = _acl_list(item['acl'], NAMESPACE_PRIVILEGES)
        require(item['present'] or not item['acl'])
    value['schemas'].sort(key=lambda x: x['name'])
    schema_by_name = {item['name']: item for item in value['schemas']}
    require(type(value['roles']) is list and len(value['roles']) <= MAX_RECORDS)
    for role in value['roles']:
        require(type(role) is dict and set(role) == {'name', *ROLE_FLAGS, 'settings_present'})
        _name(role['name']); require(all(type(role[key]) is bool for key in (*ROLE_FLAGS, 'settings_present')))
    value['roles'] = _sort_unique(value['roles'], lambda x: x['name'])
    role_names = {x['name'] for x in value['roles']}
    require(database['owner'] in role_names); _acl_roles(database['acl'], role_names)
    for item in value['schemas']:
        if item['present']: require(item['owner'] in role_names)
        _acl_roles(item['acl'], role_names)
    if expected: require(set(selection['creating_roles']) <= role_names)
    require(type(value['memberships']) is list and len(value['memberships']) <= MAX_RECORDS)
    for edge in value['memberships']:
        require(type(edge) is dict and set(edge) == {'role','member','grantor','admin','inherit','set'})
        for key in ('role','member','grantor'):
            _name(edge[key]); require(edge[key] in role_names)
        require(all(type(edge[key]) is bool for key in ('admin','inherit','set')))
    value['memberships'] = _sort_unique(value['memberships'], lambda x: (x['role'], x['member'], x['grantor']))
    require(type(value['default_acls']) is list and len(value['default_acls']) <= MAX_RECORDS)
    for record in value['default_acls']:
        require(type(record) is dict and set(record) == {'creator','schema','object_type','acl'})
        _name(record['creator']); require(record['creator'] in role_names and record['creator'] in selection['creating_roles'])
        require(record['schema'] is None or record['schema'] in selection['schemas'])
        if record['schema'] is not None: require(schema_by_name[record['schema']]['present'])
        require(record['object_type'] in OBJECT_TYPES)
        record['acl'] = _acl_list(record['acl'], DEFAULT_PRIVILEGES[record['object_type']])
        _acl_roles(record['acl'], role_names)
    value['default_acls'] = _sort_unique(value['default_acls'], lambda x: (x['creator'], x['schema'] or '', x['object_type']))
    require(sum(len(item['acl']) for item in [database, *value['schemas'], *value['default_acls']]) <= MAX_RECORDS)
    return value


def validate_response(raw, schemas, creating_roles):
    return _contract(_parse(raw), schemas, creating_roles)


def validate_expected(value, schemas, creating_roles):
    return _data(value, schemas, creating_roles, expected=True)


def observe(cfg, work, schemas, creating_roles):
    """Observe through the existing protected psql transport only."""
    _selection(schemas, creating_roles)  # fail before any transport call
    h.versions(cfg, work)
    return validate_response(h.psql(cfg, work, observation_sql(schemas, creating_roles)), schemas, creating_roles)


def compare(expected, observed, schemas, creating_roles):
    """Compare independently supplied expected bounded catalog data; never authorize restore."""
    expected = validate_expected(expected, schemas, creating_roles)
    observed = _data(observed, schemas, creating_roles)
    categories = []
    if expected['server_version_num'] != observed['server_version_num']:
        categories.append('server-version')
    for key, category in (
        ('database', 'database'), ('schemas', 'schemas'), ('roles', 'roles'),
        ('memberships', 'memberships'), ('default_acls', 'default-acls'),
    ):
        if expected[key] != observed[key]: categories.append(category)
    observed_roles = {role['name'] for role in observed['roles']}
    if any(role not in observed_roles for role in creating_roles): categories.append('selected-creator-missing')
    if any(not item['present'] and next(x for x in expected['schemas'] if x['name'] == item['name'])['present']
           for item in observed['schemas']): categories.append('required-schema-missing')
    return {'prerequisite_catalog_matches': not categories,
            'export_ready': False, 'restore_verified': False, 'execution_supported': False,
            'mismatch_categories': categories, 'mandatory_unknowns': list(UNKNOWN)}
