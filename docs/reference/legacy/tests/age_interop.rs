//! Explicit interoperability check against the independently installed Go age CLI.

use std::{fs, process::Command};

use age::x25519::Identity;
use anyhow::{Result, anyhow, ensure};

#[test]
#[ignore = "requires the independently installed Go age CLI; run with --ignored"]
fn independent_age_implementation_round_trip() -> Result<()> {
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    let archive = work.path().join("archive");
    let key = work.path().join("recovery.agekey");
    fs::create_dir(&source)?;
    let contents = b"Synthetic interoperability fixture, not a production secret.";
    fs::write(source.join("data.bin"), contents)?;
    let generated = Command::new(env!("CARGO_BIN_EXE_sparc"))
        .arg("keygen")
        .arg(&key)
        .output()?;
    ensure!(generated.status.success(), "SPARC key generation failed");
    let recipient = String::from_utf8(generated.stdout)?.trim().to_owned();
    let key_text = fs::read_to_string(&key)?;
    let identity: Identity = key_text
        .lines()
        .find(|line| line.starts_with("AGE-SECRET-KEY-"))
        .ok_or_else(|| anyhow!("no native identity in generated file"))?
        .parse()
        .map_err(|_| anyhow!("invalid generated identity"))?;
    let manifest = sparc::pack(&source, &archive, &identity.to_public())?;

    let decoded_manifest = Command::new("age")
        .args(["--decrypt", "--identity"])
        .arg(&key)
        .arg(archive.join("manifest.age"))
        .output()?;
    ensure!(
        decoded_manifest.status.success(),
        "Go age could not decrypt SPARC manifest"
    );
    assert_eq!(
        serde_json::from_slice::<sparc::Manifest>(&decoded_manifest.stdout)?,
        manifest
    );
    let decoded_payload = Command::new("age")
        .args(["--decrypt", "--identity"])
        .arg(&key)
        .arg(archive.join("00000000.age"))
        .output()?;
    ensure!(
        decoded_payload.status.success(),
        "Go age could not decrypt SPARC payload"
    );
    assert_eq!(decoded_payload.stdout, contents);

    // Re-encrypt both artifacts with Go age, then verify and restore with the Rust library.
    let json = work.path().join("synthetic-manifest.json");
    fs::write(&json, &decoded_manifest.stdout)?;
    for (input, filename) in [
        (source.join("data.bin"), "00000000.age"),
        (json, "manifest.age"),
    ] {
        let encrypted = work.path().join("independent.age");
        let output = Command::new("age")
            .args(["--encrypt", "--recipient"])
            .arg(&recipient)
            .arg("--output")
            .arg(&encrypted)
            .arg(input)
            .output()?;
        ensure!(output.status.success(), "independent age encryption failed");
        fs::rename(encrypted, archive.join(filename))?;
    }
    fs::remove_dir_all(&source)?;
    assert_eq!(sparc::verify(&archive, &identity)?, manifest);
    sparc::unpack(&archive, &work.path().join("restored"), &identity)?;
    assert_eq!(fs::read(work.path().join("restored/data.bin"))?, contents);
    Ok(())
}
