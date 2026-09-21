"""Fixed developer-only v2 rehearsal. No CLI and no arbitrary SQL input API.

Parent owns authorization, private work directories/configuration, quiescence,
source credential denial during restore and provider-catalog receipts. SQL in an
archive is trusted executable code, not sandboxed by these fixture assertions.
"""
import hashlib
import json
from pathlib import Path
import re
import stat

import hosted_rehearsal as h
import native_recipe as native
import expanded_metadata as metadata

RECIPE = 'supabase-2.117.0-native-pg17-fixture-v2'
ARTIFACTS = ('schema.sql', 'data.sql', 'expanded.json', 'catalog.json')


def fixture_metadata():
    return dict(version=1, roles=[dict(role) for role in metadata.ROLES],
                memberships=[dict(edge) for edge in metadata.MEMBERSHIPS],
                history=[
                    dict(version='202609200001', name=None, statements=None),
                    dict(version='202609200002', name='', statements=[]),
                    dict(version='202609200003', name="雪 ' \\ \n",
                         statements=['SELECT 1/0;', None, "\\! false\n' $x$ \\"])])


def validate_metadata(raw):
    value = metadata.validate(raw)
    h.require(value == fixture_metadata(), 'expanded fixture metadata mismatch')
    return value


def json_sql(value):
    # Fixed, validated values only; migration statement strings remain inert data.
    return "convert_from(decode('" + json.dumps(value).encode().hex() + "', 'hex'), 'UTF8')::jsonb"


COLLISIONS = """DO $collision$ BEGIN
IF EXISTS (SELECT FROM pg_roles WHERE rolname IN
 ('sparc_rehearsal_reader','sparc_rehearsal_member'))
THEN RAISE EXCEPTION 'expanded role collision'; END IF;
IF EXISTS (SELECT FROM pg_namespace WHERE nspname='supabase_migrations')
THEN RAISE EXCEPTION 'expanded history collision'; END IF;
END $collision$;
"""


def guard():
    # Identical managed baseline, with precisely the two fixture namespaces.
    old = ','.join(sorted(h.BASE_SCHEMAS.split(',') + [h.SCHEMA]))
    new = ','.join(sorted(h.BASE_SCHEMAS.split(',') + [h.SCHEMA, 'supabase_migrations']))
    return h.guard(True).replace("'" + old + "'", "'" + new + "'")


EXTENSION = """
CREATE TYPE sparc_rehearsal.note_state AS ENUM ('draft','published');
CREATE TABLE sparc_rehearsal.recovery_cases (
 id integer PRIMARY KEY, state sparc_rehearsal.note_state NOT NULL,
 label text NOT NULL CHECK (length(label)>0));
CREATE INDEX recovery_cases_published ON sparc_rehearsal.recovery_cases (id)
 WHERE state='published'::sparc_rehearsal.note_state;
CREATE FUNCTION sparc_rehearsal.label_length(text) RETURNS integer
 LANGUAGE sql IMMUTABLE STRICT SECURITY INVOKER
 AS 'SELECT pg_catalog.length($1)';
CREATE VIEW sparc_rehearsal.published_cases WITH (security_invoker=true) AS
 SELECT id, label, sparc_rehearsal.label_length(label) AS label_length
 FROM sparc_rehearsal.recovery_cases WHERE state='published'::sparc_rehearsal.note_state;
INSERT INTO sparc_rehearsal.recovery_cases VALUES (1,'draft','draft'),(2,'published','雪');
GRANT USAGE ON SCHEMA sparc_rehearsal TO sparc_rehearsal_reader;
GRANT SELECT ON sparc_rehearsal.recovery_cases, sparc_rehearsal.published_cases
 TO sparc_rehearsal_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA sparc_rehearsal
 GRANT SELECT ON TABLES TO sparc_rehearsal_reader;
"""

# Actual catalog extraction, not a synthesized fixture manifest. Creator edges
# are verified separately below and never replayed as fixture memberships.
METADATA_QUERY = """SELECT jsonb_build_object('version',1,
 'roles',(SELECT jsonb_agg(jsonb_build_object('name',rolname,'login',rolcanlogin,
  'superuser',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,
  'replication',rolreplication,'bypassrls',rolbypassrls,'inherit',rolinherit)
  ORDER BY CASE rolname WHEN 'sparc_rehearsal_reader' THEN 0 ELSE 1 END)
  FROM pg_roles WHERE rolname IN ('sparc_rehearsal_reader','sparc_rehearsal_member')),
 'memberships',(SELECT jsonb_agg(jsonb_build_object('role',r.rolname,'member',m.rolname,
  'admin',a.admin_option,'inherit',a.inherit_option,'set',a.set_option))
  FROM pg_auth_members a JOIN pg_roles r ON r.oid=a.roleid JOIN pg_roles m ON m.oid=a.member
  WHERE r.rolname='sparc_rehearsal_reader' AND m.rolname='sparc_rehearsal_member'),
 'history',(SELECT jsonb_agg(to_jsonb(t) ORDER BY version)
  FROM supabase_migrations.schema_migrations t))"""


def metadata_assertions():
    return "DO $metadata$ BEGIN IF (" + METADATA_QUERY + ") IS DISTINCT FROM " + json_sql(fixture_metadata()) + """
THEN RAISE EXCEPTION 'expanded metadata rejected'; END IF;
IF EXISTS (SELECT FROM pg_roles WHERE rolname IN ('sparc_rehearsal_reader','sparc_rehearsal_member')
 AND (rolconnlimit<>-1 OR rolvaliduntil IS NOT NULL OR rolconfig IS NOT NULL))
 OR EXISTS (SELECT FROM pg_db_role_setting WHERE setrole IN
 ('sparc_rehearsal_reader'::regrole,'sparc_rehearsal_member'::regrole))
 OR (SELECT count(*) FROM pg_auth_members WHERE roleid IN
 ('sparc_rehearsal_reader'::regrole,'sparc_rehearsal_member'::regrole) OR member IN
 ('sparc_rehearsal_reader'::regrole,'sparc_rehearsal_member'::regrole)) <> 3
 OR NOT EXISTS (SELECT FROM pg_auth_members WHERE roleid='sparc_rehearsal_reader'::regrole
 AND member='sparc_rehearsal_member'::regrole AND grantor='postgres'::regrole
 AND NOT admin_option AND inherit_option AND set_option)
 OR (SELECT count(*) FROM pg_auth_members WHERE roleid IN
 ('sparc_rehearsal_reader'::regrole,'sparc_rehearsal_member'::regrole)
 AND member='postgres'::regrole AND grantor='supabase_admin'::regrole
 AND admin_option AND NOT inherit_option AND NOT set_option) <> 2
 OR NOT EXISTS (SELECT FROM pg_roles WHERE rolname='supabase_admin' AND rolsuper)
 OR (SELECT count(*) FROM pg_shdepend WHERE refclassid='pg_authid'::regclass
 AND refobjid='sparc_rehearsal_reader'::regrole) <> 4
 OR EXISTS (SELECT FROM pg_shdepend WHERE refclassid='pg_authid'::regclass
 AND refobjid='sparc_rehearsal_reader'::regrole AND (deptype<>'a' OR dbid<>
 (SELECT oid FROM pg_database WHERE datname=current_database())))
 OR EXISTS (SELECT FROM pg_shdepend WHERE refclassid='pg_authid'::regclass
 AND refobjid='sparc_rehearsal_member'::regrole)
THEN RAISE EXCEPTION 'expanded role attributes or edges rejected'; END IF;
END $metadata$;
"""


def source_extension_sql():
    roles, history = metadata.restore_sql(json.dumps(fixture_metadata()).encode())
    return ('BEGIN;\nSET LOCAL search_path=pg_catalog;\n' + h.guard(True) + h.assertions()
            + COLLISIONS + roles + EXTENSION + history + guard() + assertions()
            + SOURCE_PROBE + assertions() + 'COMMIT; SELECT \'sparc-expanded-seed-ok\';')


def readonly_sql(expanded=True):
    return ('BEGIN READ ONLY;\nSET LOCAL search_path=pg_catalog;\n'
            + (guard() + assertions() if expanded else h.guard() + COLLISIONS)
            + "SELECT 'sparc-expanded-check-ok'; ROLLBACK;")


def check(cfg, work, expanded=True):
    output = h.psql(cfg, work, readonly_sql(expanded))
    h.require(output.rstrip().endswith(b'sparc-expanded-check-ok'), 'expanded check incomplete')


def capture_metadata(cfg, work):
    output = h.psql(cfg, work, 'BEGIN READ ONLY; SET LOCAL search_path=pg_catalog;\n'
                    + guard() + assertions() + METADATA_QUERY + '; ROLLBACK;')
    # Assertions emit only integer probe results; JSON is the final result.
    raw = output.rstrip().split(b'\n')[-1]
    value = validate_metadata(raw)
    return json.dumps(value).encode()


def capture(cfg, work, scripts, archive, recipient, identity):
    """Parent-only: quiescent capture + independent encrypted archive verification."""
    info = Path(work).lstat()
    h.require(stat.S_ISDIR(info.st_mode) and info.st_mode & 0o077 == 0, 'work directory must be private')
    archive = Path(archive).absolute()
    h.require(not archive.exists() and not archive.is_symlink(), 'archive destination already exists')
    h.require(re.fullmatch(r'age1[a-z0-9]{58}', recipient) is not None, 'native age recipient required')
    identity = h.absolute_file(str(Path(identity).absolute()), private=True)
    h.versions(cfg, work)
    check(cfg, work)
    raw = capture_metadata(cfg, work)
    validate_metadata(raw)
    catalog = capture_catalog(cfg, work)
    artifacts = {'expanded.json': raw, 'catalog.json': catalog}
    for mode in ('schema', 'data'):
        sql = native.dump(cfg, work, scripts, mode)
        h.require(len(sql) < h.LIMIT and b'-- PostgreSQL database dump\n' in sql
                  and b'-- PostgreSQL database dump complete\n' in sql, 'invalid expanded dump')
        artifacts[mode + '.sql'] = sql
    h.require(sum(map(len, artifacts.values())) < h.LIMIT - 131072, 'expanded artifacts exceed limit')
    check(cfg, work)
    h.require(validate_catalog(capture_catalog(cfg, work)) == validate_catalog(catalog), 'source catalog changed')
    h.require(validate_metadata(capture_metadata(cfg, work)) == validate_metadata(raw), 'source changed')
    material = Path(work) / 'expanded-material'
    material.mkdir(mode=0o700)
    for name, data in artifacts.items():
        h.private_write(material / name, data)
    contract = dict(version=2, recipe=RECIPE, source_ref=cfg['project_ref'], scripts=native.SCRIPT_SHA256,
                    sha256={name: hashlib.sha256(data).hexdigest() for name, data in artifacts.items()})
    h.private_write(material / 'recovery.json', json.dumps(contract).encode())
    h.run([cfg['sparc'], 'pack', str(material), str(archive), recipient], work)
    h.run([cfg['sparc'], 'verify', str(archive), str(identity)], work)


def restore(cfg, work, archive, identity, fence):
    """Destination config ONLY. Any attempted mutation consumes the parent fence.

    Parent must protect fence directory, deny source credentials and quarantine
    target on any failure. There is deliberately no cleanup or automatic retry.
    """
    h.require(not Path(fence).exists() and not Path(fence).is_symlink(), 'expanded attempt already fenced')
    for directory in (Path(work), Path(fence).absolute().parent):
        info = directory.lstat()
        h.require(stat.S_ISDIR(info.st_mode) and info.st_mode & 0o077 == 0, 'work and fence directories must be private')
    archive = Path(archive).absolute()
    h.require(archive.is_dir() and not archive.is_symlink(), 'invalid archive directory')
    expected = {'manifest.age', '00000000.age', '00000001.age', '00000002.age', '00000003.age', '00000004.age'}
    total = 0
    for entry in archive.iterdir():
        h.require(entry.name in expected, 'unexpected archive entry')
        expected.remove(entry.name)
        info = entry.lstat()
        h.require(stat.S_ISREG(info.st_mode) and info.st_size <= h.LIMIT, 'unsafe archive entry')
        total += info.st_size
    h.require(not expected and total <= h.LIMIT + 65536, 'incomplete or oversized archive')
    identity = h.absolute_file(str(Path(identity).absolute()), private=True)
    h.run([cfg['sparc'], 'verify', str(archive), str(identity)], work)
    material = Path(work) / 'expanded-restoration'
    h.run([cfg['sparc'], 'unpack', str(archive), str(material), str(identity)], work)
    h.require({p.name for p in material.iterdir()} == set(ARTIFACTS) | {'recovery.json'}, 'unexpected recovery inventory')
    h.read_file(material / 'recovery.json', 16384, private=True)
    contract = h.read_json(material / 'recovery.json', ('version','recipe','source_ref','scripts','sha256'))
    h.require(type(contract['version']) is int and contract['version'] == 2
              and contract['recipe'] == RECIPE and contract['scripts'] == native.SCRIPT_SHA256
              and type(contract['source_ref']) is str and re.fullmatch('[a-z]{20}', contract['source_ref'])
              and contract['source_ref'] != cfg['project_ref'], 'invalid expanded recovery contract')
    h.require(type(contract['sha256']) is dict and set(contract['sha256']) == set(ARTIFACTS), 'invalid artifact digests')
    artifacts = {}
    for name in ARTIFACTS:
        data = h.read_file(material / name, h.LIMIT, private=True)
        h.require(hashlib.sha256(data).hexdigest() == contract['sha256'][name], 'artifact digest mismatch')
        artifacts[name] = data
    h.require(sum(map(len, artifacts.values())) < h.LIMIT - 131072, 'expanded artifacts exceed limit')
    validate_metadata(artifacts['expanded.json'])
    catalog = validate_catalog(artifacts['catalog.json'])
    roles, history = metadata.restore_sql(artifacts['expanded.json'])
    h.versions(cfg, work)
    check(cfg, work, expanded=False)
    sql = (('BEGIN;\nSET LOCAL search_path=pg_catalog;\n' + h.guard() + COLLISIONS + roles).encode()
           + artifacts['schema.sql'] + b'\n' + artifacts['data.sql']
           + ('\nSET LOCAL session_replication_role=origin;\n'
              "SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='5s';\n"
              "SET LOCAL timezone='UTC'; SET LOCAL standard_conforming_strings=on;\n"
              'SET LOCAL search_path=pg_catalog;\n' + history + guard() + assertions()
              + PROBE + assertions() + catalog_assertion(catalog) + "COMMIT; SELECT 'sparc-expanded-restore-ok';").encode())
    h.require(len(sql) < h.LIMIT, 'expanded restore exceeds limit')
    h.private_write(fence, b'expanded-v2 attempt consumed; quarantine on failure\n')
    output = h.psql(cfg, work, sql)
    h.require(output.rstrip().endswith(b'sparc-expanded-restore-ok'), 'expanded restore incomplete; quarantine target')
    check(cfg, work)
    h.require(validate_catalog(capture_catalog(cfg, work)) == catalog, 'restored catalog mismatch; quarantine target')


# Fixed v2 copy of the v1 invariants; v1 itself remains unchanged.
def legacy_assertions():
    return f"""
-- SPARC_ASSERT_FIXTURE
-- pg_dump sets row_security=off; role probes must actually exercise policies.
SET LOCAL row_security=on;
DO $assert$ BEGIN
IF (SELECT jsonb_agg(id ORDER BY id) FROM {h.SCHEMA}.owners)
 IS DISTINCT FROM '["{h.A}","{h.B}"]'::jsonb
 OR (SELECT jsonb_agg(jsonb_build_array(id,owner_id,body,optional,created_at) ORDER BY id) FROM {h.SCHEMA}.notes)
 IS DISTINCT FROM jsonb_build_array(
 jsonb_build_array(41,'{h.A}',E'hello 雪\\nline',NULL,'2026-09-18T00:00:00+00:00'),
 jsonb_build_array(42,'{h.B}','quote '' and \\ slash','present','2026-09-18T00:00:00+00:00'))
 OR EXISTS (SELECT FROM {h.SCHEMA}.empty_table)
 OR (SELECT last_value <> 42 OR NOT is_called FROM {h.SCHEMA}.note_id)
 OR (SELECT string_agg(c.relname || ':' || c.relkind::text, ',' ORDER BY c.relname)
     FROM pg_class c WHERE relnamespace='{h.SCHEMA}'::regnamespace)
 IS DISTINCT FROM 'empty_table:r,empty_table_pkey:i,note_id:S,notes:r,notes_pkey:i,owners:r,owners_pkey:i,published_cases:v,recovery_cases:r,recovery_cases_pkey:i,recovery_cases_published:i'
 OR (SELECT count(*) FROM pg_proc WHERE pronamespace='{h.SCHEMA}'::regnamespace) <> 1
 OR (SELECT count(*) FROM pg_type WHERE typnamespace='{h.SCHEMA}'::regnamespace) <> 12
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='{h.SCHEMA}'::regnamespace
     AND relname IN ('owners','empty_table') AND (relrowsecurity OR relforcerowsecurity))
 OR EXISTS (SELECT FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
            WHERE c.relnamespace='{h.SCHEMA}'::regnamespace AND NOT t.tgisinternal)
 OR (SELECT count(*) FROM pg_constraint WHERE connamespace='{h.SCHEMA}'::regnamespace) <> 6
 OR NOT EXISTS (SELECT FROM pg_constraint WHERE conrelid='{h.SCHEMA}.notes'::regclass
     AND contype='f' AND confrelid='{h.SCHEMA}.owners'::regclass AND confdeltype='c'
     AND convalidated AND conkey=ARRAY[2]::smallint[] AND confkey=ARRAY[1]::smallint[])
 OR NOT EXISTS (SELECT FROM pg_class WHERE oid='{h.SCHEMA}.notes'::regclass AND relrowsecurity AND relforcerowsecurity)
 OR (SELECT count(*) FROM pg_policy WHERE polrelid='{h.SCHEMA}.notes'::regclass) <> 1
 OR NOT EXISTS (SELECT FROM pg_policy WHERE polrelid='{h.SCHEMA}.notes'::regclass
     AND polname='owner_read' AND polcmd='r' AND polpermissive
     AND polroles=ARRAY[(SELECT oid FROM pg_roles WHERE rolname='authenticated')])
 OR (SELECT string_agg(c.relname || '.' || a.attname || ':' || format_type(a.atttypid,a.atttypmod)
       || ':' || a.attnotnull::text, ',' ORDER BY c.relname,a.attnum)
     FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
     WHERE c.relnamespace='{h.SCHEMA}'::regnamespace AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped)
 IS DISTINCT FROM 'empty_table.id:integer:true,notes.id:integer:true,notes.owner_id:uuid:true,notes.body:text:true,notes.optional:text:false,notes.created_at:timestamp with time zone:true,owners.id:uuid:true,recovery_cases.id:integer:true,recovery_cases.state:sparc_rehearsal.note_state:true,recovery_cases.label:text:true'
 OR (SELECT count(*) FROM pg_constraint WHERE connamespace='{h.SCHEMA}'::regnamespace
     AND contype='p' AND conkey=ARRAY[1]::smallint[]) <> 4
 OR (SELECT string_agg(a.attname || ':' || pg_get_expr(d.adbin,d.adrelid), ',' ORDER BY a.attnum)
     FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid=d.adrelid AND a.attnum=d.adnum
     WHERE d.adrelid='{h.SCHEMA}.notes'::regclass)
 IS DISTINCT FROM $defaults$id:nextval('sparc_rehearsal.note_id'::regclass),body:'draft'::text,created_at:CURRENT_TIMESTAMP$defaults$
 OR NOT EXISTS (SELECT FROM pg_sequences WHERE schemaname='{h.SCHEMA}' AND sequencename='note_id'
     AND start_value=41 AND increment_by=1 AND min_value=1 AND NOT cycle AND cache_size=1)
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='{h.SCHEMA}'::regnamespace
     AND relowner<>(SELECT oid FROM pg_roles WHERE rolname='postgres'))
 OR pg_get_serial_sequence('{h.SCHEMA}.notes','id') IS DISTINCT FROM '{h.SCHEMA}.note_id'
 OR EXISTS (SELECT FROM pg_class c, LATERAL aclexplode(coalesce(c.relacl,acldefault(
       CASE WHEN c.relkind='S' THEN 's'::"char" ELSE 'r'::"char" END,c.relowner))) a
     WHERE c.relnamespace='{h.SCHEMA}'::regnamespace AND c.relkind IN ('r','S','v')
     AND a.grantee <> c.relowner AND NOT (c.relname='notes'
       AND a.grantee=(SELECT oid FROM pg_roles WHERE rolname='authenticated')
       AND a.privilege_type='SELECT' AND NOT a.is_grantable) AND NOT (c.relname IN ('recovery_cases','published_cases')
       AND a.grantee='sparc_rehearsal_reader'::regrole AND a.privilege_type='SELECT' AND NOT a.is_grantable))
 OR EXISTS (SELECT FROM pg_namespace n, LATERAL aclexplode(coalesce(n.nspacl,acldefault('n',n.nspowner))) a
     WHERE n.nspname='{h.SCHEMA}' AND a.grantee <> n.nspowner
       AND NOT (a.grantee IN ('authenticated'::regrole,'sparc_rehearsal_reader'::regrole)
                AND a.privilege_type='USAGE' AND NOT a.is_grantable))
 OR has_schema_privilege('anon','{h.SCHEMA}','USAGE')
 OR has_schema_privilege('authenticated','{h.SCHEMA}','CREATE')
 OR NOT has_schema_privilege('authenticated','{h.SCHEMA}','USAGE')
 OR NOT has_table_privilege('authenticated','{h.SCHEMA}.notes','SELECT')
 OR has_table_privilege('authenticated','{h.SCHEMA}.notes','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
 OR has_table_privilege('authenticated','{h.SCHEMA}.owners','SELECT,INSERT,UPDATE,DELETE')
 OR has_table_privilege('authenticated','{h.SCHEMA}.empty_table','SELECT,INSERT,UPDATE,DELETE')
 OR has_sequence_privilege('authenticated','{h.SCHEMA}.note_id','USAGE,SELECT,UPDATE')
THEN RAISE EXCEPTION 'fixture assertion rejected'; END IF;
END $assert$;
SET LOCAL ROLE authenticated;
SET LOCAL request.jwt.claim.sub = '{h.A}';
SELECT 1 / ((count(*)=1 AND min(id)=41)::int) FROM {h.SCHEMA}.notes;
SET LOCAL request.jwt.claim.sub = '{h.B}';
SELECT 1 / ((count(*)=1 AND min(id)=42)::int) FROM {h.SCHEMA}.notes;
SET LOCAL request.jwt.claim.sub = '{h.C}';
SELECT 1 / ((count(*)=0)::int) FROM {h.SCHEMA}.notes;
RESET ROLE;
"""


def assertions():
    return legacy_assertions() + metadata_assertions() + """
-- SPARC_ASSERT_EXPANDED
DO $expanded$ BEGIN
IF (SELECT jsonb_agg(jsonb_build_array(id,state,label) ORDER BY id)
 FROM sparc_rehearsal.recovery_cases) IS DISTINCT FROM '[[1,"draft","draft"],[2,"published","雪"]]'::jsonb
 OR (SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM sparc_rehearsal.published_cases v)
 IS DISTINCT FROM '[{"id":2,"label":"雪","label_length":1}]'::jsonb
 OR (SELECT string_agg(enumlabel,',' ORDER BY enumsortorder) FROM pg_enum
 WHERE enumtypid='sparc_rehearsal.note_state'::regtype) IS DISTINCT FROM 'draft,published'
 OR (SELECT string_agg(typname||':'||typtype::text,',' ORDER BY typname) FROM pg_type
 WHERE typnamespace='sparc_rehearsal'::regnamespace) IS DISTINCT FROM
 '_empty_table:b,_note_state:b,_notes:b,_owners:b,_published_cases:b,_recovery_cases:b,empty_table:c,note_state:e,notes:c,owners:c,published_cases:c,recovery_cases:c'
 OR EXISTS (SELECT FROM pg_type WHERE typnamespace='sparc_rehearsal'::regnamespace
 AND typowner<>'postgres'::regrole)
 OR EXISTS (SELECT FROM pg_type t, LATERAL aclexplode(coalesce(t.typacl,acldefault('T',t.typowner))) a
 WHERE t.typnamespace='sparc_rehearsal'::regnamespace AND
 (a.grantor<>t.typowner OR a.is_grantable OR a.privilege_type<>'USAGE' OR a.grantee NOT IN (0,t.typowner)))
 OR (SELECT string_agg(conrelid::regclass::text||':'||conname||':'||pg_get_constraintdef(oid),E'\n'
 ORDER BY conrelid::regclass::text,conname) FROM pg_constraint WHERE connamespace='sparc_rehearsal'::regnamespace)
 IS DISTINCT FROM $constraints$sparc_rehearsal.empty_table:empty_table_pkey:PRIMARY KEY (id)
sparc_rehearsal.notes:notes_owner_id_fkey:FOREIGN KEY (owner_id) REFERENCES sparc_rehearsal.owners(id) ON DELETE CASCADE
sparc_rehearsal.notes:notes_pkey:PRIMARY KEY (id)
sparc_rehearsal.owners:owners_pkey:PRIMARY KEY (id)
sparc_rehearsal.recovery_cases:recovery_cases_label_check:CHECK ((length(label) > 0))
sparc_rehearsal.recovery_cases:recovery_cases_pkey:PRIMARY KEY (id)$constraints$
 OR EXISTS (SELECT FROM pg_constraint WHERE connamespace='sparc_rehearsal'::regnamespace
 AND (NOT convalidated OR condeferrable OR condeferred))
 OR (SELECT string_agg(pg_get_indexdef(i.indexrelid),E'\n' ORDER BY c.relname)
 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relnamespace='sparc_rehearsal'::regnamespace)
 IS DISTINCT FROM $indexes$CREATE UNIQUE INDEX empty_table_pkey ON sparc_rehearsal.empty_table USING btree (id)
CREATE UNIQUE INDEX notes_pkey ON sparc_rehearsal.notes USING btree (id)
CREATE UNIQUE INDEX owners_pkey ON sparc_rehearsal.owners USING btree (id)
CREATE UNIQUE INDEX recovery_cases_pkey ON sparc_rehearsal.recovery_cases USING btree (id)
CREATE INDEX recovery_cases_published ON sparc_rehearsal.recovery_cases USING btree (id) WHERE (state = 'published'::sparc_rehearsal.note_state)$indexes$
 OR EXISTS (SELECT FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
 WHERE c.relnamespace='sparc_rehearsal'::regnamespace AND (NOT indisvalid OR NOT indisready OR NOT indislive))
 OR (SELECT count(*) FROM pg_attrdef d JOIN pg_class c ON c.oid=d.adrelid
 WHERE c.relnamespace='sparc_rehearsal'::regnamespace)<>3
 OR (SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid
 WHERE c.relnamespace='sparc_rehearsal'::regnamespace)<>1
 OR (SELECT pg_get_expr(polqual,polrelid) FROM pg_policy WHERE polrelid='sparc_rehearsal.notes'::regclass)
 IS DISTINCT FROM $policy$(owner_id = (NULLIF(current_setting('request.jwt.claim.sub'::text, true), ''::text))::uuid)$policy$
 OR EXISTS (SELECT FROM pg_policy WHERE polrelid='sparc_rehearsal.notes'::regclass AND polwithcheck IS NOT NULL)
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='sparc_rehearsal'::regnamespace AND
 (relpersistence<>'p' OR relispartition OR reltablespace<>0 OR
 (relname<>'notes' AND (relrowsecurity OR relforcerowsecurity)) OR
 reloptions IS DISTINCT FROM CASE WHEN relname='published_cases' THEN ARRAY['security_invoker=true'] ELSE NULL END))
 OR EXISTS (SELECT FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid
 JOIN pg_class p ON p.oid=i.inhparent WHERE c.relnamespace='sparc_rehearsal'::regnamespace
 OR p.relnamespace='sparc_rehearsal'::regnamespace)
 OR (SELECT count(*) FROM pg_rewrite r JOIN pg_class c ON c.oid=r.ev_class
 WHERE c.relnamespace='sparc_rehearsal'::regnamespace)<>1
 OR pg_get_viewdef('sparc_rehearsal.published_cases'::regclass) IS DISTINCT FROM $view$ SELECT id,
    label,
    sparc_rehearsal.label_length(label) AS label_length
   FROM sparc_rehearsal.recovery_cases
  WHERE (state = 'published'::sparc_rehearsal.note_state);$view$
 OR NOT EXISTS (SELECT FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
 WHERE p.oid='sparc_rehearsal.label_length(text)'::regprocedure AND p.proowner='postgres'::regrole
 AND l.lanname='sql' AND p.prokind='f' AND NOT p.prosecdef AND NOT p.proleakproof
 AND p.proisstrict AND NOT p.proretset AND p.provolatile='i' AND p.proparallel='u'
 AND p.prorettype='integer'::regtype AND p.proargtypes='25'::oidvector AND p.pronargdefaults=0
 AND p.provariadic=0 AND p.prosupport=0 AND p.proconfig IS NULL AND p.proargnames IS NULL
 AND p.proallargtypes IS NULL AND p.procost=100 AND p.prorows=0
 AND p.prosrc='SELECT pg_catalog.length($1)' AND p.prosqlbody IS NULL)
 OR (SELECT coalesce(proacl,acldefault('f',proowner)) FROM pg_proc
 WHERE oid='sparc_rehearsal.label_length(text)'::regprocedure)
 IS DISTINCT FROM acldefault('f','postgres'::regrole)
 OR (SELECT count(*) FROM pg_default_acl WHERE defaclnamespace='sparc_rehearsal'::regnamespace)<>1
 OR NOT EXISTS (SELECT FROM pg_default_acl WHERE defaclnamespace='sparc_rehearsal'::regnamespace
 AND defaclrole='postgres'::regrole AND defaclobjtype='r'
 AND defaclacl=ARRAY['sparc_rehearsal_reader=r/postgres']::aclitem[])
 OR EXISTS (SELECT FROM pg_namespace WHERE nspname IN ('sparc_rehearsal','supabase_migrations') AND nspowner<>'postgres'::regrole)
 OR EXISTS (SELECT FROM pg_namespace n WHERE n.nspname IN ('sparc_rehearsal','supabase_migrations')
 AND ARRAY(SELECT a::text FROM unnest(coalesce(n.nspacl,acldefault('n',n.nspowner))) a ORDER BY a::text)
 IS DISTINCT FROM CASE WHEN n.nspname='sparc_rehearsal' THEN
 ARRAY['authenticated=U/postgres','postgres=UC/postgres','sparc_rehearsal_reader=U/postgres']
 ELSE ARRAY['postgres=UC/postgres'] END)
 OR EXISTS (SELECT FROM pg_class c WHERE c.relnamespace IN
 ('sparc_rehearsal'::regnamespace,'supabase_migrations'::regnamespace) AND c.relkind IN ('r','v','S')
 AND ARRAY(SELECT a::text FROM unnest(coalesce(c.relacl,acldefault(
 CASE WHEN c.relkind='S' THEN 's'::"char" ELSE 'r'::"char" END,c.relowner))) a ORDER BY a::text)
 IS DISTINCT FROM CASE WHEN c.relname='notes' THEN ARRAY['authenticated=r/postgres','postgres=arwdDxtm/postgres']
 WHEN c.relname IN ('recovery_cases','published_cases') THEN ARRAY['postgres=arwdDxtm/postgres','sparc_rehearsal_reader=r/postgres']
 WHEN c.relkind='S' THEN ARRAY['postgres=rwU/postgres'] ELSE ARRAY['postgres=arwdDxtm/postgres'] END)
 OR EXISTS (SELECT FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
 JOIN pg_type t ON t.oid=a.atttypid
 WHERE c.relnamespace IN ('sparc_rehearsal'::regnamespace,'supabase_migrations'::regnamespace)
 AND c.relkind IN ('r','v') AND a.attnum>0
 AND (a.attisdropped OR a.attidentity<>'' OR a.attgenerated<>'' OR a.attcollation<>t.typcollation
 OR cardinality(a.attacl)>0))
 OR (SELECT string_agg(c.relname||':'||c.relkind::text,',' ORDER BY c.relname)
 FROM pg_class c WHERE relnamespace='supabase_migrations'::regnamespace)
 IS DISTINCT FROM 'schema_migrations:r,schema_migrations_pkey:i'
 OR EXISTS (SELECT FROM pg_proc WHERE pronamespace='supabase_migrations'::regnamespace)
 OR (SELECT count(*) FROM pg_type WHERE typnamespace='supabase_migrations'::regnamespace)<>2
 OR (SELECT string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull::text,',' ORDER BY a.attnum)
 FROM pg_attribute a WHERE a.attrelid='supabase_migrations.schema_migrations'::regclass AND a.attnum>0 AND NOT a.attisdropped)
 IS DISTINCT FROM 'version:text:true,name:text:false,statements:text[]:false'
 OR (SELECT count(*) FROM pg_constraint WHERE connamespace='supabase_migrations'::regnamespace)<>1
 OR NOT EXISTS (SELECT FROM pg_constraint WHERE conrelid='supabase_migrations.schema_migrations'::regclass
 AND conname='schema_migrations_pkey' AND contype='p' AND conkey=ARRAY[1]::smallint[]
 AND convalidated AND NOT condeferrable AND NOT condeferred)
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='supabase_migrations'::regnamespace
 AND (relowner<>'postgres'::regrole OR relrowsecurity OR relforcerowsecurity OR relispartition OR relpersistence<>'p' OR reloptions IS NOT NULL))
 OR EXISTS (SELECT FROM pg_attrdef WHERE adrelid='supabase_migrations.schema_migrations'::regclass)
 OR EXISTS (SELECT FROM pg_trigger WHERE tgrelid='supabase_migrations.schema_migrations'::regclass)
 OR EXISTS (SELECT FROM pg_policy WHERE polrelid='supabase_migrations.schema_migrations'::regclass)
 OR EXISTS (SELECT FROM pg_class c,LATERAL aclexplode(coalesce(c.relacl,acldefault('r',c.relowner))) a
 WHERE c.relnamespace='supabase_migrations'::regnamespace AND a.grantee<>c.relowner)
 OR EXISTS (SELECT FROM pg_namespace n,LATERAL aclexplode(coalesce(n.nspacl,acldefault('n',n.nspowner))) a
 WHERE n.nspname='supabase_migrations' AND a.grantee<>n.nspowner)
 OR EXISTS (SELECT FROM pg_default_acl WHERE defaclnamespace='supabase_migrations'::regnamespace)
 OR (SELECT seqtypid<>'bigint'::regtype OR seqmax<>9223372036854775807 FROM pg_sequence WHERE seqrelid='sparc_rehearsal.note_id'::regclass)
THEN RAISE EXCEPTION 'expanded catalog assertion rejected'; END IF;
END $expanded$;
DO $permissions$ DECLARE role_name text; BEGIN
FOREACH role_name IN ARRAY ARRAY['sparc_rehearsal_reader','sparc_rehearsal_member'] LOOP
 IF NOT has_schema_privilege(role_name,'sparc_rehearsal','USAGE')
 OR has_schema_privilege(role_name,'sparc_rehearsal','CREATE')
 OR has_schema_privilege(role_name,'supabase_migrations','USAGE,CREATE')
 OR NOT has_table_privilege(role_name,'sparc_rehearsal.recovery_cases','SELECT')
 OR NOT has_table_privilege(role_name,'sparc_rehearsal.published_cases','SELECT')
 OR NOT has_function_privilege(role_name,'sparc_rehearsal.label_length(text)','EXECUTE')
 OR has_table_privilege(role_name,'sparc_rehearsal.recovery_cases','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
 OR has_table_privilege(role_name,'sparc_rehearsal.published_cases','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
 OR has_table_privilege(role_name,'sparc_rehearsal.notes','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
 OR has_table_privilege(role_name,'sparc_rehearsal.owners','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
 OR has_table_privilege(role_name,'sparc_rehearsal.empty_table','SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
 OR has_sequence_privilege(role_name,'sparc_rehearsal.note_id','USAGE,SELECT,UPDATE')
 THEN RAISE EXCEPTION 'expanded permissions rejected'; END IF;
END LOOP;
END $permissions$;
"""


NEW_OBJECT_PROBE = """
-- Destination only; all ephemeral mutations roll back to the savepoint.
SAVEPOINT expanded_probe;
INSERT INTO sparc_rehearsal.recovery_cases VALUES (3,'published','probe');
DO $probe$ BEGIN
 BEGIN INSERT INTO sparc_rehearsal.recovery_cases VALUES (4,'draft','');
 RAISE EXCEPTION 'empty label accepted'; EXCEPTION WHEN check_violation THEN NULL; END;
 BEGIN INSERT INTO sparc_rehearsal.recovery_cases VALUES (4,'invalid','label');
 RAISE EXCEPTION 'invalid enum accepted'; EXCEPTION WHEN invalid_text_representation THEN NULL; END;
END $probe$;
SET LOCAL ROLE sparc_rehearsal_member;
DO $denied$ BEGIN
 BEGIN INSERT INTO sparc_rehearsal.recovery_cases VALUES (5,'draft','denied');
 RAISE EXCEPTION 'member write permitted'; EXCEPTION WHEN insufficient_privilege THEN NULL; END;
 BEGIN GRANT sparc_rehearsal_reader TO authenticated;
 RAISE EXCEPTION 'role administration permitted'; EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $denied$;
RESET ROLE;
-- View must not bypass revoked base-table access even though view SELECT remains.
REVOKE SELECT ON sparc_rehearsal.recovery_cases FROM sparc_rehearsal_reader;
SET LOCAL ROLE sparc_rehearsal_member;
DO $denied$ BEGIN
 BEGIN PERFORM * FROM sparc_rehearsal.published_cases;
 RAISE EXCEPTION 'view bypassed invoker'; EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $denied$;
RESET ROLE;
ROLLBACK TO SAVEPOINT expanded_probe;
RELEASE SAVEPOINT expanded_probe;
"""


# PostgreSQL's creator edge grants ADMIN but not SET. Only mutating, authorized
# transactions add a temporary postgres-granted edge; the superuser-granted
# canonical edge remains untouched. Read-only capture never impersonates roles.
PROBE_ENABLE = """
DO $edge$ BEGIN
IF EXISTS (SELECT FROM pg_auth_members WHERE roleid='sparc_rehearsal_member'::regrole
 AND member='postgres'::regrole AND grantor='postgres'::regrole)
THEN RAISE EXCEPTION 'temporary probe edge collision'; END IF;
END $edge$;
GRANT sparc_rehearsal_member TO postgres WITH ADMIN FALSE, INHERIT FALSE, SET TRUE GRANTED BY postgres;
"""
PROBE_DISABLE = """
RESET ROLE;
REVOKE sparc_rehearsal_member FROM postgres GRANTED BY postgres RESTRICT;
"""
ROLE_READ_PROBE = """
SET LOCAL ROLE sparc_rehearsal_member;
SELECT 1/((count(*)=2)::int) FROM sparc_rehearsal.recovery_cases;
SELECT 1/((count(*)=1 AND min(label_length)=1)::int) FROM sparc_rehearsal.published_cases;
SELECT 1/((sparc_rehearsal.label_length('雪')=1)::int);
DO $denied$ BEGIN
 BEGIN PERFORM * FROM sparc_rehearsal.notes; RAISE EXCEPTION 'old table access permitted';
 EXCEPTION WHEN insufficient_privilege THEN NULL; END;
 BEGIN PERFORM * FROM supabase_migrations.schema_migrations; RAISE EXCEPTION 'history access permitted';
 EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $denied$;
RESET ROLE;
"""


FUTURE_PROBE = """
SAVEPOINT future_probe;
CREATE TABLE sparc_rehearsal.future_probe (id integer);
INSERT INTO sparc_rehearsal.future_probe VALUES (1);
DO $acl$ BEGIN
IF (SELECT relacl FROM pg_class WHERE oid='sparc_rehearsal.future_probe'::regclass)
 IS DISTINCT FROM ARRAY['postgres=arwdDxtm/postgres','sparc_rehearsal_reader=r/postgres']::aclitem[]
THEN RAISE EXCEPTION 'future default ACL rejected'; END IF;
END $acl$;

SET LOCAL ROLE sparc_rehearsal_member;
SELECT 1/((count(*)=1)::int) FROM sparc_rehearsal.future_probe;
DO $denied$ BEGIN
 BEGIN INSERT INTO sparc_rehearsal.future_probe VALUES (2);
 RAISE EXCEPTION 'future write permitted'; EXCEPTION WHEN insufficient_privilege THEN NULL; END;
END $denied$;
RESET ROLE;
ROLLBACK TO SAVEPOINT future_probe;
RELEASE SAVEPOINT future_probe;
"""
SOURCE_PROBE = PROBE_ENABLE + ROLE_READ_PROBE + NEW_OBJECT_PROBE + PROBE_DISABLE
PROBE = h.PROBE + PROBE_ENABLE + ROLE_READ_PROBE + NEW_OBJECT_PROBE + FUTURE_PROBE + PROBE_DISABLE


# Names and ordered ACL entries, never cluster-local OIDs. This receipt is
# supplementary: fixed assertions run first; it is not a learned live baseline.
CATALOG_QUERY = """SELECT jsonb_build_object(
 'relations',(SELECT jsonb_agg(jsonb_build_array(n.nspname,c.relname,c.relkind,c.relowner::regrole::text,
 c.relrowsecurity,c.relforcerowsecurity,c.reloptions,
 ARRAY(SELECT a::text FROM unnest(coalesce(c.relacl,acldefault(
 CASE WHEN c.relkind='S' THEN 's'::"char" ELSE 'r'::"char" END,c.relowner))) a ORDER BY a::text))
 ORDER BY n.nspname,c.relname) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname IN ('sparc_rehearsal','supabase_migrations')),
 'schemas',(SELECT jsonb_agg(jsonb_build_array(nspname,nspowner::regrole::text,
 ARRAY(SELECT a::text FROM unnest(coalesce(nspacl,acldefault('n',nspowner))) a ORDER BY a::text)) ORDER BY nspname)
 FROM pg_namespace WHERE nspname IN ('sparc_rehearsal','supabase_migrations')),
 'types',(SELECT jsonb_agg(jsonb_build_array(t.typname,t.typtype,t.typowner::regrole::text,
 ARRAY(SELECT a::text FROM unnest(coalesce(t.typacl,acldefault('T',t.typowner))) a ORDER BY a::text),
 ARRAY(SELECT enumlabel FROM pg_enum WHERE enumtypid=t.oid ORDER BY enumsortorder)) ORDER BY t.typname)
 FROM pg_type t WHERE typnamespace='sparc_rehearsal'::regnamespace),
 'functions',(SELECT jsonb_agg(jsonb_build_array(pg_get_functiondef(p.oid),
 ARRAY(SELECT a::text FROM unnest(coalesce(p.proacl,acldefault('f',p.proowner))) a ORDER BY a::text)) ORDER BY p.proname)
 FROM pg_proc p WHERE pronamespace='sparc_rehearsal'::regnamespace),
 'view',pg_get_viewdef('sparc_rehearsal.published_cases'::regclass),
 'indexes',(SELECT jsonb_agg(jsonb_build_array(pg_get_indexdef(i.indexrelid),i.indisvalid,i.indisready)
 ORDER BY c.relname) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
 WHERE c.relnamespace IN ('sparc_rehearsal'::regnamespace,'supabase_migrations'::regnamespace)),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid),
 convalidated,condeferrable,condeferred) ORDER BY conrelid::regclass::text,conname)
 FROM pg_constraint WHERE connamespace IN ('sparc_rehearsal'::regnamespace,'supabase_migrations'::regnamespace)),
 'default_privileges',(SELECT jsonb_agg(jsonb_build_array(defaclrole::regrole::text,
 defaclnamespace::regnamespace::text,defaclobjtype,ARRAY(SELECT a::text FROM unnest(defaclacl) a ORDER BY a::text))
 ORDER BY defaclrole::regrole::text,defaclobjtype) FROM pg_default_acl
 WHERE defaclnamespace IN ('sparc_rehearsal'::regnamespace,'supabase_migrations'::regnamespace)),
 'creator_edges',(SELECT jsonb_agg(jsonb_build_array(roleid::regrole::text,member::regrole::text,
 grantor::regrole::text,admin_option,inherit_option,set_option) ORDER BY roleid::regrole::text)
 FROM pg_auth_members WHERE roleid IN ('sparc_rehearsal_reader'::regrole,'sparc_rehearsal_member'::regrole)
 AND member='postgres'::regrole))"""
CATALOG_KEYS = {'relations','schemas','types','functions','view','indexes','constraints',
                'default_privileges','creator_edges'}


def validate_catalog(raw):
    try:
        h.require(type(raw) is bytes and len(raw) <= 65536, 'invalid expanded catalog')
        value = json.loads(raw.decode('utf-8'), object_pairs_hook=h.pairs)
        h.require(type(value) is dict and set(value) == CATALOG_KEYS, 'invalid expanded catalog')
        h.require(type(value['view']) is str and all(type(value[k]) is list for k in CATALOG_KEYS - {'view'}),
                  'invalid expanded catalog')
        encoded = json.dumps(value, ensure_ascii=False, allow_nan=False)
        encoded.encode('utf-8', errors='strict')
        h.require('\\u0000' not in encoded, 'invalid expanded catalog')
        return value
    except (ValueError, UnicodeError, TypeError, RecursionError, h.RehearsalError):
        raise h.RehearsalError('invalid expanded catalog') from None


def capture_catalog(cfg, work):
    output = h.psql(cfg, work, 'BEGIN READ ONLY; SET LOCAL search_path=pg_catalog;\n'
                    + guard() + assertions() + CATALOG_QUERY + '; ROLLBACK;')
    value = validate_catalog(output.rstrip().split(b'\n')[-1])
    return json.dumps(value).encode()


def catalog_assertion(value):
    return 'DO $receipt$ BEGIN IF (' + CATALOG_QUERY + ') IS DISTINCT FROM ' + json_sql(value) + """
THEN RAISE EXCEPTION 'expanded catalog receipt mismatch'; END IF; END $receipt$;
"""
