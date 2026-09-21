use std::{fs, path::Path};

use age::x25519::Identity;
use anyhow::Result;

#[test]
fn round_trip_without_source() -> Result<()> {
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    let archive = work.path().join("archive");
    let destination = work.path().join("restored");
    fs::create_dir_all(source.join("database"))?;
    fs::create_dir_all(source.join("storage/empty-directory"))?;
    let sql = b"-- synthetic data only\nSELECT 'never execute archived SQL';\n";
    let binary: Vec<u8> = (0..200_000).map(|n| (n % 251) as u8).collect();
    fs::write(source.join("database/data.sql"), sql)?;
    fs::write(source.join("storage/photo.bin"), &binary)?;
    fs::write(source.join("settings.json"), br#"{"smtp":"synthetic"}"#)?;
    fs::write(source.join("empty.txt"), b"")?;
    fs::write(source.join("café.txt"), "Unicode filenames are preserved")?;

    let identity = Identity::generate();
    let manifest = sparc::pack(&source, &archive, &identity.to_public())?;
    fs::remove_dir_all(&source)?;
    assert_eq!(manifest.artifacts.len(), 5);
    assert_eq!(sparc::verify(&archive, &identity)?, manifest);
    assert_eq!(sparc::unpack(&archive, &destination, &identity)?, manifest);
    assert_eq!(fs::read(destination.join("database/data.sql"))?, sql);
    assert_eq!(fs::read(destination.join("storage/photo.bin"))?, binary);
    assert_eq!(
        fs::read(destination.join("settings.json"))?,
        br#"{"smtp":"synthetic"}"#
    );
    assert!(destination.join("storage/empty-directory").is_dir());
    assert!(fs::read(destination.join("empty.txt"))?.is_empty());
    assert_eq!(
        fs::read_to_string(destination.join("café.txt"))?,
        "Unicode filenames are preserved"
    );
    Ok(())
}

fn sample() -> Result<(tempfile::TempDir, Identity, sparc::Manifest)> {
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    fs::create_dir(&source)?;
    fs::write(source.join("data.txt"), b"synthetic confidential contents")?;
    let identity = Identity::generate();
    let manifest = sparc::pack(&source, &work.path().join("archive"), &identity.to_public())?;
    Ok((work, identity, manifest))
}

fn replace_manifest(archive: &Path, identity: &Identity, value: &serde_json::Value) -> Result<()> {
    fs::write(
        archive.join("manifest.age"),
        age::encrypt(&identity.to_public(), &serde_json::to_vec(value)?)?,
    )?;
    Ok(())
}

#[test]
fn empty_directory_round_trip() -> Result<()> {
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    fs::create_dir(&source)?;
    let identity = Identity::generate();
    sparc::pack(&source, &work.path().join("archive"), &identity.to_public())?;
    let result = sparc::unpack(
        &work.path().join("archive"),
        &work.path().join("restored"),
        &identity,
    )?;
    assert!(result.artifacts.is_empty());
    assert_eq!(fs::read_dir(work.path().join("restored"))?.count(), 0);
    Ok(())
}

#[test]
fn refuses_existing_destinations_and_recursive_pack() -> Result<()> {
    let (work, identity, _) = sample()?;
    let source = work.path().join("source");
    let archive = work.path().join("archive");
    let before = fs::read(archive.join("manifest.age"))?;
    assert!(sparc::pack(&source, &archive, &identity.to_public()).is_err());
    assert_eq!(fs::read(archive.join("manifest.age"))?, before);
    assert!(
        sparc::pack(
            &source,
            &source.join("nested-archive"),
            &identity.to_public()
        )
        .is_err()
    );
    assert!(!source.join("nested-archive").exists());

    for occupied in [false, true] {
        let destination = work.path().join(format!("occupied-{occupied}"));
        fs::create_dir(&destination)?;
        if occupied {
            fs::write(destination.join("keep.txt"), b"do not overwrite")?;
        }
        assert!(sparc::unpack(&archive, &destination, &identity).is_err());
        assert!(sparc::unpack(&archive, &destination.join("."), &identity).is_err());
        assert!(!destination.join("data.txt").exists());
        if occupied {
            assert_eq!(fs::read(destination.join("keep.txt"))?, b"do not overwrite");
        }
    }
    Ok(())
}

#[test]
fn rejects_corrupt_incomplete_and_unlisted_payloads() -> Result<()> {
    for case in [
        "truncated",
        "tampered",
        "appended",
        "missing",
        "extra",
        "no-manifest",
        "bad-manifest",
        "wrong-key",
    ] {
        let (work, identity, _) = sample()?;
        let archive = work.path().join("archive");
        let payload = archive.join("00000000.age");
        let key = if case == "wrong-key" {
            Identity::generate()
        } else {
            identity
        };
        match case {
            "truncated" => {
                let mut bytes = fs::read(&payload)?;
                bytes.pop();
                fs::write(&payload, bytes)?;
            }
            "tampered" => {
                let mut bytes = fs::read(&payload)?;
                let last = bytes.len() - 1;
                bytes[last] ^= 1;
                fs::write(&payload, bytes)?;
            }
            "appended" => {
                use std::io::Write;
                fs::OpenOptions::new()
                    .append(true)
                    .open(&payload)?
                    .write_all(b"unlisted trailing bytes")?;
            }
            "missing" => fs::remove_file(&payload)?,
            "extra" => fs::write(archive.join("unlisted.age"), b"unexpected")?,
            "no-manifest" => fs::remove_file(archive.join("manifest.age"))?,
            "bad-manifest" => fs::write(archive.join("manifest.age"), b"not an age file")?,
            _ => {}
        }
        assert!(sparc::verify(&archive, &key).is_err(), "accepted {case}");
        let destination = work.path().join("restored");
        assert!(
            sparc::unpack(&archive, &destination, &key).is_err(),
            "restored {case}"
        );
        assert!(
            !destination.join("data.txt").exists(),
            "published unverified file: {case}"
        );
    }
    Ok(())
}

#[test]
fn rejects_authenticated_unsafe_manifests_before_output() -> Result<()> {
    let (work, identity, manifest) = sample()?;
    let archive = work.path().join("archive");
    let original = serde_json::to_value(manifest)?;
    let mut invalid = Vec::new();
    for path in [
        "../escaped",
        "/absolute",
        "a/../../escaped",
        "a\\b",
        "C:drive",
        "a//b",
        "./file",
        "a/./b",
        "a\nb",
    ] {
        let mut value = original.clone();
        value["artifacts"][0]["path"] = path.into();
        invalid.push(value);
    }
    for (field, value) in [
        ("version", serde_json::json!(99)),
        ("format", serde_json::json!("other-tool")),
        ("unknown", serde_json::json!(true)),
    ] {
        let mut changed = original.clone();
        changed[field] = value;
        invalid.push(changed);
    }
    for (field, value) in [
        ("bytes", serde_json::json!(u64::MAX)),
        ("id", serde_json::json!(42)),
        ("sha256", serde_json::json!("invalid")),
    ] {
        let mut changed = original.clone();
        changed["artifacts"][0][field] = value;
        invalid.push(changed);
    }
    let mut duplicate = original.clone();
    let record = duplicate["artifacts"][0].clone();
    duplicate["artifacts"].as_array_mut().unwrap().push(record);
    invalid.push(duplicate);
    let mut parent_missing = original.clone();
    parent_missing["artifacts"][0]["path"] = "missing/data.txt".into();
    invalid.push(parent_missing);
    let mut conflicting_directory = original.clone();
    conflicting_directory["directories"] = serde_json::json!(["data.txt"]);
    invalid.push(conflicting_directory);
    let mut deep = original.clone();
    deep["artifacts"][0]["path"] = vec!["a"; 65].join("/").into();
    invalid.push(deep);

    for (index, value) in invalid.iter().enumerate() {
        replace_manifest(&archive, &identity, value)?;
        assert!(
            sparc::verify(&archive, &identity).is_err(),
            "accepted invalid manifest {index}"
        );
        let destination = work.path().join(format!("output-{index}"));
        assert!(sparc::unpack(&archive, &destination, &identity).is_err());
        assert!(
            !destination.exists(),
            "created output for invalid manifest {index}"
        );
    }
    assert!(!work.path().join("escaped").exists());
    Ok(())
}

#[test]
fn checks_payload_size_and_hash_not_just_age_authentication() -> Result<()> {
    for field in ["bytes", "sha256"] {
        let (work, identity, manifest) = sample()?;
        let archive = work.path().join("archive");
        let mut value = serde_json::to_value(manifest)?;
        value["artifacts"][0][field] = if field == "bytes" {
            serde_json::json!(0)
        } else {
            "0".repeat(64).into()
        };
        replace_manifest(&archive, &identity, &value)?;
        assert!(sparc::verify(&archive, &identity).is_err());
        let destination = work.path().join("restored");
        assert!(sparc::unpack(&archive, &destination, &identity).is_err());
        assert!(!destination.join("data.txt").exists());
    }
    Ok(())
}

#[cfg(unix)]
#[test]
fn rejects_symlinks_and_keeps_outputs_private() -> Result<()> {
    use std::os::unix::fs::{PermissionsExt, symlink};
    let (work, identity, _) = sample()?;
    let archive = work.path().join("archive");
    let source = work.path().join("source");
    assert_eq!(fs::metadata(&archive)?.permissions().mode() & 0o777, 0o700);
    for entry in fs::read_dir(&archive)? {
        assert_eq!(entry?.metadata()?.permissions().mode() & 0o777, 0o600);
    }
    let destination = work.path().join("restored");
    sparc::unpack(&archive, &destination, &identity)?;
    assert_eq!(
        fs::metadata(&destination)?.permissions().mode() & 0o777,
        0o700
    );
    assert_eq!(
        fs::metadata(destination.join("data.txt"))?
            .permissions()
            .mode()
            & 0o777,
        0o600
    );

    symlink(&destination, work.path().join("linked-destination"))?;
    assert!(sparc::unpack(&archive, &work.path().join("linked-destination"), &identity).is_err());
    symlink(&archive, work.path().join("linked-archive"))?;
    assert!(sparc::verify(&work.path().join("linked-archive"), &identity).is_err());
    symlink(&source, work.path().join("linked-source"))?;
    assert!(
        sparc::pack(
            &work.path().join("linked-source"),
            &work.path().join("bad-source"),
            &identity.to_public()
        )
        .is_err()
    );
    symlink(destination.join("data.txt"), source.join("link.txt"))?;
    assert!(
        sparc::pack(
            &source,
            &work.path().join("bad-pack"),
            &identity.to_public()
        )
        .is_err()
    );
    fs::remove_file(archive.join("00000000.age"))?;
    symlink(destination.join("data.txt"), archive.join("00000000.age"))?;
    assert!(sparc::verify(&archive, &identity).is_err());
    Ok(())
}

#[test]
fn bounds_manifest_size_entry_count_and_nested_restore() -> Result<()> {
    let (work, identity, manifest) = sample()?;
    let archive = work.path().join("archive");
    assert!(sparc::unpack(&archive, &archive.join("restored"), &identity).is_err());
    assert!(!archive.join("restored").exists());
    let mut value = serde_json::to_value(&manifest)?;
    value["directories"] = serde_json::json!(vec!["directory"; 100_001]);
    replace_manifest(&archive, &identity, &value)?;
    assert!(
        sparc::verify(&archive, &identity)
            .unwrap_err()
            .to_string()
            .contains("too many")
    );

    fs::write(
        archive.join("manifest.age"),
        age::encrypt(&identity.to_public(), &vec![b' '; 16 * 1024 * 1024 + 1])?,
    )?;
    assert!(
        sparc::verify(&archive, &identity)
            .unwrap_err()
            .to_string()
            .contains("size limit")
    );
    fs::OpenOptions::new()
        .write(true)
        .open(archive.join("manifest.age"))?
        .set_len(32 * 1024 * 1024 + 1)?;
    assert!(
        sparc::verify(&archive, &identity)
            .unwrap_err()
            .to_string()
            .contains("size limit")
    );
    Ok(())
}

#[cfg(unix)]
#[test]
fn rejects_special_files_before_creating_archive() -> Result<()> {
    use std::os::unix::net::UnixListener;
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    fs::create_dir(&source)?;
    let identity = Identity::generate();
    let socket = UnixListener::bind(source.join("socket"))?;
    assert!(sparc::pack(&source, &work.path().join("archive"), &identity.to_public()).is_err());
    assert!(!work.path().join("archive").exists());
    drop(socket);
    Ok(())
}

// macOS rejects invalid UTF-8 filenames at creation time, before SPARC can read them.
#[cfg(all(unix, not(target_os = "macos")))]
#[test]
fn rejects_non_utf8_source_names() -> Result<()> {
    use std::{ffi::OsString, os::unix::ffi::OsStringExt};
    let work = tempfile::tempdir()?;
    let source = work.path().join("source");
    fs::create_dir(&source)?;
    fs::write(source.join(OsString::from_vec(vec![0xff])), b"synthetic")?;
    assert!(
        sparc::pack(
            &source,
            &work.path().join("archive"),
            &Identity::generate().to_public()
        )
        .is_err()
    );
    assert!(!work.path().join("archive").exists());
    Ok(())
}

#[test]
fn filesystem_name_collisions_never_overwrite_verified_files() -> Result<()> {
    for (first, second) in [
        ("data.txt", "DATA.TXT"),
        ("caf\u{00e9}.txt", "cafe\u{0301}.txt"),
    ] {
        let (work, identity, manifest) = sample()?;
        let archive = work.path().join("archive");
        let probe = work.path().join("probe");
        fs::create_dir(&probe)?;
        fs::write(probe.join(first), b"probe")?;
        let names_collide = probe.join(second).exists();
        let mut value = serde_json::to_value(manifest)?;
        value["artifacts"][0]["path"] = first.into();
        let mut other = value["artifacts"][0].clone();
        other["id"] = 1.into();
        other["path"] = second.into();
        value["artifacts"].as_array_mut().unwrap().push(other);
        fs::copy(archive.join("00000000.age"), archive.join("00000001.age"))?;
        replace_manifest(&archive, &identity, &value)?;
        sparc::verify(&archive, &identity)?;
        let destination = work.path().join("restored");
        let result = sparc::unpack(&archive, &destination, &identity);
        if names_collide {
            assert!(result.is_err());
            assert_eq!(fs::read_dir(&destination)?.count(), 1);
        } else {
            result?;
            assert_eq!(fs::read_dir(&destination)?.count(), 2);
        }
        assert_eq!(
            fs::read(destination.join(first))?,
            b"synthetic confidential contents"
        );
    }
    Ok(())
}

#[test]
fn archive_does_not_contain_plaintext_names_or_contents() -> Result<()> {
    let (work, _, _) = sample()?;
    for entry in fs::read_dir(work.path().join("archive"))? {
        let bytes = fs::read(entry?.path())?;
        for secret in [
            b"data.txt".as_slice(),
            b"synthetic confidential contents".as_slice(),
        ] {
            assert!(!bytes.windows(secret.len()).any(|window| window == secret));
        }
    }
    Ok(())
}
