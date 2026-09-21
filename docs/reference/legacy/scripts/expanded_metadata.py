"""Offline, fixed-fixture role/history contract; not a general database restorer.

No connections or live command. A caller must independently authorize projects,
verify source provenance and target state, and own the restore transaction.
"""
import json
import re

import hosted_rehearsal as harness

ROLE_NAMES = ("sparc_rehearsal_reader", "sparc_rehearsal_member")
ROLES = [dict(name=name, login=False, superuser=False, createdb=False,
              createrole=False, replication=False, bypassrls=False, inherit=True)
         for name in ROLE_NAMES]
MEMBERSHIPS = [{"role": ROLE_NAMES[0], "member": ROLE_NAMES[1],
                "admin": False, "inherit": True, "set": True}]
ERROR = "invalid expanded metadata"


def require(condition):
    harness.require(condition, ERROR)


def text(value):
    require(type(value) is str and "\x00" not in value)
    value.encode("utf-8", errors="strict")


def validate(raw):
    """Decode at most 64 KiB, preserving nullable history values exactly."""
    try:
        require(type(raw) is bytes and len(raw) <= 65536)
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=harness.pairs)
        require(type(value) is dict and set(value) ==
                {"version", "roles", "memberships", "history"})
        require(type(value["version"]) is int and value["version"] == 1)
        # Serialized equality distinguishes JSON booleans from numeric 0/1.
        for key, expected in (("roles", ROLES), ("memberships", MEMBERSHIPS)):
            require(json.dumps(value[key], sort_keys=True, allow_nan=False) ==
                    json.dumps(expected, sort_keys=True))
        history = value["history"]
        require(type(history) is list and len(history) <= 100)
        versions = []
        for row in history:
            require(type(row) is dict and set(row) == {"version", "name", "statements"})
            require(type(row["version"]) is str and
                    re.fullmatch(r"[0-9]{1,32}", row["version"]) is not None)
            versions.append(row["version"])
            if row["name"] is not None:
                text(row["name"])
            statements = row["statements"]
            if statements is not None:
                require(type(statements) is list and len(statements) <= 100)
                for statement in statements:
                    if statement is not None:
                        text(statement)
        require(versions == sorted(set(versions)))
        return value
    except (harness.RehearsalError, ValueError, UnicodeError, TypeError, RecursionError):
        raise harness.RehearsalError(ERROR) from None


def restore_sql(raw):
    """Return (roles-before-schema, history-after-data) transaction fragments.

    Both collisions are checked before role creation; history is checked again
    before insertion. Caller independently verifies effective permissions afterward.
    """
    value = validate(raw)
    roles = """DO $guard$ BEGIN
IF EXISTS (SELECT FROM pg_roles WHERE rolname IN
 ('sparc_rehearsal_reader', 'sparc_rehearsal_member'))
THEN RAISE EXCEPTION 'expanded role collision'; END IF;
IF EXISTS (SELECT FROM pg_namespace WHERE nspname='supabase_migrations')
THEN RAISE EXCEPTION 'expanded history collision'; END IF;
END $guard$;
CREATE ROLE sparc_rehearsal_reader NOLOGIN INHERIT NOCREATEDB NOCREATEROLE NOBYPASSRLS;
CREATE ROLE sparc_rehearsal_member NOLOGIN INHERIT NOCREATEDB NOCREATEROLE NOBYPASSRLS;
GRANT sparc_rehearsal_reader TO sparc_rehearsal_member WITH ADMIN FALSE;
GRANT sparc_rehearsal_reader TO sparc_rehearsal_member WITH INHERIT TRUE;
GRANT sparc_rehearsal_reader TO sparc_rehearsal_member WITH SET TRUE;
"""
    payload = json.dumps(value["history"], ensure_ascii=True).encode().hex()
    history = """DO $guard$ BEGIN
IF EXISTS (SELECT FROM pg_namespace WHERE nspname='supabase_migrations')
THEN RAISE EXCEPTION 'expanded history collision'; END IF;
END $guard$;
CREATE SCHEMA supabase_migrations AUTHORIZATION postgres;
REVOKE ALL ON SCHEMA supabase_migrations FROM PUBLIC;
CREATE TABLE supabase_migrations.schema_migrations
 (version text PRIMARY KEY, name text, statements text[]);
REVOKE ALL ON supabase_migrations.schema_migrations FROM PUBLIC;
INSERT INTO supabase_migrations.schema_migrations(version, name, statements)
SELECT row->>'version', row->>'name',
 CASE WHEN row->'statements' = 'null'::jsonb THEN NULL
 ELSE ARRAY(SELECT element #>> '{}' FROM
   jsonb_array_elements(row->'statements') WITH ORDINALITY AS items(element, ordinal)
   ORDER BY ordinal) END
FROM jsonb_array_elements(convert_from(decode('""" + payload + """', 'hex'), 'UTF8')::jsonb) AS rows(row);
"""
    return roles, history
