use std::{
    fs,
    path::Path,
    process::{Command, Output},
};

use anyhow::Result;

fn run(args: &[&str]) -> Result<Output> {
    Ok(Command::new(env!("CARGO_BIN_EXE_sparc"))
        .args(args)
        .output()?)
}

fn text(path: &Path) -> &str {
    path.to_str().unwrap()
}

#[test]
fn cli_round_trip_keeps_key_out_of_output_and_refuses_overwrites() -> Result<()> {
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    let archive = work.path().join("archive");
    let restored = work.path().join("restored");
    let key = work.path().join("recovery.agekey");
    fs::create_dir(&source)?;
    fs::write(
        source.join("data.sql"),
        b"-- synthetic fixture, never executed\n",
    )?;

    let generated = run(&["keygen", text(&key)])?;
    assert!(
        generated.status.success(),
        "{}",
        String::from_utf8_lossy(&generated.stderr)
    );
    let public = String::from_utf8(generated.stdout)?.trim().to_owned();
    assert!(public.starts_with("age1"));
    let key_contents = fs::read_to_string(&key)?;
    assert!(key_contents.contains("AGE-SECRET-KEY-"));
    assert!(!String::from_utf8_lossy(&generated.stderr).contains("AGE-SECRET-KEY-"));
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(fs::metadata(&key)?.permissions().mode() & 0o777, 0o600);
    }
    assert!(!run(&["keygen", text(&key)])?.status.success());
    assert_eq!(fs::read_to_string(&key)?, key_contents);

    assert!(
        run(&["pack", text(&source), text(&archive), &public])?
            .status
            .success()
    );
    fs::remove_dir_all(&source)?;
    let verified = run(&["verify", text(&archive), text(&key)])?;
    assert!(
        verified.status.success(),
        "{}",
        String::from_utf8_lossy(&verified.stderr)
    );
    assert!(String::from_utf8_lossy(&verified.stdout).contains("Local archive verified"));
    assert!(
        run(&["unpack", text(&archive), text(&restored), text(&key)])?
            .status
            .success()
    );
    assert_eq!(
        fs::read(restored.join("data.sql"))?,
        b"-- synthetic fixture, never executed\n"
    );
    assert!(
        !run(&["unpack", text(&archive), text(&restored), text(&key)])?
            .status
            .success()
    );
    Ok(())
}

#[test]
fn cli_help_and_invalid_inputs_are_explicit() -> Result<()> {
    let help = run(&["--help"])?;
    assert!(help.status.success());
    assert!(String::from_utf8_lossy(&help.stdout).contains("not a Supabase backup"));
    assert!(!run(&[])?.status.success());
    assert!(!run(&["unknown"])?.status.success());
    assert!(!run(&["pack", "only-one-argument"])?.status.success());
    let invalid = run(&[
        "pack",
        "missing-source",
        "new-output",
        "sensitive-invalid-recipient",
    ])?;
    assert!(!invalid.status.success());
    assert!(!String::from_utf8_lossy(&invalid.stderr).contains("sensitive-invalid-recipient"));
    Ok(())
}

#[test]
fn cli_rejects_wrong_and_malformed_identities_without_echoing_secrets() -> Result<()> {
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    let archive = work.path().join("archive");
    let key = work.path().join("key.agekey");
    let other_key = work.path().join("other.agekey");
    fs::create_dir(&source)?;
    let generated = run(&["keygen", text(&key)])?;
    assert!(generated.status.success());
    let public = String::from_utf8(generated.stdout)?.trim().to_owned();
    assert!(run(&["keygen", text(&other_key)])?.status.success());
    assert!(
        run(&["pack", text(&source), text(&archive), &public])?
            .status
            .success()
    );
    assert!(
        !run(&["verify", text(&archive), text(&other_key)])?
            .status
            .success()
    );
    fs::write(&other_key, "PRIVATE-INVALID-KEY-DO-NOT-ECHO")?;
    let invalid = run(&["verify", text(&archive), text(&other_key)])?;
    assert!(!invalid.status.success());
    assert!(!String::from_utf8_lossy(&invalid.stderr).contains("PRIVATE-INVALID"));
    Ok(())
}
