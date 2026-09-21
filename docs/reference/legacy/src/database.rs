//! Declaration-only offline planning. No tool, database or environment inspection.

use std::{io::Read, path::Path};

use anyhow::{Result, anyhow, ensure};
use serde::{Deserialize, Serialize};

const MAX_INPUT: usize = 16 * 1024;

#[derive(Debug, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Declarations {
    pub version: u8,
    pub connection_mode: ConnectionMode,
    pub source_major: Option<u8>,
    pub client_major: Option<u8>,
    pub destination_major: Option<u8>,
}

#[derive(Debug, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ConnectionMode {
    Direct,
    Session,
    Transaction,
    Unknown,
}

#[derive(Debug, Serialize)]
pub struct DatabasePlan {
    pub version: u8,
    pub offline_plan_only: bool,
    pub execution_supported: bool,
    pub export_ready: bool,
    pub restore_verified: bool,
    pub evidence: &'static str,
    pub declared: Declarations,
    pub blockers: Vec<&'static str>,
    pub unknowns: Vec<&'static str>,
    pub proposed_artifacts: Vec<ProposedArtifact>,
    pub provenance: [&'static str; 4],
}

#[derive(Debug, Serialize)]
pub struct ProposedArtifact {
    pub path: &'static str,
    pub requirements: &'static str,
}

/// Validate at most 16 KiB of non-secret JSON declarations and return a fixed plan.
/// Success means valid input, never readiness or verified recovery. Errors omit
/// supplied values and parser chains, even when formatted with `{:#}` or `{:?}`.
pub fn plan_database(input: &[u8]) -> Result<DatabasePlan> {
    ensure!(
        input.len() <= MAX_INPUT,
        "database declarations exceed 16 KiB"
    );
    // Serde also accepts structs as arrays and unit enums as objects. Enforce
    // the JSON object/string shape, then parse the original bounded bytes again
    // so duplicate fields still fail rather than being collapsed by Value.
    let shape: serde_json::Value =
        serde_json::from_slice(input).map_err(|_| anyhow!("invalid database declarations JSON"))?;
    ensure!(
        shape.is_object()
            && shape
                .get("connection_mode")
                .is_some_and(serde_json::Value::is_string),
        "invalid database declarations JSON"
    );
    let declared: Declarations =
        serde_json::from_slice(input).map_err(|_| anyhow!("invalid database declarations JSON"))?;
    ensure!(
        declared.version == 1,
        "unsupported database declarations version"
    );
    for major in [
        declared.source_major,
        declared.client_major,
        declared.destination_major,
    ]
    .into_iter()
    .flatten()
    {
        ensure!(
            (10..=18).contains(&major),
            "declared PostgreSQL major outside prototype range 10..=18"
        );
    }

    let mut blockers = vec![
        "Database export and restore execution are not implemented.",
        "Supabase-aware coverage and native/CLI parity have not been proven by live round trips.",
    ];
    let mut unknowns = Vec::new();
    if declared.source_major.is_none() {
        unknowns.push("Source PostgreSQL major is undeclared.");
    }
    if declared.client_major.is_none() {
        unknowns.push("PostgreSQL dump client major is undeclared; a Supabase CLI version does not identify it.");
    }
    if declared.destination_major.is_none() {
        unknowns.push("Destination PostgreSQL major is undeclared.");
    }
    if let (Some(source), Some(client)) = (declared.source_major, declared.client_major) {
        if client < source {
            blockers.push("Declared dump client is older than source: PostgreSQL pg_dump refuses newer servers.");
        } else if client > source {
            blockers.push("Declared dump client is newer than source: SPARC conservatively requires matching majors; this is not a PostgreSQL export prohibition.");
        }
    }
    if let Some(destination) = declared.destination_major
        && [declared.source_major, declared.client_major]
            .into_iter()
            .flatten()
            .any(|major| destination < major)
    {
        blockers.push("Declared destination is older than source or dump client: downgrade/backward loading is not supported by this plan.");
    }
    match declared.connection_mode {
        ConnectionMode::Transaction => {
            blockers.push("Transaction pooling is prohibited for dump/restore by SPARC policy.");
        }
        ConnectionMode::Unknown => unknowns.push("Connection mode is undeclared."),
        ConnectionMode::Direct | ConnectionMode::Session => {}
    }
    unknowns.extend([
        "All inputs are user declarations, not detected facts; exact server/client versions, tool/image digests and compatibility remain unverified.",
        "Connectivity, direct/session suitability, TLS, permissions and credential channels are unverified.",
        "All feature usage is unknown, not absent: Auth, Storage, managed-schema customizations, roles, history, extensions, Vault, large objects and automation require inventory.",
        "Auth identities/passwords/sessions/MFA and destination Auth/Storage baselines require compatibility and recovery checks; service configuration is separate.",
        "Storage SQL metadata is not object bytes or verified ownership/RLS; vector/analytics datasets are unsupported.",
        "The migration guide adds storage.buckets_vectors and storage.vector_indexes exclusions; these are recipe additions, not universal CLI defaults.",
        "Independent roles/schema/data/history passes and Storage exports do not prove a shared snapshot; approve a quiet window or test coordination.",
        "Disk/size bounds, empty destination, extension support and source-independent recovery remain unverified.",
        "Restore SQL is executable code; cron/webhooks/queues/hooks require tested side-effect suppression and explicit activation approval.",
        "CLI dry-run is not offline or secret-safe: connection resolution can cause side effects and output expanded PGPASSWORD. No CLI command is executed here.",
    ]);
    Ok(DatabasePlan {
        version: 1,
        offline_plan_only: true,
        execution_supported: false,
        export_ready: false,
        restore_verified: false,
        evidence: "declared_not_observed",
        declared,
        blockers,
        unknowns,
        proposed_artifacts: vec![
            ProposedArtifact {
                path: "database/roles.sql",
                requirements: "Proposed only: exclude reserved/temporary roles and role passwords; replace custom LOGIN passwords. Verify ownership, memberships, grants, default privileges and negative RLS checks.",
            },
            ProposedArtifact {
                path: "database/schema.sql",
                requirements: "Proposed only: Supabase-aware application schema. Default CLI schema exclusions include auth, storage, supabase_migrations and other managed/extension schemas; not a whole-project schema export.",
            },
            ProposedArtifact {
                path: "database/data.sql",
                requirements: "Proposed only: application and eligible Auth/Storage data. Default data exclusions differ from schema exclusions, including auth.schema_migrations, storage.migrations, supabase_functions.migrations, vault, pgsodium and pgsodium_masks. Large-object coverage needs explicit proof.",
            },
            ProposedArtifact {
                path: "database/managed-schema-customizations.sql",
                requirements: "Proposed separate inventory of supported custom policies/triggers/functions; reconcile against destination-owned schemas, never replace managed schemas wholesale.",
            },
            ProposedArtifact {
                path: "database/migration-history/",
                requirements: "Proposed separate supabase_migrations schema/data passes; reconcile without rerunning restored migrations. History does not recover original repository files.",
            },
            ProposedArtifact {
                path: "database/extension-data/",
                requirements: "Proposed follow-up: enumerate extension-owned/configuration data, Vault encrypted records and key metadata. SQL backups omit the source root key; recovery needs an authorized encrypted key capture while source is active and tested decryption in an unused destination. Replacing a target key can strand ciphertext.",
            },
        ],
        provenance: [
            "Supabase CLI v2.117.0, commit 21db855916f2c2b12f61cde923a27094b8528b23; reference only, not a tested compatibility claim",
            "https://github.com/supabase/cli/tree/v2.117.0/apps/cli/src/command-internal",
            "https://supabase.com/docs/guides/platform/migrating-within-supabase/backup-restore",
            "https://www.postgresql.org/docs/18/app-pgdump.html",
        ],
    })
}

/// Read only the supplied regular non-symlink file; no ambient config is read.
/// Requires stable trusted local paths, like the archive prototype: concurrent
/// path replacement is not defended against, and parent symlinks are allowed.
pub fn plan_database_file(path: &Path) -> Result<DatabasePlan> {
    let file = crate::regular_file(path).map_err(|_| {
        anyhow!("database declarations must be a readable regular non-symlink file")
    })?;
    let mut input = Vec::new();
    file.take((MAX_INPUT + 1) as u64)
        .read_to_end(&mut input)
        .map_err(|_| anyhow!("cannot read database declarations"))?;
    plan_database(&input)
}
