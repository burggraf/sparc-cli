#!/usr/bin/env python3
"""Developer-only native PG17 fixture experiment; NOT Supabase/Auth recovery.

Parent/operator authorization required. SQL archives are trusted executable code.
No live operation is part of normal tests. See the linked rehearsal plan.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import stat
import subprocess
import sys
import tempfile

LIMIT = 2 * 1024 * 1024
SCHEMA = "sparc_rehearsal"
A = "11111111-1111-4111-8111-111111111111"
B = "22222222-2222-4222-8222-222222222222"
C = "33333333-3333-4333-8333-333333333333"
BASE_SCHEMAS = "auth,extensions,graphql,graphql_public,pgbouncer,public,realtime,storage,vault"
EXTENSIONS = "pg_stat_statements:1.11:extensions,pgcrypto:1.3:extensions,plpgsql:1.0:pg_catalog,supabase_vault:0.3.1:vault,uuid-ossp:1.1:extensions"
TRIGGERS = "issue_graphql_placeholder:sql_drop:O,issue_pg_cron_access:ddl_command_end:O,issue_pg_graphql_access:ddl_command_end:O,issue_pg_net_access:ddl_command_end:O,pgrst_ddl_watch:ddl_command_end:O,pgrst_drop_watch:sql_drop:O"


class RehearsalError(Exception):
    """Only fixed, non-sensitive messages cross the CLI boundary."""


def require(condition, message="invalid rehearsal input"):
    if not condition:
        raise RehearsalError(message)


def read_file(path, limit, private=False):
    path = Path(path)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode), "expected regular non-symlink file")
    require(not private or info.st_mode & 0o077 == 0, "file must be private")
    require(info.st_size <= limit, "input exceeds limit")
    with path.open("rb") as handle:
        data = handle.read(limit + 1)
    require(len(data) <= limit, "input exceeds limit")
    return data


def pairs(items):
    result = {}
    for key, value in items:
        require(key not in result, "duplicate JSON field")
        result[key] = value
    return result


def read_json(path, keys):
    try:
        result = json.loads(read_file(path, 16384), object_pairs_hook=pairs)
    except (ValueError, UnicodeError):
        raise RehearsalError("invalid JSON") from None
    require(type(result) is dict and set(result) == set(keys), "invalid JSON fields")
    return result


def absolute_file(value, private=False):
    require(type(value) is str and len(value) <= 4096 and Path(value).is_absolute())
    path = Path(value)
    require(stat.S_ISREG(path.lstat().st_mode), "expected regular non-symlink file")
    if private:
        require(path.stat().st_mode & 0o077 == 0, "file must be private")
    return path


def config(path):
    cfg = read_json(path, ("version", "project_ref", "host", "pg_bin", "sparc", "pgpass", "ca"))
    require(type(cfg["version"]) is int and cfg["version"] == 1)
    require(type(cfg["project_ref"]) is str and re.fullmatch(r"[a-z]{20}", cfg["project_ref"]))
    require(type(cfg["host"]) is str and re.fullmatch(
        r"aws-[0-9]+-[a-z]{2}-[a-z]+-[0-9]+\.pooler\.supabase\.com", cfg["host"]))
    require(type(cfg["pg_bin"]) is str and Path(cfg["pg_bin"]).is_absolute())
    for name in ("pg_dump", "psql"):
        require(os.access(absolute_file(str(Path(cfg["pg_bin"]) / name)), os.X_OK), "tool not executable")
    require(os.access(absolute_file(cfg["sparc"]), os.X_OK), "tool not executable")
    absolute_file(cfg["pgpass"], private=True)
    require(Path(cfg["pgpass"]).parent.stat().st_mode & 0o077 == 0, "credential directory must be private")
    # Exactly one matching record, no wildcard, comments, or unrelated credentials.
    password_file = read_file(cfg["pgpass"], 16384, private=True).decode("utf-8")
    prefix = f'{cfg["host"]}:5432:postgres:postgres.{cfg["project_ref"]}:'
    require(password_file.startswith(prefix) and password_file.count("\n") <= 1
            and "\n" not in password_file.rstrip("\n")
            and "\r" not in password_file and len(password_file.rstrip("\n")) > len(prefix),
            "passfile must contain one exact tenant record")
    # libpq understands escaped colons/backslashes in the final password field.
    require(re.fullmatch(r"(?:[^:\\\n]|\\[:\\])+\n?", password_file[len(prefix):]) is not None,
            "invalid passfile record")
    ca = read_file(absolute_file(cfg["ca"]), 65536)
    require(b"-----BEGIN CERTIFICATE-----" in ca and b"-----END CERTIFICATE-----" in ca,
            "CA certificate required")
    return cfg


def private_write(path, data):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "wb") as handle:
        handle.write(data)


def child_limit():
    resource.setrlimit(resource.RLIMIT_FSIZE, (LIMIT, LIMIT))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def run(argv, work, cfg=None, sql=None):
    # No ambient HOME/PG*/Supabase/cloud credentials, configs, PATH tools or psqlrc.
    env = {"PATH": "/usr/bin:/bin", "HOME": str(work), "LC_ALL": "C", "TZ": "UTC"}
    if cfg is not None:
        env.update(PGHOST=cfg["host"], PGPORT="5432", PGDATABASE="postgres",
                   PGUSER="postgres." + cfg["project_ref"], PGPASSFILE=cfg["pgpass"],
                   PGSSLMODE="verify-full", PGSSLROOTCERT=cfg["ca"], PGCONNECT_TIMEOUT="10",
                   PGAPPNAME="sparc-fixture-rehearsal", PGOPTIONS="-c statement_timeout=15000 -c lock_timeout=5000 -c timezone=UTC -c standard_conforming_strings=on")
    with tempfile.TemporaryFile(dir=work) as output, tempfile.TemporaryFile(dir=work) as stdin:
        if sql is not None:
            data = sql.encode("utf-8") if isinstance(sql, str) else sql
            require(len(data) <= LIMIT, "SQL exceeds limit")
            stdin.write(data)
            stdin.seek(0)
        try:
            result = subprocess.run(argv, stdin=stdin, stdout=output, stderr=subprocess.DEVNULL,
                                    cwd=work, env=env, timeout=60, check=False,
                                    preexec_fn=child_limit)
        except (OSError, subprocess.TimeoutExpired):
            raise RehearsalError("tool failed or timed out; stop and quarantine any target") from None
        require(result.returncode == 0, "tool failed; stop and quarantine any target")
        output.seek(0)
        data = output.read(LIMIT + 1)
        require(len(data) < LIMIT, "tool output exceeds limit")
        return data


def versions(cfg, work):
    for name in ("psql", "pg_dump"):
        output = run([str(Path(cfg["pg_bin"]) / name), "--version"], work)
        require(re.fullmatch(name.encode() + rb" \(PostgreSQL\) 17\.[0-9]+[^\n]*\n?", output),
                "native PostgreSQL 17 tools required")


def psql(cfg, work, sql):
    return run([str(Path(cfg["pg_bin"]) / "psql"), "-X", "-w", "-qAt",
                "--set", "ON_ERROR_STOP=1", "--file", "-"], work, cfg, sql)


def guard(fixture=False):
    schemas = sorted(BASE_SCHEMAS.split(",") + ([SCHEMA] if fixture else []))
    return f"""
-- SPARC_GUARD_{'FIXTURE' if fixture else 'EMPTY'}
DO $guard$ BEGIN
IF current_database() <> 'postgres' OR current_user <> 'postgres'
 OR current_setting('server_version_num')::int / 10000 <> 17
 OR (SELECT string_agg(nspname, ',' ORDER BY nspname) FROM pg_namespace
     WHERE nspname !~ '^pg_' AND nspname <> 'information_schema') IS DISTINCT FROM '{','.join(schemas)}'
 OR (SELECT string_agg(e.extname || ':' || e.extversion || ':' || n.nspname, ',' ORDER BY e.extname)
     FROM pg_extension e JOIN pg_namespace n ON n.oid=e.extnamespace) IS DISTINCT FROM '{EXTENSIONS}'
 OR (SELECT string_agg(evtname || ':' || evtevent || ':' || evtenabled::text, ',' ORDER BY evtname)
     FROM pg_event_trigger) IS DISTINCT FROM '{TRIGGERS}'
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='public'::regnamespace)
 OR EXISTS (SELECT FROM pg_proc WHERE pronamespace='public'::regnamespace)
 OR EXISTS (SELECT FROM pg_type WHERE typnamespace='public'::regnamespace)
 OR EXISTS (SELECT FROM pg_foreign_server) OR EXISTS (SELECT FROM pg_subscription)
 OR EXISTS (SELECT FROM pg_default_acl WHERE defaclnamespace=0)
 OR EXISTS (SELECT FROM auth.users) OR EXISTS (SELECT FROM auth.identities)
 OR EXISTS (SELECT FROM storage.buckets) OR EXISTS (SELECT FROM vault.secrets)
 OR (SELECT count(*) FROM pg_publication) <> 1
 OR NOT EXISTS (SELECT FROM pg_publication WHERE pubname='supabase_realtime' AND NOT puballtables)
 OR EXISTS (SELECT FROM pg_publication_tables)
THEN RAISE EXCEPTION 'fixture baseline rejected'; END IF;
END $guard$;
"""


SEED = f"""
CREATE SCHEMA {SCHEMA} AUTHORIZATION postgres;
REVOKE ALL ON SCHEMA {SCHEMA} FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA {SCHEMA} REVOKE ALL ON TABLES FROM PUBLIC, anon, authenticated;
ALTER DEFAULT PRIVILEGES IN SCHEMA {SCHEMA} REVOKE ALL ON SEQUENCES FROM PUBLIC, anon, authenticated;
CREATE TABLE {SCHEMA}.owners (id uuid PRIMARY KEY);
CREATE SEQUENCE {SCHEMA}.note_id START 41;
CREATE TABLE {SCHEMA}.notes (
 id integer PRIMARY KEY DEFAULT nextval('{SCHEMA}.note_id'),
 owner_id uuid NOT NULL REFERENCES {SCHEMA}.owners(id) ON DELETE CASCADE,
 body text NOT NULL DEFAULT 'draft', optional text,
 created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP);
ALTER SEQUENCE {SCHEMA}.note_id OWNED BY {SCHEMA}.notes.id;
CREATE TABLE {SCHEMA}.empty_table (id integer PRIMARY KEY);
INSERT INTO {SCHEMA}.owners VALUES ('{A}'), ('{B}');
INSERT INTO {SCHEMA}.notes(owner_id,body,optional,created_at) VALUES
 ('{A}', E'hello 雪\\nline', NULL, '2026-09-18 00:00:00+00'),
 ('{B}', 'quote '' and \\ slash', 'present', '2026-09-18 00:00:00+00');
ALTER TABLE {SCHEMA}.notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE {SCHEMA}.notes FORCE ROW LEVEL SECURITY;
CREATE POLICY owner_read ON {SCHEMA}.notes FOR SELECT TO authenticated
 USING (owner_id = NULLIF(current_setting('request.jwt.claim.sub', true), '')::uuid);
GRANT USAGE ON SCHEMA {SCHEMA} TO authenticated;
GRANT SELECT ON {SCHEMA}.notes TO authenticated;
"""


def assertions():
    return f"""
-- SPARC_ASSERT_FIXTURE
-- pg_dump sets row_security=off; role probes must actually exercise policies.
SET LOCAL row_security=on;
DO $assert$ BEGIN
IF (SELECT jsonb_agg(id ORDER BY id) FROM {SCHEMA}.owners)
 IS DISTINCT FROM '["{A}","{B}"]'::jsonb
 OR (SELECT jsonb_agg(jsonb_build_array(id,owner_id,body,optional,created_at) ORDER BY id) FROM {SCHEMA}.notes)
 IS DISTINCT FROM jsonb_build_array(
 jsonb_build_array(41,'{A}',E'hello 雪\\nline',NULL,'2026-09-18T00:00:00+00:00'),
 jsonb_build_array(42,'{B}','quote '' and \\ slash','present','2026-09-18T00:00:00+00:00'))
 OR EXISTS (SELECT FROM {SCHEMA}.empty_table)
 OR (SELECT last_value <> 42 OR NOT is_called FROM {SCHEMA}.note_id)
 OR (SELECT string_agg(c.relname || ':' || c.relkind::text, ',' ORDER BY c.relname)
     FROM pg_class c WHERE relnamespace='{SCHEMA}'::regnamespace)
 IS DISTINCT FROM 'empty_table:r,empty_table_pkey:i,note_id:S,notes:r,notes_pkey:i,owners:r,owners_pkey:i'
 OR EXISTS (SELECT FROM pg_proc WHERE pronamespace='{SCHEMA}'::regnamespace)
 OR (SELECT count(*) FROM pg_type WHERE typnamespace='{SCHEMA}'::regnamespace) <> 6
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='{SCHEMA}'::regnamespace
     AND relname IN ('owners','empty_table') AND (relrowsecurity OR relforcerowsecurity))
 OR EXISTS (SELECT FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid
            WHERE c.relnamespace='{SCHEMA}'::regnamespace AND NOT t.tgisinternal)
 OR (SELECT count(*) FROM pg_constraint WHERE connamespace='{SCHEMA}'::regnamespace) <> 4
 OR NOT EXISTS (SELECT FROM pg_constraint WHERE conrelid='{SCHEMA}.notes'::regclass
     AND contype='f' AND confrelid='{SCHEMA}.owners'::regclass AND confdeltype='c'
     AND convalidated AND conkey=ARRAY[2]::smallint[] AND confkey=ARRAY[1]::smallint[])
 OR NOT EXISTS (SELECT FROM pg_class WHERE oid='{SCHEMA}.notes'::regclass AND relrowsecurity AND relforcerowsecurity)
 OR (SELECT count(*) FROM pg_policy WHERE polrelid='{SCHEMA}.notes'::regclass) <> 1
 OR NOT EXISTS (SELECT FROM pg_policy WHERE polrelid='{SCHEMA}.notes'::regclass
     AND polname='owner_read' AND polcmd='r' AND polpermissive
     AND polroles=ARRAY[(SELECT oid FROM pg_roles WHERE rolname='authenticated')])
 OR (SELECT string_agg(c.relname || '.' || a.attname || ':' || format_type(a.atttypid,a.atttypmod)
       || ':' || a.attnotnull::text, ',' ORDER BY c.relname,a.attnum)
     FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
     WHERE c.relnamespace='{SCHEMA}'::regnamespace AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped)
 IS DISTINCT FROM 'empty_table.id:integer:true,notes.id:integer:true,notes.owner_id:uuid:true,notes.body:text:true,notes.optional:text:false,notes.created_at:timestamp with time zone:true,owners.id:uuid:true'
 OR (SELECT count(*) FROM pg_constraint WHERE connamespace='{SCHEMA}'::regnamespace
     AND contype='p' AND conkey=ARRAY[1]::smallint[]) <> 3
 OR (SELECT string_agg(a.attname || ':' || pg_get_expr(d.adbin,d.adrelid), ',' ORDER BY a.attnum)
     FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid=d.adrelid AND a.attnum=d.adnum
     WHERE d.adrelid='{SCHEMA}.notes'::regclass)
 IS DISTINCT FROM $defaults$id:nextval('sparc_rehearsal.note_id'::regclass),body:'draft'::text,created_at:CURRENT_TIMESTAMP$defaults$
 OR NOT EXISTS (SELECT FROM pg_sequences WHERE schemaname='{SCHEMA}' AND sequencename='note_id'
     AND start_value=41 AND increment_by=1 AND min_value=1 AND NOT cycle AND cache_size=1)
 OR EXISTS (SELECT FROM pg_class WHERE relnamespace='{SCHEMA}'::regnamespace
     AND relowner<>(SELECT oid FROM pg_roles WHERE rolname='postgres'))
 OR pg_get_serial_sequence('{SCHEMA}.notes','id') IS DISTINCT FROM '{SCHEMA}.note_id'
 OR EXISTS (SELECT FROM pg_class c, LATERAL aclexplode(coalesce(c.relacl,acldefault(
       CASE WHEN c.relkind='S' THEN 's'::"char" ELSE 'r'::"char" END,c.relowner))) a
     WHERE c.relnamespace='{SCHEMA}'::regnamespace AND c.relkind IN ('r','S')
     AND a.grantee <> c.relowner AND NOT (c.relname='notes'
       AND a.grantee=(SELECT oid FROM pg_roles WHERE rolname='authenticated')
       AND a.privilege_type='SELECT' AND NOT a.is_grantable))
 OR EXISTS (SELECT FROM pg_namespace n, LATERAL aclexplode(coalesce(n.nspacl,acldefault('n',n.nspowner))) a
     WHERE n.nspname='{SCHEMA}' AND a.grantee <> n.nspowner
       AND NOT (a.grantee=(SELECT oid FROM pg_roles WHERE rolname='authenticated')
                AND a.privilege_type='USAGE' AND NOT a.is_grantable))
 OR has_schema_privilege('anon','{SCHEMA}','USAGE')
 OR has_schema_privilege('authenticated','{SCHEMA}','CREATE')
 OR NOT has_schema_privilege('authenticated','{SCHEMA}','USAGE')
 OR NOT has_table_privilege('authenticated','{SCHEMA}.notes','SELECT')
 OR has_table_privilege('authenticated','{SCHEMA}.notes','INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
 OR has_table_privilege('authenticated','{SCHEMA}.owners','SELECT,INSERT,UPDATE,DELETE')
 OR has_table_privilege('authenticated','{SCHEMA}.empty_table','SELECT,INSERT,UPDATE,DELETE')
 OR has_sequence_privilege('authenticated','{SCHEMA}.note_id','USAGE,SELECT,UPDATE')
THEN RAISE EXCEPTION 'fixture assertion rejected'; END IF;
END $assert$;
SET LOCAL ROLE authenticated;
SET LOCAL request.jwt.claim.sub = '{A}';
SELECT 1 / ((count(*)=1 AND min(id)=41)::int) FROM {SCHEMA}.notes;
SET LOCAL request.jwt.claim.sub = '{B}';
SELECT 1 / ((count(*)=1 AND min(id)=42)::int) FROM {SCHEMA}.notes;
SET LOCAL request.jwt.claim.sub = '{C}';
SELECT 1 / ((count(*)=0)::int) FROM {SCHEMA}.notes;
RESET ROLE;
"""


PROBE = f"""
-- Target only: nextval is NOT rolled back. Explicitly return sequence state.
INSERT INTO {SCHEMA}.owners VALUES ('{C}');
INSERT INTO {SCHEMA}.notes(owner_id) VALUES ('{C}');
DO $probe$ BEGIN
IF NOT EXISTS (SELECT FROM {SCHEMA}.notes WHERE id=43 AND owner_id='{C}'
 AND body='draft' AND optional IS NULL AND created_at=CURRENT_TIMESTAMP)
THEN RAISE EXCEPTION 'default probe rejected'; END IF;
END $probe$;
DELETE FROM {SCHEMA}.owners WHERE id='{C}';
DO $probe$ BEGIN
IF EXISTS (SELECT FROM {SCHEMA}.notes WHERE owner_id='{C}')
THEN RAISE EXCEPTION 'cascade probe rejected'; END IF;
END $probe$;
SELECT setval('{SCHEMA}.note_id',42,true);
"""


def check(cfg, work, fixture=False):
    sql = "BEGIN READ ONLY;\n" + guard(fixture)
    if fixture:
        sql += assertions()
    sql += "SELECT 'sparc-check-ok'; ROLLBACK;"
    require(psql(cfg, work, sql).rstrip().endswith(b"sparc-check-ok"), "database check incomplete")


def main(args):
    require(len(args) in (2, 4) and args[0] in ("seed", "capture", "restore"),
            "usage: seed CONFIG | capture CONFIG NEW_ARCHIVE RECIPIENT | restore CONFIG ARCHIVE IDENTITY")
    mode = args[0]
    require((mode == "seed" and len(args) == 2) or (mode != "seed" and len(args) == 4))
    cfg = config(args[1])
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="sparc-fixture-") as directory:
        work = Path(directory)
        versions(cfg, work)
        if mode == "seed":
            check(cfg, work)
            output = psql(cfg, work, "BEGIN;\n" + guard() + SEED + assertions() + "COMMIT; SELECT 'sparc-seed-ok';")
            require(output.rstrip().endswith(b"sparc-seed-ok"), "seed result incomplete; quarantine target")
        elif mode == "capture":
            archive = Path(args[2]).absolute()
            require(not archive.exists() and not archive.is_symlink(), "archive destination already exists")
            require(re.fullmatch(r"age1[a-z0-9]{58}", args[3]) is not None, "native age recipient required")
            check(cfg, work, fixture=True)
            sql = run([str(Path(cfg["pg_bin"]) / "pg_dump"), "--no-password", "--format=plain",
                       "--schema=" + SCHEMA, "--strict-names", "--no-owner", "--no-comments",
                       "--no-security-labels", "--lock-wait-timeout=5s"], work, cfg)
            require(sql.startswith(b"--\n-- PostgreSQL database dump\n")
                    and b"-- PostgreSQL database dump complete\n" in sql, "incomplete fixture dump")
            check(cfg, work, fixture=True)
            material = work / "material"
            material.mkdir(mode=0o700)
            private_write(material / "fixture.sql", sql)
            private_write(material / "recovery.json", json.dumps({"version": 1,
                          "source_ref": cfg["project_ref"], "sha256": hashlib.sha256(sql).hexdigest()}).encode())
            run([cfg["sparc"], "pack", str(material), str(archive), args[3]], work)
        else:
            print("rehearsal: restore executes trusted SQL; not a sandbox or full recovery", file=sys.stderr)
            archive = Path(args[2]).absolute()
            require(archive.is_dir() and not archive.is_symlink(), "invalid archive directory")
            expected = {"manifest.age", "00000000.age", "00000001.age"}
            total = 0
            for entry in archive.iterdir():
                require(entry.name in expected, "unexpected archive entry")
                expected.remove(entry.name)
                info = entry.lstat()
                require(stat.S_ISREG(info.st_mode) and info.st_size <= LIMIT, "unsafe archive entry")
                total += info.st_size
            require(not expected and total <= LIMIT + 65536, "incomplete or oversized archive")
            identity = absolute_file(str(Path(args[3]).absolute()), private=True)
            material = work / "material"
            run([cfg["sparc"], "unpack", str(archive), str(material), str(identity)], work)
            require({p.name for p in material.iterdir()} == {"fixture.sql", "recovery.json"}, "unexpected recovery files")
            meta = read_json(material / "recovery.json", ("version", "source_ref", "sha256"))
            require(type(meta["version"]) is int and meta["version"] == 1
                    and type(meta["source_ref"]) is str and re.fullmatch(r"[a-z]{20}", meta["source_ref"]),
                    "invalid recovery contract")
            require(meta["source_ref"] != cfg["project_ref"], "source and destination must differ")
            sql = read_file(material / "fixture.sql", LIMIT, private=True)
            require(meta["sha256"] == hashlib.sha256(sql).hexdigest(), "fixture digest mismatch")
            check(cfg, work)
            # pg_dump output can contain psql meta-commands: this is TRUSTED code, not a sandbox.
            restore = ("BEGIN;\n" + guard()).encode() + sql + ("\n" + guard(True) + assertions()
                       + PROBE + assertions() + "COMMIT; SELECT 'sparc-restore-ok';").encode()
            output = psql(cfg, work, restore)
            require(output.rstrip().endswith(b"sparc-restore-ok"), "restore result incomplete; quarantine target")
        print("fixture " + mode + " completed; NOT full database or Auth recovery")


if __name__ == "__main__":
    try:
        main(sys.argv[1:])
    except RehearsalError as error:
        print("rehearsal: " + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, UnicodeError, TypeError, KeyError):
        print("rehearsal: invalid input or local operation failed; stop", file=sys.stderr)
        sys.exit(1)
