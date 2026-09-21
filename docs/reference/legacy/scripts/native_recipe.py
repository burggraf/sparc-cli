"""Developer-only pinned upstream-script/native-PG17 fixture experiment.

Not unmodified CLI/Docker parity or a full Supabase backup. Callers must validate
config/client versions and source fixture guards using hosted_rehearsal first.
No standalone live command is exposed until capture/restore gates are ready.
"""
import hashlib
import json
import re
from pathlib import Path
import shlex
import stat

import hosted_rehearsal as harness

# Supabase CLI v2.117.0, commit 21db855916f2c2b12f61cde923a27094b8528b23.
# Paths: apps/cli-go/pkg/migration/scripts/dump_{schema,data}.sh
RECIPE = "supabase-2.117.0-native-pg17-fixture-v1"
SCRIPT_SHA256 = {
    "schema": "5cd57189f6565ddf651ff149995398a4c9b1971ca34a0093a77c011a41f21d64",
    "data": "c943c7a926122ea0649ddd4bf9b8fb9b12bed23ac9f33da0b489ac83e687d241",
}


def dump(cfg, work, scripts, mode):
    harness.require(mode in SCRIPT_SHA256, "unsupported recipe mode")
    script = harness.read_file(Path(scripts) / f"dump_{mode}.sh", 16384)
    harness.require(hashlib.sha256(script).hexdigest() == SCRIPT_SHA256[mode],
                    "upstream script digest mismatch")
    # Upstream reads PGPASSWORD under set -u. Empty selects libpq's passfile;
    # the shared runner supplies a scoped PGPASSFILE and verify-full plus CA.
    flags = "--schema=" + harness.SCHEMA if mode == "schema" else ""
    prefix = (
        f"export PATH={shlex.quote(str(Path(cfg['pg_bin'])) + ':/usr/bin:/bin')}\n"
        "export PGPASSWORD='' EXCLUDED_SCHEMAS='' EXTRA_SED=''\n"
        f"export INCLUDED_SCHEMAS={harness.SCHEMA}\n"
        f"export EXTRA_FLAGS={shlex.quote(flags)}\n"
        'export PGOPTIONS="$PGOPTIONS -c default_transaction_read_only=on"\n'
    ).encode()
    return harness.run(["/bin/bash", "-e", "-u", "-o", "pipefail"], work, cfg,
                       prefix + script)


def capture(cfg, work, scripts, archive, recipient):
    archive = Path(archive).absolute()
    harness.require(not archive.exists() and not archive.is_symlink(),
                    "archive destination already exists")
    harness.require(re.fullmatch(r"age1[a-z0-9]{58}", recipient) is not None,
                    "native age recipient required")
    harness.versions(cfg, work)
    harness.check(cfg, work, fixture=True)
    artifacts = {}
    for mode in ("schema", "data"):
        sql = dump(cfg, work, scripts, mode)
        harness.require(len(sql) < harness.LIMIT
                        and b"-- PostgreSQL database dump\n" in sql
                        and b"-- PostgreSQL database dump complete\n" in sql,
                        "incomplete or oversized recipe dump")
        artifacts[mode + ".sql"] = sql
    harness.require(sum(map(len, artifacts.values())) < harness.LIMIT - 65536,
                    "combined recipe output exceeds limit")
    harness.check(cfg, work, fixture=True)
    material = Path(work) / "material"
    material.mkdir(mode=0o700)
    for name, data in artifacts.items():
        harness.private_write(material / name, data)
    metadata = {"version": 1, "recipe": RECIPE, "source_ref": cfg["project_ref"],
                "scripts": SCRIPT_SHA256,
                "sha256": {name: hashlib.sha256(data).hexdigest()
                           for name, data in artifacts.items()}}
    harness.private_write(material / "recovery.json", json.dumps(metadata).encode())
    harness.run([cfg["sparc"], "pack", str(material), str(archive), recipient], work)


def restore(cfg, work, archive, identity):
    """Execute ONLY trusted locally generated fixture SQL, never arbitrary archives."""
    archive = Path(archive).absolute()
    harness.require(archive.is_dir() and not archive.is_symlink(), "invalid archive directory")
    expected = {"manifest.age", "00000000.age", "00000001.age", "00000002.age"}
    total = 0
    for entry in archive.iterdir():
        harness.require(entry.name in expected, "unexpected archive entry")
        expected.remove(entry.name)
        info = entry.lstat()
        harness.require(stat.S_ISREG(info.st_mode) and info.st_size <= harness.LIMIT,
                        "unsafe archive entry")
        total += info.st_size
    harness.require(not expected and total <= harness.LIMIT + 65536,
                    "incomplete or oversized archive")
    identity = harness.absolute_file(str(Path(identity).absolute()), private=True)
    material = Path(work) / "restoration"
    harness.run([cfg["sparc"], "unpack", str(archive), str(material), str(identity)], work)
    harness.require({p.name for p in material.iterdir()} == {"schema.sql", "data.sql", "recovery.json"},
                    "unexpected recovery inventory")
    meta = harness.read_json(material / "recovery.json",
                             ("version", "recipe", "source_ref", "scripts", "sha256"))
    harness.require(type(meta["version"]) is int and meta["version"] == 1
                    and meta["recipe"] == RECIPE and meta["scripts"] == SCRIPT_SHA256
                    and type(meta["source_ref"]) is str
                    and re.fullmatch(r"[a-z]{20}", meta["source_ref"]) is not None
                    and meta["source_ref"] != cfg["project_ref"], "invalid recovery contract")
    harness.require(type(meta["sha256"]) is dict
                    and set(meta["sha256"]) == {"schema.sql", "data.sql"}, "invalid artifact digests")
    artifacts = []
    for name in ("schema.sql", "data.sql"):
        data = harness.read_file(material / name, harness.LIMIT, private=True)
        harness.require(hashlib.sha256(data).hexdigest() == meta["sha256"][name],
                        "artifact digest mismatch")
        artifacts.append(data)
    harness.require(sum(map(len, artifacts)) < harness.LIMIT - 65536, "combined SQL exceeds limit")
    harness.versions(cfg, work)
    harness.check(cfg, work)
    # The upstream data script resets settings. Restore timeouts and normal
    # trigger behavior explicitly before integrity probes. SQL remains trusted code.
    sql = (("BEGIN;\n" + harness.guard()).encode() + b"\n".join(artifacts)
           + ("\nSET LOCAL session_replication_role=origin;\n"
              "SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='5s';\n"
              "SET LOCAL timezone='UTC';\n" + harness.guard(True) + harness.assertions()
              + harness.PROBE + harness.assertions()
              + "COMMIT; SELECT 'sparc-recipe-restore-ok';").encode())
    output = harness.psql(cfg, work, sql)
    harness.require(output.rstrip().endswith(b"sparc-recipe-restore-ok"),
                    "restore incomplete; quarantine target")


def cleanup_sql():
    """Parent-only authorized fixture cleanup; caller must verify target identity."""
    return ("BEGIN;\n" + harness.guard(True)
            + "LOCK TABLE sparc_rehearsal.notes, sparc_rehearsal.owners, "
              "sparc_rehearsal.empty_table IN ACCESS EXCLUSIVE MODE;\n"
            + harness.assertions()
            + "DROP TABLE sparc_rehearsal.notes RESTRICT;\n"
              "DROP TABLE sparc_rehearsal.owners RESTRICT;\n"
              "DROP TABLE sparc_rehearsal.empty_table RESTRICT;\n"
              "DROP SCHEMA sparc_rehearsal RESTRICT;\n"
            + harness.guard() + "COMMIT; SELECT 'sparc-recipe-cleanup-ok';")
