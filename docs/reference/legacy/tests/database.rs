use std::{fs, process::Command};

use anyhow::Result;
use serde_json::Value;

#[test]
fn cli_produces_an_offline_blocked_plan_without_tools() -> Result<()> {
    let work = tempfile::tempdir()?;
    let input = work.path().join("declarations.json");
    fs::write(
        &input,
        br#"{"version":1,"connection_mode":"direct","source_major":17,"client_major":15,"destination_major":17}"#,
    )?;
    let output = Command::new(env!("CARGO_BIN_EXE_sparc"))
        .arg("plan-database")
        .arg(&input)
        .env("PATH", "")
        .env("PGPASSWORD", "SYNTHETIC-SECRET-DO-NOT-ECHO")
        .env("SUPABASE_ACCESS_TOKEN", "SYNTHETIC-SECRET-DO-NOT-ECHO")
        .output()?;
    assert!(
        output.status.success(),
        "{}",
        String::from_utf8_lossy(&output.stderr)
    );
    let plan: Value = serde_json::from_slice(&output.stdout)?;
    assert_eq!(plan["offline_plan_only"], true);
    assert_eq!(plan["execution_supported"], false);
    assert_eq!(plan["export_ready"], false);
    assert_eq!(plan["restore_verified"], false);
    assert!(
        plan["blockers"]
            .as_array()
            .unwrap()
            .iter()
            .any(|blocker| { blocker.as_str().unwrap().contains("client is older") })
    );
    assert!(!String::from_utf8_lossy(&output.stdout).contains("SYNTHETIC-SECRET"));
    assert!(output.stderr.is_empty());
    Ok(())
}

#[test]
fn declarations_drive_only_conservative_blockers_and_unknowns() -> Result<()> {
    use sparc::database::plan_database;

    for (source, client, destination, expected) in [
        (17, 15, 17, Some("client is older")),
        (15, 17, 15, Some("destination is older")),
        (17, 17, 15, Some("destination is older")),
        (15, 17, 17, Some("conservatively requires matching")),
        (17, 17, 17, None),
        (10, 10, 18, None),
        (18, 18, 18, None),
    ] {
        let input = serde_json::to_vec(&serde_json::json!({
            "version": 1, "connection_mode": "session",
            "source_major": source, "client_major": client, "destination_major": destination,
        }))?;
        let plan = plan_database(&input)?;
        assert!(plan.offline_plan_only);
        assert!(!plan.execution_supported && !plan.export_ready && !plan.restore_verified);
        assert_eq!(plan.evidence, "declared_not_observed");
        assert!(plan.blockers[0].contains("not implemented"));
        assert!(plan.blockers[1].contains("not been proven"));
        if let Some(expected) = expected {
            assert!(
                plan.blockers
                    .iter()
                    .any(|message| message.contains(expected))
            );
        } else {
            assert_eq!(plan.blockers.len(), 2);
        }
        assert_eq!(
            serde_json::to_vec(&plan)?,
            serde_json::to_vec(&plan_database(&input)?)?
        );
        assert_eq!(
            plan.proposed_artifacts
                .iter()
                .map(|artifact| artifact.path)
                .collect::<Vec<_>>(),
            [
                "database/roles.sql",
                "database/schema.sql",
                "database/data.sql",
                "database/managed-schema-customizations.sql",
                "database/migration-history/",
                "database/extension-data/"
            ]
        );
        assert!(
            plan.unknowns
                .iter()
                .any(|message| message.contains("All feature usage is unknown"))
        );
        assert!(plan.unknowns.iter().any(|message| message.contains("TLS")));
    }
    let omitted = plan_database(br#"{"version":1,"connection_mode":"unknown"}"#)?;
    let nulls = plan_database(br#"{"version":1,"connection_mode":"unknown","source_major":null,"client_major":null,"destination_major":null}"#)?;
    assert_eq!(serde_json::to_vec(&omitted)?, serde_json::to_vec(&nulls)?);
    assert!(omitted.declared.source_major.is_none());
    assert!(omitted.declared.client_major.is_none());
    assert!(omitted.declared.destination_major.is_none());
    for component in ["Source", "client", "Destination", "Connection"] {
        assert!(
            omitted
                .unknowns
                .iter()
                .any(|message| message.contains(component))
        );
    }
    for mode in ["direct", "session", "transaction", "unknown"] {
        let input = format!(r#"{{"version":1,"connection_mode":"{mode}"}}"#);
        let plan = plan_database(input.as_bytes())?;
        assert_eq!(
            plan.blockers.len(),
            if mode == "transaction" { 3 } else { 2 }
        );
        assert_eq!(
            plan.blockers
                .iter()
                .any(|message| message.contains("Transaction pooling")),
            mode == "transaction"
        );
    }
    Ok(())
}

#[test]
fn rejects_invalid_declarations_without_leaking_error_chains() -> Result<()> {
    use sparc::database::plan_database;

    let valid = serde_json::json!({"version":1,"connection_mode":"direct"});
    for (field, value) in [
        ("password", serde_json::json!("SYNTHETIC-SECRET")),
        ("url", serde_json::json!("SYNTHETIC-SECRET")),
        ("connection_mode", serde_json::json!("SYNTHETIC-SECRET")),
        ("version", serde_json::json!(0)),
        ("version", serde_json::json!(2)),
        ("version", serde_json::json!("SYNTHETIC-SECRET")),
        ("source_major", serde_json::json!(9)),
        ("client_major", serde_json::json!(19)),
        ("destination_major", serde_json::json!(-1)),
        ("source_major", serde_json::json!(17.5)),
        ("client_major", serde_json::json!(256)),
        ("destination_major", serde_json::json!("SYNTHETIC-SECRET")),
    ] {
        let mut input = valid.clone();
        input[field] = value;
        let error = plan_database(&serde_json::to_vec(&input)?).unwrap_err();
        assert!(!format!("{error:#} {error:?}").contains("SYNTHETIC-SECRET"));
        assert_eq!(error.chain().count(), 1);
    }
    for input in [
        b"SYNTHETIC-SECRET".as_slice(),
        br#"{"version":1,"connection_mode":"direct","password":"SYNTHETIC-SECRET""#,
        br#"{"version":1,"version":1,"connection_mode":"direct"}"#,
        br#"{"version":1}"#,
        br#"{"connection_mode":"direct"}"#,
        br#"{"version":null,"connection_mode":"direct"}"#,
        br#"{"version":1,"connection_mode":null}"#,
        br#"[1,"direct",17,17,17]"#,
        br#"[1,"direct",null,null,null]"#,
        br#"{"version":1,"connection_mode":{"direct":null}}"#,
        br#"{"version":1,"connection_mode":{"unknown":null}}"#,
        br#"{"version":1,"source_major":null,"source_major":17,"connection_mode":"direct"}"#,
        b"[]",
        b"\xff",
    ] {
        let error = plan_database(input).unwrap_err();
        assert!(!format!("{error:#} {error:?}").contains("SYNTHETIC-SECRET"));
        assert_eq!(error.chain().count(), 1);
    }
    Ok(())
}

#[test]
fn file_and_byte_inputs_enforce_the_exact_bound() -> Result<()> {
    use sparc::database::{plan_database, plan_database_file};

    let work = tempfile::tempdir()?;
    let path = work.path().join("input.json");
    let mut input = br#"{"version":1,"connection_mode":"direct"}"#.to_vec();
    input.resize(16 * 1024, b' ');
    fs::write(&path, &input)?;
    assert!(plan_database(&input).is_ok());
    assert!(plan_database_file(&path).is_ok());
    input.push(b' ');
    fs::write(&path, &input)?;
    assert!(plan_database(&input).is_err());
    assert!(plan_database_file(&path).is_err());
    for path in [
        work.path().to_owned(),
        work.path().join("SYNTHETIC-SECRET-missing"),
    ] {
        let error = plan_database_file(&path).unwrap_err();
        assert!(!format!("{error:#} {error:?}").contains("SYNTHETIC-SECRET"));
        assert_eq!(error.chain().count(), 1);
    }
    #[cfg(unix)]
    {
        use std::os::unix::{fs::symlink, net::UnixListener};
        let link = work.path().join("link");
        symlink(&path, &link)?;
        assert!(plan_database_file(&link).is_err());
        let socket = work.path().join("socket");
        let _listener = UnixListener::bind(&socket)?;
        assert!(plan_database_file(&socket).is_err());
    }
    Ok(())
}

#[test]
fn cli_rejects_invalid_input_without_echo_and_documents_validity_only() -> Result<()> {
    let work = tempfile::tempdir()?;
    let path = work.path().join("SYNTHETIC-SECRET-input");
    for input in [
        br#"{"version":1,"connection_mode":"direct","password":"SYNTHETIC-SECRET"}"#.as_slice(),
        br#"[1,"direct",17,17,17]"#,
        br#"[1,"direct",null,null,null]"#,
        br#"{"version":1,"connection_mode":{"direct":null}}"#,
        br#"{"version":1,"connection_mode":{"unknown":null}}"#,
        br#"{"version":1,"source_major":null,"source_major":17,"connection_mode":"direct"}"#,
    ] {
        fs::write(&path, input)?;
        let output = Command::new(env!("CARGO_BIN_EXE_sparc"))
            .arg("plan-database")
            .arg(&path)
            .output()?;
        assert!(!output.status.success());
        assert!(output.stdout.is_empty());
        assert_eq!(
            String::from_utf8(output.stderr)?,
            "sparc: invalid database declarations JSON\n"
        );
    }
    fs::remove_file(&path)?;
    let output = Command::new(env!("CARGO_BIN_EXE_sparc"))
        .arg("plan-database")
        .arg(&path)
        .output()?;
    assert!(!output.status.success());
    assert!(!String::from_utf8_lossy(&output.stderr).contains("SYNTHETIC-SECRET"));
    let help = Command::new(env!("CARGO_BIN_EXE_sparc"))
        .arg("--help")
        .output()?;
    assert!(help.status.success());
    assert!(
        String::from_utf8_lossy(&help.stdout)
            .contains("not export readiness or a verified restore")
    );
    Ok(())
}
