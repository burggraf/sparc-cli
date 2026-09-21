"""Bounded read-only PG17 direct catalog dependency observation."""
import copy
import json
from pathlib import Path
import stat

import application_inventory as inventory
import hosted_rehearsal as h

LIMIT = 2 * 1024 * 1024
MAX_RECORDS = 10000
FAMILIES = ('schemas', 'edges', 'memberships')
UNKNOWN = (
    'direct-catalog-edges-only-not-dependency-closure',
    'dynamic-and-string-bodied-routine-dependencies',
    'routine-bodies-row-data-and-expressions-not-read',
    'unsupported-catalog-identities-are-unresolved',
    'external-effects-and-destination-prerequisites',
)


class DependencyInspectionError(Exception):
    """Fixed non-sensitive failure for this developer-only observer."""


def require(condition, message='invalid dependency inspection input'):
    if not condition:
        raise DependencyInspectionError(message)


def validate_schemas(schemas):
    try:
        return inventory.validate_schemas(schemas)
    except inventory.InspectionError:
        raise DependencyInspectionError('invalid schema selection') from None


def _selected_hex(schemas):
    return json.dumps(validate_schemas(schemas), ensure_ascii=False, separators=(',', ':')).encode('utf-8').hex()


def observation_sql(schemas):
    """Fixed PG17 query; input names are data encoded once as hex JSON."""
    selected = _selected_hex(schemas)
    return f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path = pg_catalog;
WITH selected AS (
 SELECT value AS name FROM jsonb_array_elements_text(convert_from(decode('{selected}','hex'),'UTF8')::jsonb) AS value
), namespaces AS (
 SELECT n.oid,n.nspname FROM pg_namespace n JOIN selected s ON s.name=n.nspname
), sources AS (
 SELECT 'relation'::text kind,'pg_class'::regclass classid,c.oid objid,0::int objsubid,n.nspname schema,c.relname name,
        NULL::text owner_kind,NULL::text owner,NULL::text subname,NULL::text arguments
 FROM pg_class c JOIN namespaces n ON n.oid=c.relnamespace
 UNION ALL SELECT 'column','pg_class'::regclass,c.oid,a.attnum,n.nspname,c.relname,NULL,NULL,a.attname,NULL
 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN namespaces n ON n.oid=c.relnamespace WHERE a.attnum>0 AND NOT a.attisdropped
 UNION ALL SELECT 'routine','pg_proc'::regclass,p.oid,0,n.nspname,p.proname,NULL,NULL,NULL,pg_get_function_identity_arguments(p.oid)
 FROM pg_proc p JOIN namespaces n ON n.oid=p.pronamespace
 UNION ALL SELECT 'type','pg_type'::regclass,t.oid,0,n.nspname,t.typname,NULL,NULL,NULL,NULL
 FROM pg_type t JOIN namespaces n ON n.oid=t.typnamespace
 UNION ALL SELECT 'constraint','pg_constraint'::regclass,c.oid,0,n.nspname,c.conname,
   CASE WHEN c.conrelid<>0 THEN 'relation' WHEN c.contypid<>0 THEN 'domain' END,
   COALESCE(cr.relname,ct.typname),NULL,NULL
 FROM pg_constraint c JOIN namespaces n ON n.oid=c.connamespace
 LEFT JOIN pg_class cr ON cr.oid=c.conrelid LEFT JOIN pg_type ct ON ct.oid=c.contypid
 UNION ALL SELECT 'default','pg_attrdef'::regclass,d.oid,0,n.nspname,c.relname,NULL,NULL,a.attname,NULL
 FROM pg_attrdef d JOIN pg_class c ON c.oid=d.adrelid JOIN pg_attribute a ON a.attrelid=d.adrelid AND a.attnum=d.adnum JOIN namespaces n ON n.oid=c.relnamespace
 UNION ALL SELECT 'rewrite','pg_rewrite'::regclass,r.oid,0,n.nspname,c.relname,NULL,NULL,r.rulename,NULL
 FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class JOIN namespaces n ON n.oid=c.relnamespace
 UNION ALL SELECT 'policy','pg_policy'::regclass,p.oid,0,n.nspname,c.relname,NULL,NULL,p.polname,NULL
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN namespaces n ON n.oid=c.relnamespace
 UNION ALL SELECT 'trigger','pg_trigger'::regclass,t.oid,0,n.nspname,c.relname,NULL,NULL,t.tgname,NULL
 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN namespaces n ON n.oid=c.relnamespace
 UNION ALL SELECT 'schema','pg_namespace'::regclass,n.oid,0,n.nspname,n.nspname,NULL,NULL,NULL,NULL FROM namespaces n
), raw_edges AS (
 SELECT s.kind source_kind,s.schema source_schema,s.name source_name,s.owner_kind source_owner_kind,s.owner source_owner,s.subname source_subname,s.arguments source_arguments,
   CASE WHEN d.refclassid='pg_class'::regclass AND rc.oid IS NOT NULL AND (d.refobjsubid=0 OR ra.attnum IS NOT NULL) THEN CASE WHEN d.refobjsubid=0 THEN 'relation' ELSE 'column' END
        WHEN d.refclassid='pg_proc'::regclass AND pp.oid IS NOT NULL THEN 'routine'
        WHEN d.refclassid='pg_type'::regclass AND tt.oid IS NOT NULL THEN 'type'
        WHEN d.refclassid='pg_constraint'::regclass AND cc.oid IS NOT NULL AND (cc.conrelid<>0 OR cc.contypid<>0) THEN 'constraint'
        WHEN d.refclassid='pg_namespace'::regclass AND nn.oid IS NOT NULL THEN 'schema'
        WHEN d.refclassid='pg_extension'::regclass AND ee.oid IS NOT NULL THEN 'extension'
        ELSE 'unresolved' END target_kind,
   COALESCE(rn.nspname,pn.nspname,tn.nspname,cn.nspname,nn.nspname,en.nspname) raw_target_schema,
   COALESCE(rc.relname,pp.proname,tt.typname,cc.conname,nn.nspname,ee.extname) raw_target_name,
   CASE WHEN cc.conrelid<>0 THEN 'relation' WHEN cc.contypid<>0 THEN 'domain' END raw_target_owner_kind,
   COALESCE(tcr.relname,tct.typname) raw_target_owner,
   CASE WHEN d.refclassid='pg_class'::regclass AND d.refobjsubid<>0 THEN ra.attname END raw_target_subname,
   CASE WHEN d.refclassid='pg_proc'::regclass THEN pg_get_function_identity_arguments(pp.oid) END raw_target_arguments,
   d.deptype::text dependency_type,se.extname source_extension,te.extname target_extension,
   d.refclassid::oid::bigint refclassid,d.refobjid::bigint refobjid,d.refobjsubid
 FROM sources s JOIN pg_depend d ON d.classid=s.classid AND d.objid=s.objid AND d.objsubid=s.objsubid
 LEFT JOIN pg_class rc ON d.refclassid='pg_class'::regclass AND rc.oid=d.refobjid
 LEFT JOIN pg_namespace rn ON rn.oid=rc.relnamespace
 LEFT JOIN pg_attribute ra ON ra.attrelid=rc.oid AND ra.attnum=d.refobjsubid AND NOT ra.attisdropped
 LEFT JOIN pg_proc pp ON d.refclassid='pg_proc'::regclass AND pp.oid=d.refobjid LEFT JOIN pg_namespace pn ON pn.oid=pp.pronamespace
 LEFT JOIN pg_type tt ON d.refclassid='pg_type'::regclass AND tt.oid=d.refobjid LEFT JOIN pg_namespace tn ON tn.oid=tt.typnamespace
 LEFT JOIN pg_constraint cc ON d.refclassid='pg_constraint'::regclass AND cc.oid=d.refobjid LEFT JOIN pg_namespace cn ON cn.oid=cc.connamespace
 LEFT JOIN pg_class tcr ON tcr.oid=cc.conrelid LEFT JOIN pg_type tct ON tct.oid=cc.contypid
 LEFT JOIN pg_namespace nn ON d.refclassid='pg_namespace'::regclass AND nn.oid=d.refobjid
 LEFT JOIN pg_extension ee ON d.refclassid='pg_extension'::regclass AND ee.oid=d.refobjid LEFT JOIN pg_namespace en ON en.oid=ee.extnamespace
 LEFT JOIN pg_depend sx ON sx.classid=s.classid AND sx.objid=s.objid AND sx.objsubid=0 AND sx.refclassid='pg_extension'::regclass AND sx.deptype='e'
 LEFT JOIN pg_extension se ON se.oid=sx.refobjid
 LEFT JOIN pg_depend tx ON tx.classid=d.refclassid AND tx.objid=d.refobjid AND tx.objsubid=0 AND tx.refclassid='pg_extension'::regclass AND tx.deptype='e'
 LEFT JOIN pg_extension te ON te.oid=tx.refobjid
), edges AS (
 SELECT source_kind,source_schema,source_name,source_owner_kind,source_owner,source_subname,source_arguments,target_kind,
   CASE WHEN target_kind='unresolved' THEN NULL ELSE raw_target_schema END target_schema,
   CASE WHEN target_kind='unresolved' THEN NULL ELSE raw_target_name END target_name,
   CASE WHEN target_kind='constraint' THEN raw_target_owner_kind END target_owner_kind,
   CASE WHEN target_kind='constraint' THEN raw_target_owner END target_owner,
   CASE WHEN target_kind='unresolved' THEN NULL ELSE raw_target_subname END target_subname,
   CASE WHEN target_kind='unresolved' THEN NULL ELSE raw_target_arguments END target_arguments,
   dependency_type,source_extension,target_extension,target_kind='unresolved' unresolved,
   CASE WHEN target_kind='unresolved' THEN refclassid END target_local_class_id,
   CASE WHEN target_kind='unresolved' THEN refobjid END target_local_object_id,
   CASE WHEN target_kind='unresolved' THEN refobjsubid END target_local_subobject_id
 FROM raw_edges
), inheritance AS (
 SELECT 'relation'::text source_kind,cn.nspname source_schema,child.relname source_name,NULL::text source_owner_kind,NULL::text source_owner,NULL::text source_subname,NULL::text source_arguments,
   'relation'::text target_kind,pn.nspname target_schema,parent.relname target_name,NULL::text target_owner_kind,NULL::text target_owner,NULL::text target_subname,NULL::text target_arguments,
   'inheritance'::text dependency_type,se.extname source_extension,te.extname target_extension,false unresolved,NULL::bigint target_local_class_id,NULL::bigint target_local_object_id,NULL::int target_local_subobject_id
 FROM pg_inherits i JOIN pg_class child ON child.oid=i.inhrelid JOIN namespaces cn ON cn.oid=child.relnamespace JOIN pg_class parent ON parent.oid=i.inhparent JOIN pg_namespace pn ON pn.oid=parent.relnamespace
 LEFT JOIN pg_depend sx ON sx.classid='pg_class'::regclass AND sx.objid=child.oid AND sx.objsubid=0 AND sx.refclassid='pg_extension'::regclass AND sx.deptype='e' LEFT JOIN pg_extension se ON se.oid=sx.refobjid
 LEFT JOIN pg_depend tx ON tx.classid='pg_class'::regclass AND tx.objid=parent.oid AND tx.objsubid=0 AND tx.refclassid='pg_extension'::regclass AND tx.deptype='e' LEFT JOIN pg_extension te ON te.oid=tx.refobjid
), all_edges AS (SELECT * FROM edges UNION ALL SELECT * FROM inheritance), memberships AS (
 SELECT DISTINCT side,kind,schema,name,owner_kind,owner,subname,arguments,local_class_id,local_object_id,local_subobject_id,extension FROM (
  SELECT 'source'::text side,source_kind kind,source_schema schema,source_name name,source_owner_kind owner_kind,source_owner owner,source_subname subname,source_arguments arguments,NULL::bigint local_class_id,NULL::bigint local_object_id,NULL::int local_subobject_id,source_extension extension FROM all_edges WHERE source_extension IS NOT NULL
  UNION ALL SELECT 'target',target_kind,target_schema,target_name,target_owner_kind,target_owner,target_subname,target_arguments,target_local_class_id,target_local_object_id,target_local_subobject_id,target_extension FROM all_edges WHERE target_extension IS NOT NULL
 ) x
)
SELECT jsonb_build_object(
 'version',1,'server_version_num',current_setting('server_version_num')::int,
 'transaction',jsonb_build_object('read_only',current_setting('transaction_read_only'),'isolation',current_setting('transaction_isolation'),'search_path',current_setting('search_path')),
 'schemas',COALESCE((SELECT jsonb_agg(jsonb_build_object('name',nspname) ORDER BY nspname) FROM namespaces),'[]'::jsonb),
 'edges',COALESCE((SELECT jsonb_agg(jsonb_build_object('source_kind',source_kind,'source_schema',source_schema,'source_name',source_name,'source_owner_kind',source_owner_kind,'source_owner',source_owner,'source_subname',source_subname,'source_arguments',source_arguments,'target_kind',target_kind,'target_schema',target_schema,'target_name',target_name,'target_owner_kind',target_owner_kind,'target_owner',target_owner,'target_subname',target_subname,'target_arguments',target_arguments,'dependency_type',dependency_type,'source_extension',source_extension,'target_extension',target_extension,'unresolved',unresolved,'target_local_class_id',target_local_class_id,'target_local_object_id',target_local_object_id,'target_local_subobject_id',target_local_subobject_id) ORDER BY source_kind,source_schema,source_name,source_owner_kind NULLS FIRST,source_owner NULLS FIRST,source_subname NULLS FIRST,target_kind,target_schema NULLS FIRST,target_name NULLS FIRST,dependency_type,target_local_class_id NULLS FIRST,target_local_object_id NULLS FIRST,target_local_subobject_id NULLS FIRST) FROM all_edges),'[]'::jsonb),
 'memberships',COALESCE((SELECT jsonb_agg(jsonb_build_object('side',side,'kind',kind,'schema',schema,'name',name,'owner_kind',owner_kind,'owner',owner,'subname',subname,'arguments',arguments,'local_class_id',local_class_id,'local_object_id',local_object_id,'local_subobject_id',local_subobject_id,'extension',extension) ORDER BY side,kind,schema NULLS FIRST,name NULLS FIRST,owner_kind NULLS FIRST,owner NULLS FIRST,subname NULLS FIRST,local_class_id NULLS FIRST,local_object_id NULLS FIRST,local_subobject_id NULLS FIRST,extension) FROM memberships),'[]'::jsonb)
)::text;
ROLLBACK;
"""


def empty_response(schemas):
    return {'version': 1, 'server_version_num': 170000,
            'transaction': {'read_only': 'on', 'isolation': 'repeatable read', 'search_path': 'pg_catalog'},
            'schemas': [{'name': name} for name in validate_schemas(schemas)], 'edges': [], 'memberships': []}


def _pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise DependencyInspectionError('invalid dependency response')
        result[key] = value
    return result


def _json(raw):
    require(type(raw) is bytes and len(raw) <= LIMIT, 'invalid dependency response')
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=_pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
    except (UnicodeError, ValueError, RecursionError):
        raise DependencyInspectionError('invalid dependency response') from None


def _walk(value, depth=0):
    require(depth <= 32, 'invalid dependency response')
    if type(value) is str:
        try: require(len(value.encode('utf-8')) <= 4096, 'invalid dependency response')
        except UnicodeError: raise DependencyInspectionError('invalid dependency response') from None
    elif type(value) is list:
        for child in value: _walk(child, depth + 1)
    elif type(value) is dict:
        for key, child in value.items():
            require(type(key) is str, 'invalid dependency response'); _walk(child, depth + 1)
    else:
        require(type(value) in (int, bool) or value is None, 'invalid dependency response')


def _record(record, fields):
    require(type(record) is dict and set(record) == set(fields), 'invalid dependency response')
    for field, kind in fields.items():
        value = record[field]
        if kind == 'name':
            require(type(value) is str and '\x00' not in value and 0 < len(value.encode('utf-8')) <= 63, 'invalid dependency response')
        elif kind == 'optional-name':
            require(value is None or (type(value) is str and '\x00' not in value and 0 < len(value.encode('utf-8')) <= 63), 'invalid dependency response')
        elif kind == 'text':
            require(type(value) is str and '\x00' not in value and value != '', 'invalid dependency response')
        elif kind == 'optional-text':
            require(value is None or (type(value) is str and '\x00' not in value), 'invalid dependency response')
        elif kind == 'bool': require(type(value) is bool, 'invalid dependency response')
        elif kind == 'optional-oid': require(value is None or (type(value) is int and 0 <= value <= 4294967295), 'invalid dependency response')


EDGE_FIELDS = {'source_kind':'text','source_schema':'name','source_name':'name','source_owner_kind':'optional-text','source_owner':'optional-name','source_subname':'optional-name','source_arguments':'optional-text','target_kind':'text','target_schema':'optional-name','target_name':'optional-name','target_owner_kind':'optional-text','target_owner':'optional-name','target_subname':'optional-name','target_arguments':'optional-text','dependency_type':'text','source_extension':'optional-name','target_extension':'optional-name','unresolved':'bool','target_local_class_id':'optional-oid','target_local_object_id':'optional-oid','target_local_subobject_id':'optional-oid'}
MEMBERSHIP_FIELDS = {'side':'text','kind':'text','schema':'optional-name','name':'optional-name','owner_kind':'optional-text','owner':'optional-name','subname':'optional-name','arguments':'optional-text','local_class_id':'optional-oid','local_object_id':'optional-oid','local_subobject_id':'optional-oid','extension':'name'}
SOURCE_KINDS = {'relation','column','routine','type','constraint','default','rewrite','policy','trigger','schema'}
TARGET_KINDS = {'relation','column','routine','type','constraint','schema','extension','unresolved'}
DEPENDENCY_TYPES = {'n','a','i','e','p','P','S','x','inheritance'}
CONSTRAINT_OWNERS = {'relation','domain'}


def _object_identity(record, prefix, membership=False):
    if membership:
        return (record['kind'], record['schema'], record['name'], record['owner_kind'], record['owner'], record['subname'], record['arguments'], record['local_class_id'], record['local_object_id'], record['local_subobject_id'])
    return (record[prefix + '_kind'], record[prefix + '_schema'], record[prefix + '_name'], record[prefix + '_owner_kind'], record[prefix + '_owner'], record[prefix + '_subname'], record[prefix + '_arguments'], record['target_local_class_id'] if prefix == 'target' else None, record['target_local_object_id'] if prefix == 'target' else None, record['target_local_subobject_id'] if prefix == 'target' else None)


def _kind_identity(record, prefix, selected=None):
    kind = record[prefix + '_kind'] if prefix else record['kind']
    schema = record[prefix + '_schema'] if prefix else record['schema']
    name = record[prefix + '_name'] if prefix else record['name']
    owner_kind = record[prefix + '_owner_kind'] if prefix else record['owner_kind']
    owner = record[prefix + '_owner'] if prefix else record['owner']
    subname = record[prefix + '_subname'] if prefix else record['subname']
    arguments = record[prefix + '_arguments'] if prefix else record['arguments']
    if selected is not None and prefix == 'source': require(schema in selected, 'invalid dependency response')
    if kind == 'unresolved':
        require(schema is None and name is None and owner_kind is None and owner is None and subname is None and arguments is None, 'invalid dependency response')
        if prefix == 'target': require(record['target_local_class_id'] is not None and record['target_local_object_id'] is not None and record['target_local_subobject_id'] is not None, 'invalid dependency response')
        else: require(record['local_class_id'] is not None and record['local_object_id'] is not None and record['local_subobject_id'] is not None, 'invalid dependency response')
        return
    require(schema is not None and name is not None, 'invalid dependency response')
    locals_ = (record.get('target_local_class_id'), record.get('target_local_object_id'), record.get('target_local_subobject_id')) if prefix == 'target' else (record.get('local_class_id'), record.get('local_object_id'), record.get('local_subobject_id'))
    require(locals_ == (None, None, None), 'invalid dependency response')
    if kind == 'constraint': require(owner_kind in CONSTRAINT_OWNERS and owner is not None, 'invalid dependency response')
    else: require(owner_kind is None and owner is None, 'invalid dependency response')
    if kind in {'column','default','rewrite','policy','trigger'}: require(subname is not None, 'invalid dependency response')
    else: require(subname is None, 'invalid dependency response')
    if kind == 'routine': require(arguments is not None, 'invalid dependency response')
    else: require(arguments is None, 'invalid dependency response')


def validate_response(raw, schemas):
    selected = validate_schemas(schemas); value = _json(raw); _walk(value)
    require(type(value) is dict and set(value) == {'version','server_version_num','transaction',*FAMILIES}, 'invalid dependency response')
    require(type(value['version']) is int and value['version'] == 1 and type(value['server_version_num']) is int and 170000 <= value['server_version_num'] < 180000, 'invalid dependency response')
    _record(value['transaction'], {'read_only':'text','isolation':'text','search_path':'text'})
    require(value['transaction'] == {'read_only':'on','isolation':'repeatable read','search_path':'pg_catalog'}, 'invalid dependency response')
    require(type(value['schemas']) is list and len(value['schemas']) <= 32, 'invalid dependency response')
    for item in value['schemas']: _record(item, {'name':'name'})
    require([item['name'] for item in value['schemas']] == selected, 'invalid dependency response')
    require(type(value['edges']) is list and len(value['edges']) <= MAX_RECORDS and type(value['memberships']) is list and len(value['memberships']) <= MAX_RECORDS, 'invalid dependency response')
    ownership, source_extensions, target_extensions, edge_ids = {}, {}, {}, set()
    for edge in value['edges']:
        _record(edge, EDGE_FIELDS)
        require(edge['source_kind'] in SOURCE_KINDS and edge['target_kind'] in TARGET_KINDS and edge['dependency_type'] in DEPENDENCY_TYPES and edge['unresolved'] == (edge['target_kind'] == 'unresolved'), 'invalid dependency response')
        _kind_identity(edge, 'source', selected); _kind_identity(edge, 'target')
        source = _object_identity(edge, 'source'); target = _object_identity(edge, 'target')
        require((source, target, edge['dependency_type']) not in edge_ids, 'invalid dependency response'); edge_ids.add((source, target, edge['dependency_type']))
        for identity, extension in ((source, edge['source_extension']), (target, edge['target_extension'])):
            if identity in ownership: require(ownership[identity] == extension, 'invalid dependency response')
            else: ownership[identity] = extension
        source_extensions[source] = edge['source_extension']; target_extensions[target] = edge['target_extension']
    memberships = {}
    for member in value['memberships']:
        _record(member, MEMBERSHIP_FIELDS)
        require(member['side'] in {'source','target'} and member['kind'] in SOURCE_KINDS | TARGET_KINDS, 'invalid dependency response')
        if member['side'] == 'source': require(member['kind'] != 'unresolved' and member['schema'] in selected, 'invalid dependency response')
        _kind_identity(member, '')
        identity = _object_identity(member, '', membership=True)
        require(identity not in memberships and (member['side'], identity) not in memberships, 'invalid dependency response')
        memberships[(member['side'], identity)] = member['extension']
    expected = {('source', identity): extension for identity, extension in source_extensions.items() if extension is not None}
    expected.update({('target', identity): extension for identity, extension in target_extensions.items() if extension is not None})
    require(memberships == expected, 'invalid dependency response')
    return copy.deepcopy(value)


def _edge_identity(edge):
    return {key: edge[key] for key in EDGE_FIELDS if key not in {'source_extension','target_extension','unresolved'}}


def assess(raw, schemas):
    """Classify validated bytes only; false readiness flags are deliberate invariants."""
    response = validate_response(raw, schemas); selected = set(validate_schemas(schemas)); findings = []
    for edge in response['edges']:
        if edge['unresolved']: classification = 'unresolved'
        elif edge['source_extension'] is not None or edge['target_extension'] is not None or edge['target_kind'] == 'extension': classification = 'extension-requirement'
        elif edge['target_schema'] in selected: classification = 'internal'
        elif edge['target_schema'] in inventory.PROTECTED_SCHEMAS: classification = 'protected-external'
        elif edge['target_schema'] in {'pg_catalog','information_schema'}: classification = 'system-prerequisite'
        else: classification = 'external'
        findings.append({'code': classification, 'edge': _edge_identity(edge)})
    for member in response['memberships']: findings.append({'code':'extension-requirement','membership':copy.deepcopy(member)})
    findings.sort(key=lambda item: json.dumps(item, ensure_ascii=False, sort_keys=True, separators=(',', ':')))
    return {'version':1,'selected_schemas':sorted(selected),'catalog_coverage':['relations-and-columns','routines','all-selected-pg-types','constraints','defaults','rewrite-rules','policies','all-triggers','schemas','pg_depend','pg_inherits','extension-membership'],'response':response,'findings':findings,'mandatory_unknowns':list(UNKNOWN),'execution_supported':False,'export_ready':False,'restore_verified':False,'dependency_analysis_complete':False}


def _private_directory(path):
    path = Path(path); require(path.is_absolute(), 'invalid private directory')
    try: info = path.lstat()
    except OSError: raise DependencyInspectionError('invalid private directory') from None
    require(stat.S_ISDIR(info.st_mode) and not stat.S_ISLNK(info.st_mode) and info.st_mode & 0o077 == 0, 'invalid private directory')
    return path


def observe(cfg, work, schemas):
    schemas = validate_schemas(schemas); work = _private_directory(work); sql = observation_sql(schemas)
    try: h.versions(cfg, work); raw = h.psql(cfg, work, sql)
    except h.RehearsalError: raise DependencyInspectionError('dependency inspection transport failed') from None
    return assess(raw, schemas)
