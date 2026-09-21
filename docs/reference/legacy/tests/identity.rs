use std::fs;

use anyhow::Result;

#[test]
fn identity_file_round_trips_without_exposing_the_secret() -> Result<()> {
    let work = tempfile::tempdir()?;
    let path = work.path().join("recovery.agekey");

    let recipient = sparc::create_identity_file(&path)?;
    let identity = sparc::read_identity_file(&path)?;

    assert_eq!(identity.to_public().to_string(), recipient.to_string());
    assert!(recipient.to_string().starts_with("age1"));
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        assert_eq!(fs::metadata(path)?.permissions().mode() & 0o777, 0o600);
    }
    Ok(())
}

#[test]
fn identity_file_refuses_overwrite_and_unsafe_inputs() -> Result<()> {
    let work = tempfile::tempdir()?;
    let path = work.path().join("recovery.agekey");
    sparc::create_identity_file(&path)?;
    let original = fs::read(&path)?;

    assert!(sparc::create_identity_file(&path).is_err());
    assert_eq!(fs::read(&path)?, original);

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&path, fs::Permissions::from_mode(0o644))?;
        assert!(sparc::read_identity_file(&path).is_err());
        fs::set_permissions(&path, fs::Permissions::from_mode(0o600))?;
    }

    let malformed = work.path().join("malformed.agekey");
    fs::write(&malformed, "PRIVATE-INVALID-KEY")?;
    assert!(sparc::read_identity_file(&malformed).is_err());

    let multiple = work.path().join("multiple.agekey");
    let key = String::from_utf8(original)?;
    fs::write(&multiple, format!("{key}\n{key}"))?;
    assert!(sparc::read_identity_file(&multiple).is_err());

    let oversized = work.path().join("oversized.agekey");
    fs::write(&oversized, vec![b'x'; 16_385])?;
    assert!(sparc::read_identity_file(&oversized).is_err());
    Ok(())
}
