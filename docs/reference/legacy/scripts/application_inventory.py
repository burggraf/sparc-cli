"""Read-only, catalog-only inventory of explicitly selected application schemas."""
import json
import os
from pathlib import Path
import stat

import hosted_rehearsal as h

LIMIT = 2 * 1024 * 1024
MAX_RECORDS = 10000
PROTECTED_SCHEMAS = {'information_schema', 'auth', 'storage', 'realtime', 'vault', 'extensions',
                     'graphql', 'graphql_public', 'pgbouncer', 'pgsodium', 'pgsodium_masks', 'cron', 'net'}
FAMILIES = ('schemas', 'relations', 'columns', 'constraints', 'indexes', 'policies', 'triggers',
            'routines', 'types', 'extensions', 'default_acls')
UNKNOWN = ('unobserved-privilege-graphs', 'dynamic-routine-dependencies',
           'type-and-extension-behavior', 'extension-membership-without-identifiable-namespace',
           'external-effects', 'grants-ownership-and-default-policy-mapping',
           'data-snapshot-export-verification', 'destination-compatibility',
           'auth-storage-and-managed-services', 'scale')


class InspectionError(Exception):
    """Fixed non-sensitive failure used by this developer-only helper."""


def require(condition, message='invalid inspection input'):
    if not condition:
        raise InspectionError(message)


def _text(value):
    require(type(value) is str and '\x00' not in value, 'invalid inspection input')
    try:
        size = len(value.encode('utf-8'))
    except UnicodeError:
        raise InspectionError('invalid inspection input') from None
    require(0 < size <= 63, 'invalid inspection input')
    return value


def validate_schemas(schemas):
    require(type(schemas) is list and 1 <= len(schemas) <= 32, 'invalid schema selection')
    names = [_text(value) for value in schemas]
    require(len(set(names)) == len(names), 'invalid schema selection')
    require(all(name not in PROTECTED_SCHEMAS and not name.startswith(('pg_', 'supabase_')) for name in names),
            'protected schema selection')
    return sorted(names)


def _selected_json_hex(schemas):
    return json.dumps(validate_schemas(schemas), ensure_ascii=False, separators=(',', ':')).encode('utf-8').hex()


def inspection_sql(schemas):
    """Fixed PG17 catalog query; selection occurs only through a hex JSON datum."""
    selected = _selected_json_hex(schemas)
    return f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path = pg_catalog;
WITH selected AS (
 SELECT value AS name FROM jsonb_array_elements_text(convert_from(decode('{selected}','hex'),'UTF8')::jsonb) AS value
), namespaces AS (
 SELECT n.oid,n.nspname,r.rolname AS owner FROM pg_namespace n JOIN selected s ON s.name=n.nspname JOIN pg_roles r ON r.oid=n.nspowner
), result AS (
SELECT jsonb_build_object(
 'version',1,'server_version_num',current_setting('server_version_num')::int,
 'schemas',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',nspname,'owner',owner) ORDER BY nspname) FROM namespaces),'[]'::jsonb),
 'relations',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'name',c.relname,'kind',c.relkind::text,'owner',r.rolname,'persistence',c.relpersistence::text,'partitioned',c.relispartition,'rls',c.relrowsecurity,'force_rls',c.relforcerowsecurity) ORDER BY n.nspname,c.relname,c.relkind) FROM pg_class c JOIN namespaces n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner),'[]'::jsonb),
 'columns',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'relation',c.relname,'name',a.attname,'type_schema',tn.nspname,'type_name',t.typname,'identity',a.attidentity::text,'generated',a.attgenerated::text,'has_default',EXISTS(SELECT FROM pg_attrdef d WHERE d.adrelid=a.attrelid AND d.adnum=a.attnum),'column_acl',a.attacl IS NOT NULL) ORDER BY n.nspname,c.relname,a.attnum) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN namespaces n ON n.oid=c.relnamespace JOIN pg_type t ON t.oid=a.atttypid JOIN pg_namespace tn ON tn.oid=t.typnamespace WHERE a.attnum>0 AND NOT a.attisdropped),'[]'::jsonb),
 'constraints',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'relation',CASE WHEN c.conrelid=0 THEN NULL ELSE rc.relname END,'domain',CASE WHEN c.contypid=0 THEN NULL ELSE dt.typname END,'name',c.conname,'type',c.contype::text,'validated',c.convalidated,'deferrable',c.condeferrable,'deferred',c.condeferred,'foreign_schema',fn.nspname) ORDER BY n.nspname,COALESCE(rc.relname,dt.typname),c.conname) FROM pg_constraint c JOIN namespaces n ON n.oid=c.connamespace LEFT JOIN pg_class rc ON rc.oid=c.conrelid LEFT JOIN pg_type dt ON dt.oid=c.contypid LEFT JOIN pg_class fc ON fc.oid=c.confrelid LEFT JOIN pg_namespace fn ON fn.oid=fc.relnamespace),'[]'::jsonb),
 'indexes',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'relation',c.relname,'name',i.relname,'valid',x.indisvalid,'ready',x.indisready,'unique',x.indisunique,'expression',x.indexprs IS NOT NULL,'predicate',x.indpred IS NOT NULL) ORDER BY n.nspname,c.relname,i.relname) FROM pg_index x JOIN pg_class c ON c.oid=x.indrelid JOIN namespaces n ON n.oid=c.relnamespace JOIN pg_class i ON i.oid=x.indexrelid),'[]'::jsonb),
 'policies',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'relation',c.relname,'name',p.polname,'command',p.polcmd::text,'permissive',p.polpermissive,'roles',COALESCE((SELECT jsonb_agg(COALESCE(r.rolname,'PUBLIC') ORDER BY COALESCE(r.rolname,'PUBLIC')) FROM unnest(p.polroles) roleid LEFT JOIN pg_roles r ON r.oid=roleid),'[]'::jsonb)) ORDER BY n.nspname,c.relname,p.polname) FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN namespaces n ON n.oid=c.relnamespace),'[]'::jsonb),
 'triggers',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'relation',c.relname,'name',t.tgname,'enabled',t.tgenabled::text,'function_schema',fn.nspname) ORDER BY n.nspname,c.relname,t.tgname) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN namespaces n ON n.oid=c.relnamespace JOIN pg_proc f ON f.oid=t.tgfoid JOIN pg_namespace fn ON fn.oid=f.pronamespace WHERE NOT t.tgisinternal),'[]'::jsonb),
 'routines',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'name',p.proname,'kind',p.prokind::text,'argument_oids',to_jsonb(ARRAY(SELECT x::bigint FROM unnest(p.proargtypes::oid[]) x)),'language',l.lanname,'owner',r.rolname,'security_definer',p.prosecdef,'sql_body',p.prosqlbody IS NOT NULL) ORDER BY n.nspname,p.proname,p.oid) FROM pg_proc p JOIN namespaces n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang JOIN pg_roles r ON r.oid=p.proowner),'[]'::jsonb),
 'types',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'name',t.typname,'kind',t.typtype::text,'owner',r.rolname) ORDER BY n.nspname,t.typname) FROM pg_type t JOIN namespaces n ON n.oid=t.typnamespace JOIN pg_roles r ON r.oid=t.typowner LEFT JOIN pg_class tc ON tc.oid=t.typrelid WHERE t.typtype IN ('e','d','r') OR (t.typtype='c' AND tc.relkind='c')),'[]'::jsonb),
 'extensions',COALESCE((SELECT jsonb_agg(jsonb_build_object('object_kind',x.type,'schema',COALESCE(ns.nspname,sn.nspname),'name',x.name,'identity',x.identity,'extension',e.extname) ORDER BY x.type,COALESCE(ns.nspname,sn.nspname),x.identity,e.extname) FROM pg_depend d JOIN pg_extension e ON e.oid=d.refobjid CROSS JOIN LATERAL pg_identify_object(d.classid,d.objid,d.objsubid) x LEFT JOIN pg_namespace ns ON d.classid='pg_namespace'::regclass AND ns.oid=d.objid JOIN namespaces sn ON (ns.oid IS NOT NULL AND sn.oid=ns.oid) OR (ns.oid IS NULL AND x.schema=quote_ident(sn.nspname)) WHERE d.refclassid='pg_extension'::regclass AND d.deptype='e'),'[]'::jsonb),
 'default_acls',COALESCE((SELECT jsonb_agg(jsonb_build_object('schema',n.nspname,'owner',r.rolname,'object_type',d.defaclobjtype::text) ORDER BY n.nspname,r.rolname,d.defaclobjtype) FROM pg_default_acl d JOIN namespaces n ON n.oid=d.defaclnamespace JOIN pg_roles r ON r.oid=d.defaclrole),'[]'::jsonb)
) AS value)
SELECT value::text FROM result;
ROLLBACK;
"""


def empty_response(schemas):
    return dict(version=1, server_version_num=170000,
                schemas=[dict(name=name, owner='unknown') for name in validate_schemas(schemas)],
                **{family: [] for family in FAMILIES if family != 'schemas'})


def _pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise InspectionError('invalid inventory response')
        result[key] = value
    return result


def _json(raw):
    require(type(raw) is bytes and len(raw) <= LIMIT, 'invalid inventory response')
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=_pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (UnicodeError, ValueError, RecursionError):
        raise InspectionError('invalid inventory response') from None


def _walk(value, depth=0):
    require(depth <= 32, 'invalid inventory response')
    if type(value) is str:
        try:
            require(len(value.encode('utf-8')) <= 4096, 'invalid inventory response')
        except UnicodeError:
            raise InspectionError('invalid inventory response') from None
    elif type(value) is list:
        for child in value: _walk(child, depth + 1)
    elif type(value) is dict:
        for key, child in value.items():
            require(type(key) is str, 'invalid inventory response')
            _walk(child, depth + 1)
    else:
        require(type(value) in (int, bool) or value is None, 'invalid inventory response')


def _record(value, fields):
    require(type(value) is dict and set(value) == set(fields), 'invalid inventory response')
    for name, expected in fields.items():
        item = value[name]
        if expected == 'text': require(type(item) is str, 'invalid inventory response')
        elif expected == 'optional-text': require(item is None or type(item) is str, 'invalid inventory response')
        elif expected == 'bool': require(type(item) is bool, 'invalid inventory response')
        elif expected == 'oids': require(type(item) is list and all(type(x) is int and 0 <= x <= 4294967295 for x in item), 'invalid inventory response')
        elif expected == 'texts': require(type(item) is list and all(type(x) is str for x in item), 'invalid inventory response')


FIELDS = {
 'schemas': {'name':'text','owner':'text'},
 'relations': {'schema':'text','name':'text','kind':'text','owner':'text','persistence':'text','partitioned':'bool','rls':'bool','force_rls':'bool'},
 'columns': {'schema':'text','relation':'text','name':'text','type_schema':'text','type_name':'text','identity':'text','generated':'text','has_default':'bool','column_acl':'bool'},
 'constraints': {'schema':'text','relation':'optional-text','domain':'optional-text','name':'text','type':'text','validated':'bool','deferrable':'bool','deferred':'bool','foreign_schema':'optional-text'},
 'indexes': {'schema':'text','relation':'text','name':'text','valid':'bool','ready':'bool','unique':'bool','expression':'bool','predicate':'bool'},
 'policies': {'schema':'text','relation':'text','name':'text','command':'text','permissive':'bool','roles':'texts'},
 'triggers': {'schema':'text','relation':'text','name':'text','enabled':'text','function_schema':'text'},
 'routines': {'schema':'text','name':'text','kind':'text','argument_oids':'oids','language':'text','owner':'text','security_definer':'bool','sql_body':'bool'},
 'types': {'schema':'text','name':'text','kind':'text','owner':'text'},
 'extensions': {'object_kind':'text','schema':'text','name':'optional-text','identity':'text','extension':'text'},
 'default_acls': {'schema':'text','owner':'text','object_type':'text'},
}


def _constants(family, record):
    allowed = {'relations': {'persistence': {'p','u','t'}},
               'columns': {'identity': {'','a','d'}, 'generated': {'','s'}},
               'constraints': {'type': {'c','f','n','p','u','x','t'}},
               'policies': {'command': {'r','a','w','d','*'}},
               'triggers': {'enabled': {'O','D','R','A'}},
               'routines': {'kind': {'f','p','a','w'}},
               'types': {'kind': {'e','d','c','r'}},
               'default_acls': {'object_type': {'r','S','f','T','n'}}}
    for name, values in allowed.get(family, {}).items(): require(record[name] in values, 'invalid inventory response')
    if family == 'relations': require(len(record['kind']) == 1, 'invalid inventory response')


def _identity(family, record):
    keys = {'schemas': ('name',), 'relations': ('schema','name'), 'columns': ('schema','relation','name'),
            'constraints': ('schema','relation','domain','name'), 'indexes': ('schema','name'),
            'policies': ('schema','relation','name'), 'triggers': ('schema','relation','name'),
            'routines': ('schema','name','argument_oids'), 'types': ('schema','name'),
            'extensions': ('object_kind','schema','identity'), 'default_acls': ('schema','owner','object_type')}
    return tuple(tuple(record[key]) if type(record[key]) is list else record[key] for key in keys[family])


def validate_response(raw, schemas):
    selected = validate_schemas(schemas)
    value = _json(raw)
    _walk(value)
    require(type(value) is dict and set(value) == {'version','server_version_num',*FAMILIES}
            and type(value['version']) is int and value['version'] == 1
            and type(value['server_version_num']) is int and 170000 <= value['server_version_num'] < 180000,
            'invalid inventory response')
    for family in FAMILIES:
        require(type(value[family]) is list and len(value[family]) <= MAX_RECORDS, 'invalid inventory response')
        seen = set()
        for record in value[family]:
            _record(record, FIELDS[family]); _constants(family, record)
            if family != 'schemas': require(record['schema'] in selected, 'invalid inventory response')
            identity = _identity(family, record)
            require(identity not in seen, 'invalid inventory response'); seen.add(identity)
    require([record['name'] for record in value['schemas']] == selected, 'invalid inventory response')
    return value


def _finding(code, kind, **identity):
    return {'code': code, 'object': dict(kind=kind, **identity)}


def _owner_finding(kind, item, keys):
    return _finding('non-postgres-ownership', kind, **{key:item[key] for key in keys}) if item['owner'] != 'postgres' else None


def assess(inventory, schemas):
    inventory = validate_response(json.dumps(inventory, ensure_ascii=False, separators=(',', ':')).encode('utf-8'), schemas)
    selected, findings = set(validate_schemas(schemas)), []
    for item in inventory['schemas']:
        if (finding := _owner_finding('schema', item, ('name',))): findings.append(finding)
    for item in inventory['relations']:
        ref = {'schema':item['schema'], 'name':item['name']}
        if item['kind'] == 'f': findings.append(_finding('foreign-table','relation',**ref))
        if item['kind'] == 'm': findings.append(_finding('materialized-view','relation',**ref))
        if item['partitioned'] or item['kind'] == 'p': findings.append(_finding('partitioning','relation',**ref))
        if item['persistence'] == 'u': findings.append(_finding('unlogged-relation','relation',**ref))
        if item['kind'] not in {'r','p','v','m','S','f','c','i','I'}: findings.append(_finding('unknown-relation-kind','relation',**ref))
        if (finding := _owner_finding('relation', item, ('schema','name'))): findings.append(finding)
    for item in inventory['columns']:
        ref = {'schema':item['schema'],'relation':item['relation'],'name':item['name']}
        if item['column_acl']: findings.append(_finding('column-acl','column',**ref))
        if item['generated'] or item['has_default']: findings.append(_finding('generated-or-default-dependency-review','column',**ref))
    for item in inventory['constraints']:
        ref = {'schema':item['schema'],'relation':item['relation'],'domain':item['domain'],'name':item['name']}
        if item['foreign_schema'] is not None and item['foreign_schema'] not in selected: findings.append(_finding('external-schema-fk','constraint',**ref))
        if not item['validated']: findings.append(_finding('unvalidated-constraint','constraint',**ref))
    for item in inventory['indexes']:
        if not item['valid'] or not item['ready']: findings.append(_finding('invalid-or-unready-index','index',schema=item['schema'],relation=item['relation'],name=item['name']))
    for item in inventory['policies']: findings.append(_finding('rls-policy-review','policy',schema=item['schema'],relation=item['relation'],name=item['name']))
    for item in inventory['triggers']: findings.append(_finding('noninternal-trigger','trigger',schema=item['schema'],relation=item['relation'],name=item['name']))
    for item in inventory['routines']:
        ref = {'schema':item['schema'],'name':item['name'],'argument_oids':item['argument_oids']}
        findings.append(_finding('routine-review','routine',**ref))
        if item['security_definer']: findings.append(_finding('security-definer-routine-review','routine',**ref))
        if (finding := _owner_finding('routine', item, ('schema','name','argument_oids'))): findings.append(finding)
    for item in inventory['types']:
        findings.append(_finding('custom-type-review','type',schema=item['schema'],name=item['name']))
        if (finding := _owner_finding('type', item, ('schema','name'))): findings.append(finding)
    for item in inventory['extensions']: findings.append(_finding('extension-owned-object','extension-member',object_kind=item['object_kind'],schema=item['schema'],identity=item['identity']))
    for item in inventory['default_acls']: findings.append(_finding('default-privilege-review','default-acl',schema=item['schema'],owner=item['owner'],object_type=item['object_type']))
    return {'version':1, 'selected_schemas':sorted(selected), 'observed_families':list(FAMILIES), 'inventory':inventory,
            'findings':findings, 'unknowns':list(UNKNOWN), 'execution_supported':False, 'export_ready':False,
            'restore_verified':False, 'dependency_analysis_complete':False,
            'routine_argument_oids_portable_identities':False}


def _private_directory(path):
    path = Path(path); require(path.is_absolute(), 'invalid private directory')
    try: info = path.lstat()
    except OSError: raise InspectionError('invalid private directory') from None
    require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode) and info.st_mode & 0o077 == 0, 'invalid private directory')
    return path


def inspect(cfg, work, schemas):
    schemas = validate_schemas(schemas); work = _private_directory(work); sql = inspection_sql(schemas)
    try:
        h.versions(cfg, work); raw = h.psql(cfg, work, sql)
    except h.RehearsalError:
        raise InspectionError('inspection transport failed') from None
    return assess(validate_response(raw, schemas), schemas)


def inspect_to_file(cfg, work, schemas, output):
    schemas = validate_schemas(schemas); work = _private_directory(work); output = Path(output)
    require(output.is_absolute() and output.parent == _private_directory(output.parent), 'invalid output path')
    require(not output.exists() and not output.is_symlink(), 'invalid output path')
    result = inspect(cfg, work, schemas); data = json.dumps(result, ensure_ascii=False, separators=(',', ':')).encode('utf-8')
    require(len(data) <= LIMIT, 'inspection output exceeds limit')
    try: h.private_write(output, data)
    except OSError: raise InspectionError('invalid output path') from None
    return result
