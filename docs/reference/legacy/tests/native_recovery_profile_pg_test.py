"""Opt-in PG17 native recovery profile rehearsal; fixed synthetic fixture only.

It composes existing read-only observers with native clients and the existing SPARC
archive CLI.  It is intentionally not a reusable restore executor.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
sys.path.insert(0, str(ROOT / "tests"))
import application_dependencies as dependencies
import application_inventory as inventory
import destination_permissions as permissions
import native_schema_selection as selection
from destination_permissions_pg_test import cleanup_owned_cluster, stop_owned_cluster

SCHEMAS = ["profile core", "profile data 雪"]
OWNER = "profile_owner"
READER = "profile_reader"
STANDARD_ROLES = (
    "pg_checkpoint", "pg_create_subscription", "pg_database_owner",
    "pg_execute_server_program", "pg_maintain", "pg_monitor", "pg_read_all_data",
    "pg_read_all_settings", "pg_read_all_stats", "pg_read_server_files",
    "pg_signal_backend", "pg_stat_scan_tables", "pg_use_reserved_connections",
    "pg_write_all_data", "pg_write_server_files",
)


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def receipt(dump, metadata):
    return {"version": 1, "dump_sha256": digest(dump), "metadata_sha256": digest(metadata)}


def bounded_equal(case, actual, expected, category):
    case.assertTrue(actual == expected, category)


def bounded_command_success(case, result, category):
    case.assertTrue(result.returncode == 0, category)


def cleanup_registered_cluster(binary, root, env, owner, cleanup=cleanup_owned_cluster):
    """Clean only a process registered immediately after Popen; otherwise quarantine."""
    server = owner["server"]
    if server is None:
        owner["confirmed_stopped"] = True
        return True
    if cleanup(binary, root, server, env):
        owner["server"] = None
        owner["confirmed_stopped"] = True
        return True
    return False


class BoundedFailureDiagnosticsTest(unittest.TestCase):
    def test_failures_do_not_disclose_captured_values(self):
        canary = "PRIVATE-CANARY-VALUE"
        for category in ("metadata mismatch", "state mismatch"):
            with self.subTest(category=category), self.assertRaises(AssertionError) as raised:
                bounded_equal(self, {"captured": canary}, {"captured": "expected"}, category)
            self.assertNotIn(canary, str(raised.exception))
        with self.assertRaises(AssertionError) as raised:
            bounded_command_success(self, subprocess.CompletedProcess([], 1, b"", canary.encode()),
                                    "native client failed")
        self.assertNotIn(canary, str(raised.exception))


class RegisteredStartupOwnershipTest(unittest.TestCase):
    def test_injected_readiness_failure_quarantines_when_registered_cleanup_fails(self):
        root = Path(tempfile.mkdtemp(prefix="snrp-lifecycle-", dir="/tmp"))
        sentinel = root / "sentinel"; sentinel.write_text("retain")
        server = object(); owner = {"server": server, "confirmed_stopped": False}; calls = []
        try:
            def failed_cleanup(binary, cleanup_root, cleanup_server, env):
                calls.append((binary, cleanup_root, cleanup_server, env))
                return False
            self.assertFalse(cleanup_registered_cluster("pg-bin", root, {"PGHOST": "private"}, owner, failed_cleanup))
            self.assertEqual(calls, [("pg-bin", root, server, {"PGHOST": "private"})])
            self.assertIs(owner["server"], server)
            self.assertFalse(owner["confirmed_stopped"])
            self.assertTrue(sentinel.exists(), "failed registered cleanup must quarantine its root")
        finally:
            shutil.rmtree(root)


@unittest.skipUnless(os.environ.get("SPARC_TEST_PG17_BIN"), "opt-in local PostgreSQL 17 test")
class NativeRecoveryProfilePostgresTest(unittest.TestCase):
    def assertEqual(self, actual, expected, msg=None):
        bounded_equal(self, actual, expected, msg or "bounded equality mismatch")

    def test_fixed_profile_recovers_after_source_shutdown_and_refuses_faults(self):
        binary = Path(os.environ["SPARC_TEST_PG17_BIN"]).resolve()
        self.assertEqual(binary, Path("/opt/homebrew/opt/postgresql@17/bin").resolve())
        sparc = ROOT / "target/debug/sparc"
        self.assertTrue(sparc.is_file(), "build the existing SPARC CLI before this opt-in test")
        base = Path(tempfile.mkdtemp(prefix="snrp-", dir="/tmp")); base.chmod(0o700)
        source_root, target_root = base / "source", base / "target"
        source_owner = {"server": None, "confirmed_stopped": False}
        target_owner = {"server": None, "confirmed_stopped": False}
        source_stopped = False
        source_calls = target_calls = 0
        capture_dump_calls = 0
        restore_calls = 0
        primitive_attempted = set()
        normal_attempted = set()
        logs = []

        def cluster(root, port):
            root.mkdir(mode=0o700)
            socket = root / "s"; socket.mkdir(mode=0o700)
            env = {"PATH": "/usr/bin:/bin", "HOME": str(root), "LC_ALL": "C", "TZ": "UTC",
                   "PGHOST": str(socket), "PGPORT": str(port), "PGUSER": "bootstrap",
                   "PGDATABASE": "postgres", "PGCONNECT_TIMEOUT": "2", "PGOPTIONS": "-c timezone=UTC"}
            return socket, env

        source_socket, source_env = cluster(source_root, 6571)
        target_socket, target_env = cluster(target_root, 6572)

        def start(root, socket, port, env, owner):
            run_raw(root, env, "initdb", "-D", str(root / "data"), "-U", "bootstrap", "-A", "trust",
                    "--encoding=UTF8", "--no-locale")
            log = root / "server.log"; logs.append(log)
            handle = log.open("wb")
            try:
                server = subprocess.Popen([str(binary / "postgres"), "-D", str(root / "data"), "-k", str(socket),
                                           "-p", str(port), "-c", "listen_addresses="], cwd=root, env=env,
                                          stdin=subprocess.DEVNULL, stdout=handle, stderr=handle)
                owner["server"] = server  # Register before the first readiness assertion can fail.
                for _ in range(50):
                    result = subprocess.run([str(binary / "pg_isready"), "-q"], cwd=root, env=env,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5)
                    if result.returncode == 0:
                        return server
                    self.assertIsNone(server.poll(), "owned postgres exited before readiness")
                    time.sleep(.1)
            except BaseException:
                if not cleanup_registered_cluster(binary, root, env, owner):
                    self.fail("owned startup cleanup unconfirmed; private root quarantined: %s" % root)
                raise
            finally:
                handle.close()
            if not cleanup_registered_cluster(binary, root, env, owner):
                self.fail("owned readiness cleanup unconfirmed; private root quarantined: %s" % root)
            self.fail("private socket-only PG17 readiness timeout")

        def run_raw(root, env, tool, *args, sql=None, user=None, database=None, ok=True):
            command_env = dict(env)
            if user: command_env["PGUSER"] = user
            if database: command_env["PGDATABASE"] = database
            result = subprocess.run([str(binary / tool), *args], input=sql.encode() if sql is not None else None,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=root, env=command_env,
                                    timeout=45)
            self.assertLessEqual(len(result.stdout), 200000); self.assertLessEqual(len(result.stderr), 200000)
            if ok is True: bounded_command_success(self, result, "%s failed" % Path(tool).name)
            if ok is False: self.assertNotEqual(result.returncode, 0, "%s unexpectedly succeeded" % tool)
            return result

        def source_run(tool, *args, **kwargs):
            nonlocal source_calls
            self.assertFalse(source_stopped, "source call after confirmed shutdown")
            source_calls += 1
            return run_raw(source_root, source_env, tool, *args, **kwargs)

        def target_run(tool, *args, **kwargs):
            nonlocal target_calls
            target_calls += 1
            return run_raw(target_root, target_env, tool, *args, **kwargs)

        def source_query(sql, **kwargs):
            return source_run("psql", "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-f", "-", sql=sql, **kwargs).stdout.decode()

        def target_query(sql, **kwargs):
            return target_run("psql", "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-f", "-", sql=sql, **kwargs).stdout.decode()

        def source_observer(database, query):
            return source_query(query, database=database).encode()

        def target_observer(database, query):
            return target_query(query, database=database).encode()

        def expected_target_contract(database):
            # Independently specified stock-PG17/bootstrap contract, never read from target.
            def role(name, login=False, superuser=False, createrole=False, createdb=False,
                     replication=False, bypassrls=False):
                return {"name": name, "superuser": superuser, "inherit": True, "createrole": createrole,
                        "createdb": createdb, "login": login, "replication": replication,
                        "bypassrls": bypassrls, "settings_present": False}
            def acl(grantor, kind, privilege, name=None):
                grantee = {"kind": kind}
                if name is not None: grantee["name"] = name
                return {"grantor": grantor, "grantee": grantee, "privilege": privilege, "grant_option": False}
            version = int(target_run("postgres", "--version").stdout.split(b"17.")[1].split()[0])
            return {"version": 1, "server_version_num": 170000 + version,
                    "selection": {"schemas": sorted(SCHEMAS), "creating_roles": [OWNER]},
                    "database": {"name": database, "owner": "bootstrap", "settings_present": False,
                                 "acl": [acl("bootstrap", "public", "CONNECT"),
                                         acl("bootstrap", "role", "CREATE", "bootstrap"),
                                         acl("bootstrap", "role", "CONNECT", "bootstrap"),
                                         acl("bootstrap", "public", "TEMPORARY"),
                                         acl("bootstrap", "role", "TEMPORARY", "bootstrap"),
                                         acl("bootstrap", "role", "CREATE", OWNER)]},
                    "schemas": [{"name": schema, "present": False, "owner": None, "acl": []}
                                for schema in sorted(SCHEMAS)],
                    "roles": [role(name) for name in STANDARD_ROLES] + [
                        role("bootstrap", True, True, True, True, True, True), role(OWNER, True),
                        role(READER, True)],
                    "memberships": [
                        {"role": "pg_read_all_settings", "member": "pg_monitor", "grantor": "bootstrap", "admin": False, "inherit": True, "set": True},
                        {"role": "pg_read_all_stats", "member": "pg_monitor", "grantor": "bootstrap", "admin": False, "inherit": True, "set": True},
                        {"role": "pg_stat_scan_tables", "member": "pg_monitor", "grantor": "bootstrap", "admin": False, "inherit": True, "set": True}],
                    "default_acls": []}

        def source_profile_admission(database, schemas):
            """Fixed-profile capture gate; this is not a general source policy."""
            with patch.object(inventory.h, "versions"), patch.object(
                    inventory.h, "psql", side_effect=lambda _c, _w, sql: source_observer(database, sql)), patch.object(
                    dependencies.h, "versions"), patch.object(
                    dependencies.h, "psql", side_effect=lambda _c, _w, sql: source_observer(database, sql)):
                observed_inventory = inventory.inspect({}, source_root, schemas)
                observed_dependencies = dependencies.observe({}, source_root, schemas)
            self.assertFalse(observed_inventory["execution_supported"])
            self.assertFalse(observed_dependencies["dependency_analysis_complete"])
            details = observed_inventory["inventory"]
            self.assertEqual([item["name"] for item in details["schemas"]], sorted(SCHEMAS),
                             "source admission refused schema shape")
            self.assertTrue(all(item["owner"] == OWNER for item in details["schemas"]),
                            "source admission refused schema ownership")
            expected_relations = {
                ("profile core", "parents", "r"), ("profile core", "parents_id_seq", "S"),
                ("profile core", "parents_pkey", "i"), ("profile core", "parents_label_key", "i"),
                ("profile data 雪", "items", "r"), ("profile data 雪", "items_id_seq", "S"),
                ("profile data 雪", "items_pkey", "i"), ("profile data 雪", "items_state_idx", "i"),
            }
            self.assertEqual({(item["schema"], item["name"], item["kind"])
                              for item in details["relations"]}, expected_relations,
                             "source admission refused fixed relation kinds")
            self.assertTrue(all(item["owner"] == OWNER and item["persistence"] == "p"
                                and not item["partitioned"] and not item["rls"] and not item["force_rls"]
                                for item in details["relations"]), "source admission refused relation shape")
            self.assertEqual({(item["schema"], item["name"], item["kind"], item["owner"])
                              for item in details["types"]},
                             {("profile core", "item_state", "e", OWNER)},
                             "source admission refused fixed type shape")
            self.assertEqual(details["routines"], [], "source admission refused user routine")
            self.assertEqual(details["triggers"], [], "source admission refused user trigger")
            self.assertEqual(details["policies"], [], "source admission refused RLS policy")
            self.assertEqual(details["extensions"], [], "source admission refused extension member")
            rejected = {"external", "protected-external", "extension-requirement", "unresolved"}
            self.assertFalse(any(item["code"] in rejected for item in observed_dependencies["findings"]),
                             "source admission refused external/protected/extension/unresolved dependency")
            edges = observed_dependencies["response"]["edges"]
            self.assertFalse(any(edge["unresolved"] for edge in edges),
                             "source admission refused unresolved ordinary fixture edge")
            # PG17 omits pg_depend edges for pinned built-ins; empty direct edges are fixture-reviewed,
            # not a claim of dependency closure. The required built-ins are checked independently below.
            system_targets = {(edge["target_kind"], edge["target_schema"], edge["target_name"])
                              for edge in edges if edge["target_schema"] == "pg_catalog"}
            self.assertEqual(system_targets, set(),
                             "source admission refused unreviewed system prerequisite edge")
            reviewed_builtin_types = [
                {"schema": "pg_catalog", "name": "int4", "kind": "b"},
                {"schema": "pg_catalog", "name": "text", "kind": "b"},
            ]
            observed_builtin_types = json.loads(source_query('''SELECT COALESCE(jsonb_agg(jsonb_build_object(
                'schema',n.nspname,'name',t.typname,'kind',t.typtype::text) ORDER BY t.typname),'[]'::jsonb)::text
                FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
                WHERE n.nspname='pg_catalog' AND t.typname IN ('int4','text');''', database=database))
            self.assertEqual(observed_builtin_types, reviewed_builtin_types,
                             "source admission refused reviewed built-in type identity")
            expected_column_types = {
                ("profile core", "parents", "id", "pg_catalog", "int4"),
                ("profile core", "parents", "label", "pg_catalog", "text"),
                ("profile data 雪", "items", "id", "pg_catalog", "int4"),
                ("profile data 雪", "items", "parent_id", "pg_catalog", "int4"),
                ("profile data 雪", "items", "state", "profile core", "item_state"),
                ("profile data 雪", "items", "note", "pg_catalog", "text"),
            }
            expected_user_tables = {(schema, relation) for schema, relation, kind in expected_relations if kind == "r"}
            self.assertEqual({(item["schema"], item["relation"], item["name"], item["type_schema"], item["type_name"])
                              for item in details["columns"] if (item["schema"], item["relation"]) in expected_user_tables},
                             expected_column_types, "source admission refused fixed column type identities")
            return observed_inventory, observed_dependencies

        def capture_profile(database, schemas, output):
            nonlocal capture_dump_calls
            source_profile_admission(database, schemas)
            capture_dump_calls += 1
            return source_run("pg_dump", "-Fc", *selection.pg_dump_schema_args(schemas), "-f", str(output),
                              database=database)

        def profile_state(query, database):
            return json.loads(query('''WITH rels AS (
              SELECT n.nspname schema,c.relname name,c.relkind kind,pg_get_userbyid(c.relowner) owner
              FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
              WHERE n.nspname IN ('profile core','profile data 雪') AND c.relkind IN ('r','S','i')
            ), schemas AS (
              SELECT n.nspname name,pg_get_userbyid(n.nspowner) owner
              FROM pg_namespace n WHERE n.nspname IN ('profile core','profile data 雪')
            ), columns AS (
              SELECT n.nspname schema,c.relname table_name,a.attnum position,a.attname name,
                jsonb_build_object('schema',tn.nspname,'name',t.typname) type,
                NOT a.attnotnull nullable,a.attidentity::text identity,
                pg_get_expr(d.adbin,d.adrelid,false) default_expression
              FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
              JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_type t ON t.oid=a.atttypid
              JOIN pg_namespace tn ON tn.oid=t.typnamespace
              LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
              WHERE n.nspname IN ('profile core','profile data 雪') AND c.relkind='r'
                AND a.attnum>0 AND NOT a.attisdropped
            ), enums AS (
              SELECT n.nspname schema,t.typname name,pg_get_userbyid(t.typowner) owner,
                jsonb_agg(e.enumlabel ORDER BY e.enumsortorder) labels
              FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
              JOIN pg_enum e ON e.enumtypid=t.oid
              WHERE n.nspname IN ('profile core','profile data 雪')
              GROUP BY n.nspname,t.typname,t.typowner
            ), constraints AS (
              SELECT n.nspname schema,r.relname table_name,k.conname name,k.contype::text type,
                pg_get_constraintdef(k.oid,false) definition,k.condeferrable deferrable,
                k.condeferred initially_deferred,k.convalidated validated
              FROM pg_constraint k JOIN pg_class r ON r.oid=k.conrelid
              JOIN pg_namespace n ON n.oid=r.relnamespace
              WHERE n.nspname IN ('profile core','profile data 雪')
            ), indexes AS (
              SELECT n.nspname schema,t.relname table_name,i.relname name,
                pg_get_userbyid(i.relowner) owner,pg_get_indexdef(i.oid) definition,
                x.indisunique unique_index,x.indisprimary primary_index,x.indisvalid valid,
                x.indisready ready,x.indislive live,x.indisreplident replica_identity
              FROM pg_index x JOIN pg_class i ON i.oid=x.indexrelid
              JOIN pg_class t ON t.oid=x.indrelid JOIN pg_namespace n ON n.oid=t.relnamespace
              WHERE n.nspname IN ('profile core','profile data 雪')
            ), sequence_definitions AS (
              SELECT n.nspname schema,c.relname name,pg_get_userbyid(c.relowner) owner,
                jsonb_build_object('schema',tn.nspname,'name',typ.typname) type,
                s.seqstart start_value,s.seqmin min_value,s.seqmax max_value,
                s.seqincrement increment_by,s.seqcache cache_size,s.seqcycle cycle,
                jsonb_build_object('schema',onsp.nspname,'table',owned.relname,'column',a.attname) owned_by
              FROM pg_sequence s JOIN pg_class c ON c.oid=s.seqrelid
              JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_type typ ON typ.oid=s.seqtypid
              JOIN pg_namespace tn ON tn.oid=typ.typnamespace
              JOIN pg_depend dep ON dep.classid='pg_class'::regclass AND dep.objid=c.oid AND dep.deptype='i'
              JOIN pg_class owned ON owned.oid=dep.refobjid JOIN pg_namespace onsp ON onsp.oid=owned.relnamespace
              JOIN pg_attribute a ON a.attrelid=dep.refobjid AND a.attnum=dep.refobjsubid
              WHERE n.nspname IN ('profile core','profile data 雪')
            ), data AS (
              SELECT jsonb_build_object('parents',(SELECT jsonb_agg(jsonb_build_object('id',id,'label',label) ORDER BY id) FROM "profile core".parents),
                'items',(SELECT jsonb_agg(jsonb_build_object('id',id,'parent_id',parent_id,'state',state::text,'note',note) ORDER BY id) FROM "profile data 雪".items)) value
            ), acl_objects AS (
              SELECT 'schema'::text object_kind,n.nspname schema,n.nspname name,NULL::text column_name,
                pg_get_userbyid(n.nspowner) owner,n.nspacl acl
              FROM pg_namespace n WHERE n.nspname IN ('profile core','profile data 雪')
              UNION ALL
              SELECT CASE c.relkind WHEN 'r' THEN 'table' ELSE 'sequence' END,n.nspname,c.relname,NULL,
                pg_get_userbyid(c.relowner),c.relacl
              FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
              WHERE n.nspname IN ('profile core','profile data 雪') AND c.relkind IN ('r','S')
              UNION ALL
              SELECT 'column',n.nspname,c.relname,a.attname,pg_get_userbyid(c.relowner),a.attacl
              FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
              JOIN pg_namespace n ON n.oid=c.relnamespace
              WHERE n.nspname IN ('profile core','profile data 雪') AND c.relkind='r'
                AND a.attnum>0 AND NOT a.attisdropped
            ), default_objects AS (
              SELECT pg_get_userbyid(d.defaclrole) owner,n.nspname schema,d.defaclobjtype::text object_type,
                d.defaclacl acl
              FROM pg_default_acl d JOIN pg_namespace n ON n.oid=d.defaclnamespace
              WHERE n.nspname IN ('profile core','profile data 雪')
            ) SELECT jsonb_build_object(
              'schemas',COALESCE((SELECT jsonb_agg(to_jsonb(schemas) ORDER BY name) FROM schemas),'[]'::jsonb),
              'relations',COALESCE((SELECT jsonb_agg(to_jsonb(rels) ORDER BY schema,name,kind) FROM rels),'[]'::jsonb),
              'columns',COALESCE((SELECT jsonb_agg(to_jsonb(columns) ORDER BY schema,table_name,position) FROM columns),'[]'::jsonb),
              'enums',COALESCE((SELECT jsonb_agg(to_jsonb(enums) ORDER BY schema,name) FROM enums),'[]'::jsonb),
              'data',(SELECT value FROM data),
              'sequences',COALESCE((SELECT jsonb_agg(to_jsonb(sequence_state) ORDER BY schema,name) FROM (
                SELECT d.*,v.last_value,v.is_called FROM sequence_definitions d JOIN (
                  SELECT 'profile core'::text schema,'parents_id_seq'::text name,last_value,is_called FROM "profile core".parents_id_seq
                  UNION ALL SELECT 'profile data 雪','items_id_seq',last_value,is_called FROM "profile data 雪".items_id_seq
                ) v USING (schema,name)) sequence_state),'[]'::jsonb),
              'constraints',COALESCE((SELECT jsonb_agg(to_jsonb(constraints) ORDER BY schema,table_name,name) FROM constraints),'[]'::jsonb),
              'indexes',COALESCE((SELECT jsonb_agg(to_jsonb(indexes) ORDER BY schema,table_name,name) FROM indexes),'[]'::jsonb),
              'acls',(SELECT jsonb_agg(jsonb_build_object('object_kind',o.object_kind,'schema',o.schema,
                'name',o.name,'column',o.column_name,'owner',o.owner,'acl',CASE WHEN o.acl IS NULL THEN 'null'::jsonb ELSE
                  COALESCE((SELECT jsonb_agg(jsonb_build_object('grantor',pg_get_userbyid(x.grantor),
                    'grantee',CASE WHEN x.grantee=0 THEN jsonb_build_object('kind','public')
                      ELSE jsonb_build_object('kind','role','name',pg_get_userbyid(x.grantee)) END,
                    'privilege',x.privilege_type,'grant_option',x.is_grantable)
                    ORDER BY pg_get_userbyid(x.grantor),CASE WHEN x.grantee=0 THEN 0 ELSE 1 END,
                      pg_get_userbyid(x.grantee),x.privilege_type,x.is_grantable)
                    FROM aclexplode(o.acl) x),'[]'::jsonb) END)
                ORDER BY o.object_kind,o.schema,o.name,o.column_name NULLS FIRST) FROM acl_objects o),
              'defaults',COALESCE((SELECT jsonb_agg(jsonb_build_object('owner',o.owner,'schema',o.schema,
                'object_type',o.object_type,'acl',CASE WHEN o.acl IS NULL THEN 'null'::jsonb ELSE
                  COALESCE((SELECT jsonb_agg(jsonb_build_object('grantor',pg_get_userbyid(x.grantor),
                    'grantee',CASE WHEN x.grantee=0 THEN jsonb_build_object('kind','public')
                      ELSE jsonb_build_object('kind','role','name',pg_get_userbyid(x.grantee)) END,
                    'privilege',x.privilege_type,'grant_option',x.is_grantable)
                    ORDER BY pg_get_userbyid(x.grantor),CASE WHEN x.grantee=0 THEN 0 ELSE 1 END,
                      pg_get_userbyid(x.grantee),x.privilege_type,x.is_grantable)
                    FROM aclexplode(o.acl) x),'[]'::jsonb) END)
                ORDER BY o.owner,o.schema,o.object_type) FROM default_objects o),'[]'::jsonb)
            )::text;''', database))

        def session_identity(user, database):
            return json.loads(target_query('''SELECT jsonb_build_object(
              'session_user',session_user,'current_user',current_user,'role',jsonb_build_object(
                'name',r.rolname,'login',r.rolcanlogin,'superuser',r.rolsuper,'inherit',r.rolinherit,
                'createrole',r.rolcreaterole,'createdb',r.rolcreatedb,'replication',r.rolreplication,
                'bypassrls',r.rolbypassrls))::text FROM pg_roles r WHERE r.rolname=current_user;''',
                                           user=user, database=database))

        def public_namespace_state(database):
            return json.loads(target_query('''SELECT jsonb_build_object('owner',pg_get_userbyid(n.nspowner),
              'acl',CASE WHEN n.nspacl IS NULL THEN 'null'::jsonb ELSE COALESCE((SELECT jsonb_agg(
                jsonb_build_object('grantor',pg_get_userbyid(x.grantor),'grantee',CASE WHEN x.grantee=0
                  THEN jsonb_build_object('kind','public') ELSE jsonb_build_object('kind','role','name',pg_get_userbyid(x.grantee)) END,
                  'privilege',x.privilege_type,'grant_option',x.is_grantable)
                ORDER BY CASE WHEN x.grantee=0 THEN 0 ELSE 1 END,pg_get_userbyid(x.grantee),x.privilege_type)
                FROM aclexplode(n.nspacl) x),'[]'::jsonb) END)::text
              FROM pg_namespace n WHERE n.nspname='public';''', database=database))

        def fixture_fresh_destination(database):
            """Fixed-fixture check for concrete forbidden objects; not a general catalog policy."""
            return json.loads(target_query('''WITH objects AS (
              SELECT 'namespace' category,n.nspname identity FROM pg_namespace n
                WHERE n.nspname NOT IN ('pg_catalog','information_schema','pg_toast','public')
                  AND n.nspname !~ '^pg_(toast_)?temp_[0-9]+$'
              UNION ALL SELECT CASE WHEN c.relkind='c' THEN 'composite-relation' ELSE 'relation' END,
                  n.nspname||'.'||c.relname
                FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
                WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','S','f','c')
              UNION ALL SELECT 'routine',n.nspname||'.'||p.proname
                FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'
              UNION ALL SELECT 'standalone-type',n.nspname||'.'||t.typname
                FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
                WHERE n.nspname='public' AND t.typelem=0
                  AND t.typtype IN ('b','c','d','e','r','m')
              UNION ALL SELECT 'collation',n.nspname||'.'||c.collname
                FROM pg_collation c JOIN pg_namespace n ON n.oid=c.collnamespace
                WHERE n.nspname='public'
            ) SELECT COALESCE(jsonb_agg(jsonb_build_object('category',category,'identity',identity)
              ORDER BY category,identity),'[]'::jsonb)::text FROM objects;''', database=database))

        def no_event_trigger(database):
            return target_query("SELECT count(*) FROM pg_event_trigger WHERE evtname NOT LIKE 'pg_%';", database=database).strip() == "0"

        def restore_primitive(database, dump):
            """Private one-attempt, non-superuser transactional restore primitive."""
            nonlocal restore_calls
            self.assertNotIn(database, primitive_attempted, "one-attempt restore latch refused repeat")
            primitive_attempted.add(database); restore_calls += 1
            return target_run("pg_restore", "--single-transaction", "--exit-on-error", "--dbname=" + database,
                              str(dump), user=OWNER, ok=None)

        def normal_restore(database, dump, profile_metadata, trusted_receipt):
            """Normal admission path; no caller-controlled bypass or skip-check option."""
            self.assertNotIn(database, normal_attempted, "normal restore latch refused repeat")
            self.assertEqual(frozenset(receipt(dump, profile_metadata).items()), trusted_receipt,
                             "normal admission refused untrusted dump or metadata")
            expected_contract = expected_target_contract(database)
            self.assertNotIn("PUBLIC", {role["name"] for role in expected_contract["roles"]})
            self.assertTrue(any(acl["grantee"] == {"kind": "public"}
                                for acl in expected_contract["database"]["acl"]))
            with patch.object(permissions.h, "versions"), patch.object(
                    permissions.h, "psql",
                    side_effect=lambda _c, _w, sql: target_observer(database, sql)):
                target_observed = permissions.observe({}, target_root, SCHEMAS, [OWNER])
            comparison = permissions.compare(expected_contract, target_observed, SCHEMAS, [OWNER])
            mismatch_categories = set(comparison["mismatch_categories"])
            for category in ("database", "schemas", "roles", "memberships", "default-acls"):
                if category in mismatch_categories:
                    self.fail("normal admission refused %s" % category)
            self.assertFalse(mismatch_categories, "normal admission refused unknown prerequisite category")
            self.assertTrue(no_event_trigger(database), "normal admission refused user event trigger")
            self.assertEqual(fixture_fresh_destination(database), [],
                             "normal admission refused unexpected user object or namespace")
            normal_attempted.add(database)
            result = restore_primitive(database, dump)
            bounded_command_success(self, result, "pg_restore failed")
            return result

        def fixture_fault_restore(dump):
            """Supervisor-approved direct fault injection for this owned synthetic target only."""
            return restore_primitive("profile_fault", dump)

        try:
            self.assertTrue(b"PostgreSQL) 17." in run_raw(source_root, source_env, "postgres", "--version").stdout,
                            "source native version mismatch")
            self.assertTrue(b"PostgreSQL) 17." in run_raw(target_root, target_env, "pg_dump", "--version").stdout,
                            "target native version mismatch")
            start(source_root, source_socket, 6571, source_env, source_owner)
            start(target_root, target_socket, 6572, target_env, target_owner)
            source_query("CREATE ROLE %s LOGIN; CREATE ROLE %s LOGIN; CREATE DATABASE profile_source OWNER bootstrap; GRANT CREATE ON DATABASE profile_source TO %s;" % (OWNER, READER, OWNER))
            target_query("CREATE ROLE %s LOGIN; CREATE ROLE %s LOGIN; CREATE DATABASE profile_target OWNER bootstrap; CREATE DATABASE profile_fault OWNER bootstrap; CREATE DATABASE profile_default OWNER bootstrap; CREATE DATABASE profile_collision OWNER bootstrap; CREATE DATABASE profile_unexpected OWNER bootstrap; CREATE DATABASE profile_composite OWNER bootstrap; CREATE DATABASE profile_collation OWNER bootstrap; GRANT CREATE ON DATABASE profile_target, profile_fault, profile_default, profile_collision, profile_unexpected, profile_composite, profile_collation TO %s;" % (OWNER, READER, OWNER))
            expected_login = lambda name: {"session_user": name, "current_user": name, "role": {
                "name": name, "login": True, "superuser": False, "inherit": True,
                "createrole": False, "createdb": False, "replication": False, "bypassrls": False}}
            self.assertEqual(session_identity(OWNER, "profile_target"), expected_login(OWNER))
            self.assertEqual(session_identity(READER, "profile_target"), expected_login(READER))
            stock_public = {"owner": "pg_database_owner", "acl": [
                {"grantor": "pg_database_owner", "grantee": {"kind": "public"},
                 "privilege": "USAGE", "grant_option": False},
                {"grantor": "pg_database_owner", "grantee": {"kind": "role", "name": "pg_database_owner"},
                 "privilege": "CREATE", "grant_option": False},
                {"grantor": "pg_database_owner", "grantee": {"kind": "role", "name": "pg_database_owner"},
                 "privilege": "USAGE", "grant_option": False},
            ]}
            public_before_restore = public_namespace_state("profile_target")
            self.assertEqual(public_before_restore, stock_public,
                             "fresh PG17 public namespace differs from explicit stock owner/grants")
            source_query('''SET ROLE profile_owner;
              CREATE SCHEMA "profile core"; CREATE SCHEMA "profile data 雪";
              CREATE TYPE "profile core".item_state AS ENUM ('new','done');
              CREATE TABLE "profile core".parents (id integer GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, label text NOT NULL UNIQUE, CHECK (length(label)>0));
              CREATE TABLE "profile data 雪".items (id integer GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, parent_id integer NOT NULL REFERENCES "profile core".parents(id), state "profile core".item_state NOT NULL, note text);
              CREATE INDEX items_state_idx ON "profile data 雪".items(state);
              GRANT USAGE ON SCHEMA "profile core", "profile data 雪" TO profile_reader;
              GRANT SELECT ON "profile core".parents TO profile_reader;
              GRANT SELECT(id,state,note) ON "profile data 雪".items TO profile_reader;
              GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA "profile core", "profile data 雪" TO profile_reader;
              ALTER DEFAULT PRIVILEGES IN SCHEMA "profile data 雪" GRANT SELECT ON TABLES TO profile_reader;
              INSERT INTO "profile core".parents(label) VALUES ('α'),('雪');
              INSERT INTO "profile data 雪".items(parent_id,state,note) VALUES (1,'new','text'),(2,'done',NULL);''', database="profile_source")

            expected_state = profile_state(lambda sql, database: source_query(sql, database=database), "profile_source")
            self.assertEqual(expected_state["schemas"], [
                {"name": "profile core", "owner": OWNER},
                {"name": "profile data 雪", "owner": OWNER},
            ])
            self.assertEqual(expected_state["relations"], [
                {"schema": "profile core", "name": "parents", "kind": "r", "owner": OWNER},
                {"schema": "profile core", "name": "parents_id_seq", "kind": "S", "owner": OWNER},
                {"schema": "profile core", "name": "parents_label_key", "kind": "i", "owner": OWNER},
                {"schema": "profile core", "name": "parents_pkey", "kind": "i", "owner": OWNER},
                {"schema": "profile data 雪", "name": "items", "kind": "r", "owner": OWNER},
                {"schema": "profile data 雪", "name": "items_id_seq", "kind": "S", "owner": OWNER},
                {"schema": "profile data 雪", "name": "items_pkey", "kind": "i", "owner": OWNER},
                {"schema": "profile data 雪", "name": "items_state_idx", "kind": "i", "owner": OWNER},
            ])
            self.assertEqual(expected_state["columns"], [
                {"schema": "profile core", "table_name": "parents", "position": 1, "name": "id",
                 "type": {"schema": "pg_catalog", "name": "int4"}, "nullable": False,
                 "identity": "d", "default_expression": None},
                {"schema": "profile core", "table_name": "parents", "position": 2, "name": "label",
                 "type": {"schema": "pg_catalog", "name": "text"}, "nullable": False,
                 "identity": "", "default_expression": None},
                {"schema": "profile data 雪", "table_name": "items", "position": 1, "name": "id",
                 "type": {"schema": "pg_catalog", "name": "int4"}, "nullable": False,
                 "identity": "d", "default_expression": None},
                {"schema": "profile data 雪", "table_name": "items", "position": 2, "name": "parent_id",
                 "type": {"schema": "pg_catalog", "name": "int4"}, "nullable": False,
                 "identity": "", "default_expression": None},
                {"schema": "profile data 雪", "table_name": "items", "position": 3, "name": "state",
                 "type": {"schema": "profile core", "name": "item_state"}, "nullable": False,
                 "identity": "", "default_expression": None},
                {"schema": "profile data 雪", "table_name": "items", "position": 4, "name": "note",
                 "type": {"schema": "pg_catalog", "name": "text"}, "nullable": True,
                 "identity": "", "default_expression": None},
            ])
            self.assertEqual(expected_state["enums"], [
                {"schema": "profile core", "name": "item_state", "owner": OWNER,
                 "labels": ["new", "done"]},
            ])
            self.assertEqual(expected_state["constraints"], [
                {"schema": "profile core", "table_name": "parents", "name": "parents_label_check",
                 "type": "c", "definition": "CHECK ((length(label) > 0))", "deferrable": False,
                 "initially_deferred": False, "validated": True},
                {"schema": "profile core", "table_name": "parents", "name": "parents_label_key",
                 "type": "u", "definition": "UNIQUE (label)", "deferrable": False,
                 "initially_deferred": False, "validated": True},
                {"schema": "profile core", "table_name": "parents", "name": "parents_pkey",
                 "type": "p", "definition": "PRIMARY KEY (id)", "deferrable": False,
                 "initially_deferred": False, "validated": True},
                {"schema": "profile data 雪", "table_name": "items", "name": "items_parent_id_fkey",
                 "type": "f", "definition": 'FOREIGN KEY (parent_id) REFERENCES "profile core".parents(id)',
                 "deferrable": False, "initially_deferred": False, "validated": True},
                {"schema": "profile data 雪", "table_name": "items", "name": "items_pkey",
                 "type": "p", "definition": "PRIMARY KEY (id)", "deferrable": False,
                 "initially_deferred": False, "validated": True},
            ])
            self.assertEqual(expected_state["indexes"], [
                {"schema": "profile core", "table_name": "parents", "name": "parents_label_key", "owner": OWNER,
                 "definition": 'CREATE UNIQUE INDEX parents_label_key ON "profile core".parents USING btree (label)',
                 "unique_index": True, "primary_index": False, "valid": True, "ready": True, "live": True, "replica_identity": False},
                {"schema": "profile core", "table_name": "parents", "name": "parents_pkey", "owner": OWNER,
                 "definition": 'CREATE UNIQUE INDEX parents_pkey ON "profile core".parents USING btree (id)',
                 "unique_index": True, "primary_index": True, "valid": True, "ready": True, "live": True, "replica_identity": False},
                {"schema": "profile data 雪", "table_name": "items", "name": "items_pkey", "owner": OWNER,
                 "definition": 'CREATE UNIQUE INDEX items_pkey ON "profile data 雪".items USING btree (id)',
                 "unique_index": True, "primary_index": True, "valid": True, "ready": True, "live": True, "replica_identity": False},
                {"schema": "profile data 雪", "table_name": "items", "name": "items_state_idx", "owner": OWNER,
                 "definition": 'CREATE INDEX items_state_idx ON "profile data 雪".items USING btree (state)',
                 "unique_index": False, "primary_index": False, "valid": True, "ready": True, "live": True, "replica_identity": False},
            ])
            expected_sequence_definitions = [
                {"schema": "profile core", "name": "parents_id_seq", "owner": OWNER,
                 "type": {"schema": "pg_catalog", "name": "int4"}, "start_value": 1, "min_value": 1,
                 "max_value": 2147483647, "increment_by": 1, "cache_size": 1, "cycle": False,
                 "owned_by": {"schema": "profile core", "table": "parents", "column": "id"}},
                {"schema": "profile data 雪", "name": "items_id_seq", "owner": OWNER,
                 "type": {"schema": "pg_catalog", "name": "int4"}, "start_value": 1, "min_value": 1,
                 "max_value": 2147483647, "increment_by": 1, "cache_size": 1, "cycle": False,
                 "owned_by": {"schema": "profile data 雪", "table": "items", "column": "id"}},
            ]
            self.assertEqual([{key: value for key, value in sequence.items()
                               if key not in {"last_value", "is_called"}}
                              for sequence in expected_state["sequences"]], expected_sequence_definitions)
            self.assertEqual([(sequence["last_value"], sequence["is_called"])
                              for sequence in expected_state["sequences"]], [(2, True), (2, True)])
            expected_parents_acl = {"object_kind": "table", "schema": "profile core", "name": "parents",
                                    "column": None, "owner": OWNER, "acl": [
                {"grantor": OWNER, "grantee": {"kind": "role", "name": OWNER},
                 "privilege": privilege, "grant_option": False}
                for privilege in ["DELETE", "INSERT", "MAINTAIN", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"]
            ] + [{"grantor": OWNER, "grantee": {"kind": "role", "name": READER},
                  "privilege": "SELECT", "grant_option": False}]}
            self.assertEqual([acl for acl in expected_state["acls"]
                              if acl["object_kind"] == "table" and acl["name"] == "parents"],
                             [expected_parents_acl], "source captured parents table ACL mismatch")

            artifact = base / "capture"; artifact.mkdir(mode=0o700)
            dump = artifact / "profile.dump"
            capture_profile("profile_source", SCHEMAS, dump)
            self.assertEqual(capture_dump_calls, 1)
            source_query("CREATE DATABASE profile_external TEMPLATE profile_source; CREATE DATABASE profile_unsupported TEMPLATE profile_source;")
            source_query('''CREATE TABLE public.external_parent(id integer PRIMARY KEY); GRANT REFERENCES ON public.external_parent TO profile_owner;
              SET ROLE profile_owner; ALTER TABLE "profile data 雪".items ADD COLUMN external_id integer REFERENCES public.external_parent(id);''', database="profile_external")
            before_capture = capture_dump_calls
            with self.assertRaisesRegex(AssertionError, "external/protected/extension/unresolved dependency"):
                capture_profile("profile_external", SCHEMAS, artifact / "external.dump")
            self.assertEqual(capture_dump_calls, before_capture)
            source_query('''SET ROLE profile_owner; CREATE FUNCTION "profile core".unsupported() RETURNS integer LANGUAGE sql RETURN 1;''', database="profile_unsupported")
            with self.assertRaisesRegex(AssertionError, "user routine"):
                capture_profile("profile_unsupported", SCHEMAS, artifact / "unsupported.dump")
            self.assertEqual(capture_dump_calls, before_capture)
            with self.assertRaises(inventory.InspectionError):
                capture_profile("profile_source", [*SCHEMAS, "profile missing"], artifact / "missing.dump")
            self.assertEqual(capture_dump_calls, before_capture)
            metadata = artifact / "profile.json"
            trusted_profile = {"version": 1, "schemas": sorted(SCHEMAS), "state": expected_state}
            metadata.write_text(json.dumps(trusted_profile, ensure_ascii=False, separators=(",", ":")))
            metadata.chmod(0o600)
            trusted = frozenset(receipt(dump, metadata).items())
            identity = base / "identity"
            recipient = run_raw(base, source_env, str(sparc), "keygen", str(identity)).stdout.decode().strip()
            archive = base / "archive"
            run_raw(base, source_env, str(sparc), "pack", str(artifact), str(archive), recipient)
            run_raw(base, source_env, str(sparc), "verify", str(archive), str(identity))
            unpacked = base / "unpacked"
            run_raw(base, source_env, str(sparc), "unpack", str(archive), str(unpacked), str(identity))
            unpacked_dump, unpacked_metadata = unpacked / "profile.dump", unpacked / "profile.json"
            self.assertEqual(frozenset(receipt(unpacked_dump, unpacked_metadata).items()), trusted)
            unpacked_profile = json.loads(unpacked_metadata.read_text())
            self.assertEqual(unpacked_profile, trusted_profile,
                             "unpacked profile version/schema/state differs from trusted pre-capture evidence")

            self.assertTrue(stop_owned_cluster(binary, source_root, source_owner["server"], source_env))
            source_owner["server"] = None; source_owner["confirmed_stopped"] = True
            source_stopped = True
            calls_at_shutdown = source_calls
            # Normal admission rejects damaged dump and mismatched receipt before destination access.
            damaged = base / "damaged.dump"; shutil.copyfile(unpacked_dump, damaged); damaged.write_bytes(damaged.read_bytes() + b"x")
            before_target, before_restore = target_calls, restore_calls
            with self.assertRaisesRegex(AssertionError, "untrusted dump or metadata"):
                normal_restore("profile_target", damaged, unpacked_metadata, trusted)
            bad_receipt = frozenset({"version": 1, "dump_sha256": "0" * 64,
                                     "metadata_sha256": "0" * 64}.items())
            with self.assertRaisesRegex(AssertionError, "untrusted dump or metadata"):
                normal_restore("profile_target", unpacked_dump, unpacked_metadata, bad_receipt)
            self.assertEqual(target_calls, before_target); self.assertEqual(restore_calls, before_restore)

            target_query('''CREATE TABLE public.default_sentinel(value text PRIMARY KEY); INSERT INTO public.default_sentinel VALUES ('keep');
              ALTER DEFAULT PRIVILEGES FOR ROLE profile_owner GRANT SELECT ON TABLES TO profile_reader;''', database="profile_default")
            default_restore_calls = restore_calls
            with self.assertRaisesRegex(AssertionError, "default-acls"):
                normal_restore("profile_default", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(restore_calls, default_restore_calls, "default-ACL admission made no restore call")
            self.assertEqual(target_query("SELECT value FROM public.default_sentinel;", database="profile_default").strip(), "keep")

            target_query('''CREATE SCHEMA "profile core" AUTHORIZATION profile_owner; SET ROLE profile_owner;
              CREATE TABLE "profile core".collision_sentinel(value text PRIMARY KEY); INSERT INTO "profile core".collision_sentinel VALUES ('keep');''', database="profile_collision")
            collision_restore_calls = restore_calls
            with self.assertRaisesRegex(AssertionError, "schemas"):
                normal_restore("profile_collision", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(restore_calls, collision_restore_calls, "collision admission made no restore call")
            self.assertEqual(target_query('SELECT value FROM "profile core".collision_sentinel;', database="profile_collision").strip(), "keep")

            target_query("CREATE TABLE public.unexpected_sentinel(value text PRIMARY KEY); INSERT INTO public.unexpected_sentinel VALUES ('keep');",
                         database="profile_unexpected")
            unexpected_restore_calls = restore_calls
            with self.assertRaisesRegex(AssertionError, "unexpected user object or namespace"):
                normal_restore("profile_unexpected", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(restore_calls, unexpected_restore_calls,
                             "unexpected-public-object admission made no restore call")
            self.assertEqual(target_query("SELECT value FROM public.unexpected_sentinel;",
                                          database="profile_unexpected").strip(), "keep")

            target_query("COMMENT ON DATABASE profile_composite IS 'keep'; CREATE TYPE public.extra AS (value integer);",
                         database="profile_composite")
            self.assertEqual(fixture_fresh_destination("profile_composite"), [
                {"category": "composite-relation", "identity": "public.extra"},
                {"category": "standalone-type", "identity": "public.extra"},
            ], "composite fixture categories were not explicitly observed")
            composite_restore_calls = restore_calls
            with self.assertRaisesRegex(AssertionError, "unexpected user object or namespace"):
                normal_restore("profile_composite", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(restore_calls, composite_restore_calls,
                             "composite admission made no restore call")
            self.assertEqual(target_query("SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname='profile_composite';",
                                          database="profile_composite").strip(),
                             "keep", "composite target sentinel changed")

            target_query("COMMENT ON DATABASE profile_collation IS 'keep'; CREATE COLLATION public.extra FROM pg_catalog.\"C\";",
                         database="profile_collation")
            self.assertEqual(fixture_fresh_destination("profile_collation"), [
                {"category": "collation", "identity": "public.extra"},
            ], "collation fixture category was not explicitly observed")
            collation_restore_calls = restore_calls
            with self.assertRaisesRegex(AssertionError, "unexpected user object or namespace"):
                normal_restore("profile_collation", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(restore_calls, collation_restore_calls,
                             "collation admission made no restore call")
            self.assertEqual(target_query("SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname='profile_collation';",
                                          database="profile_collation").strip(),
                             "keep", "collation target sentinel changed")

            normal_restore("profile_target", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(source_calls, calls_at_shutdown)
            restored_state = profile_state(lambda sql, database: target_query(sql, database=database), "profile_target")
            self.assertEqual(restored_state, unpacked_profile["state"], "restored state mismatch")
            self.assertEqual([acl for acl in restored_state["acls"]
                              if acl["object_kind"] == "table" and acl["name"] == "parents"],
                             [expected_parents_acl], "target parents table ACL mismatch")
            self.assertEqual(public_namespace_state("profile_target"), public_before_restore)
            self.assertEqual(public_namespace_state("profile_target"), stock_public,
                             "restore changed explicit stock PG17 public owner/grants")
            spaced_name_resolution = json.loads(target_query('''SELECT jsonb_build_object(
              'catalog_schemas',(SELECT count(*)=2 FROM pg_namespace
                WHERE nspname IN ('profile core','profile data 雪')),
              'catalog_enum',EXISTS(SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
                WHERE n.nspname='profile core' AND t.typname='item_state'),
              'regnamespace_core',to_regnamespace('profile core') IS NOT NULL,
              'regnamespace_data',to_regnamespace('profile data 雪') IS NOT NULL)::text;''',
                                                               database="profile_target"))
            self.assertEqual(spaced_name_resolution, {"catalog_schemas": True, "catalog_enum": True,
                                                       "regnamespace_core": False, "regnamespace_data": False},
                             "spaced schema catalog/name-resolution behavior changed")

            fk_probe = json.loads(target_query('''BEGIN;
              CREATE TEMP TABLE fk_probe_result(sqlstate text);
              INSERT INTO "profile core".parents(id,label) VALUES (910001,'fk probe');
              INSERT INTO "profile data 雪".items(id,parent_id,state,note) VALUES (910001,910001,'new','valid fk');
              DO $probe$ BEGIN
                BEGIN
                  INSERT INTO "profile data 雪".items(id,parent_id,state,note) VALUES (910002,919999,'new','invalid fk');
                EXCEPTION WHEN foreign_key_violation THEN
                  INSERT INTO fk_probe_result VALUES (SQLSTATE);
                END;
              END $probe$;
              SELECT jsonb_build_object('valid_rows',(SELECT count(*) FROM "profile data 雪".items WHERE id=910001),
                'violation_sqlstate',(SELECT sqlstate FROM fk_probe_result))::text;
              ROLLBACK;''', user=OWNER, database="profile_target"))
            self.assertEqual(fk_probe, {"valid_rows": 1, "violation_sqlstate": "23503"})
            self.assertEqual(profile_state(lambda sql, database: target_query(sql, database=database), "profile_target"),
                             restored_state, "rolled-back FK probes changed recovered state")

            self.assertEqual(target_query('SELECT label FROM "profile core".parents ORDER BY id LIMIT 1;',
                                          user=READER, database="profile_target").strip(), "α",
                             "reader table SELECT grant failed")
            self.assertEqual(target_query('SELECT note FROM "profile data 雪".items ORDER BY id LIMIT 1;', user=READER, database="profile_target").strip(), "text")
            self.assertNotEqual(target_run("psql", "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-c", 'SELECT parent_id FROM "profile data 雪".items LIMIT 1;', user=READER, database="profile_target", ok=False).returncode, 0)

            # Post-verification probe: a separate reader cannot see the owner's uncommitted table.
            # Create and commit only after exact restored-state equality; leave it for owned-cluster teardown.
            target_query('''CREATE TABLE "profile data 雪".default_probe(value text PRIMARY KEY);
              INSERT INTO "profile data 雪".default_probe VALUES ('default grant');''',
                         user=OWNER, database="profile_target")
            default_probe_acl = json.loads(target_query('''SELECT COALESCE(jsonb_agg(jsonb_build_object(
              'grantor',pg_get_userbyid(x.grantor),'grantee',pg_get_userbyid(x.grantee),
              'privilege',x.privilege_type,'grant_option',x.is_grantable)
              ORDER BY pg_get_userbyid(x.grantee),x.privilege_type),'[]'::jsonb)::text
              FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
              CROSS JOIN LATERAL aclexplode(c.relacl) x
              WHERE n.nspname='profile data 雪' AND c.relname='default_probe';''', database="profile_target"))
            self.assertEqual(default_probe_acl, [
                {"grantor": OWNER, "grantee": OWNER, "privilege": privilege, "grant_option": False}
                for privilege in ["DELETE", "INSERT", "MAINTAIN", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"]
            ] + [{"grantor": OWNER, "grantee": READER, "privilege": "SELECT", "grant_option": False}])
            self.assertEqual(target_query('SELECT value FROM "profile data 雪".default_probe;',
                                          user=READER, database="profile_target").strip(), "default grant")
            self.assertNotEqual(target_run("psql", "-X", "-w", "-qAt", "-v", "ON_ERROR_STOP=1", "-c",
                                           'INSERT INTO "profile data 雪".default_probe VALUES (\'forbidden\');',
                                           user=READER, database="profile_target", ok=False).returncode, 0)
            before_repeat_target, before_repeat_restore = target_calls, restore_calls
            self.assertRaisesRegex(AssertionError, "normal restore latch refused repeat",
                                   normal_restore, "profile_target", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(target_calls, before_repeat_target); self.assertEqual(restore_calls, before_repeat_restore)

            # Normal admission rejects a user event trigger before restore; only this owned fixture direct-calls the same primitive.
            # The ddl_command_end fault waits for the application items table, then verifies earlier DDL is visible in-transaction.
            target_query('''CREATE TABLE public.rollback_sentinel(value text PRIMARY KEY); INSERT INTO public.rollback_sentinel VALUES ('keep');
              CREATE FUNCTION public.profile_restore_fault() RETURNS event_trigger LANGUAGE plpgsql AS $fault$
              BEGIN
                IF to_regclass('"profile data 雪".items') IS NOT NULL THEN
                  IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname='profile core')
                     OR NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname='profile data 雪')
                     OR NOT EXISTS (SELECT 1 FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
                       WHERE n.nspname='profile core' AND t.typname='item_state') THEN
                    RAISE EXCEPTION 'PROFILE-FAULT-PRECONDITION-FAILED';
                  END IF;
                  RAISE EXCEPTION 'PROFILE-LATE-FAULT-FIXED-MARKER';
                END IF;
              END $fault$;
              CREATE EVENT TRIGGER profile_restore_fault ON ddl_command_end EXECUTE FUNCTION public.profile_restore_fault();''', database="profile_fault")
            normal_restore_calls = restore_calls
            with self.assertRaisesRegex(AssertionError, "normal admission refused user event trigger"):
                normal_restore("profile_fault", unpacked_dump, unpacked_metadata, trusted)
            self.assertEqual(restore_calls, normal_restore_calls, "normal gate made no restore call")
            fault_result = fixture_fault_restore(unpacked_dump)
            self.assertNotEqual(fault_result.returncode, 0, "fixture fault restore unexpectedly succeeded")
            self.assertTrue(b"PROFILE-LATE-FAULT-FIXED-MARKER" in fault_result.stderr,
                            "intended late fixture fault marker absent")
            self.assertFalse(b"PROFILE-FAULT-PRECONDITION-FAILED" in fault_result.stderr,
                             "late fixture fault precondition failed")
            self.assertEqual(target_query("SELECT value FROM public.rollback_sentinel;", database="profile_fault").strip(), "keep")
            self.assertEqual(target_query("SELECT count(*) FROM pg_namespace WHERE nspname IN ('profile core','profile data 雪');", database="profile_fault").strip(), "0")
        finally:
            source_clean = cleanup_registered_cluster(binary, source_root, source_env, source_owner)
            target_clean = cleanup_registered_cluster(binary, target_root, target_env, target_owner)
            if source_clean and target_clean:
                shutil.rmtree(base)
            else:
                self.fail("owned cluster shutdown unconfirmed; private root quarantined: %s" % base)


if __name__ == "__main__":
    unittest.main()
