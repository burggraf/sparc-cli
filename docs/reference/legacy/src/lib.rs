//! Experimental local artifact packaging, not a Supabase backup implementation.

pub mod database;

use std::{
    collections::BTreeSet,
    fs::{self, DirBuilder, File, OpenOptions},
    io::{self, BufReader, Read, Write},
    iter,
    path::{Path, PathBuf},
};

use age::{
    secrecy::{ExposeSecret, SecretString},
    x25519::{Identity, Recipient},
};
use anyhow::{Context, Result, bail, ensure};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tempfile::NamedTempFile;

const FORMAT: &str = "sparc-local-artifacts";
const MAX_MANIFEST: usize = 16 * 1024 * 1024;
const MAX_ENTRIES: usize = 100_000;
const MAX_BYTES: u64 = 128 * 1024 * 1024 * 1024;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Manifest {
    pub format: String,
    pub version: u32,
    pub directories: Vec<String>,
    pub artifacts: Vec<Artifact>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Artifact {
    pub id: u32,
    pub path: String,
    pub bytes: u64,
    pub sha256: String,
}

/// Create a new unencrypted native age identity file with private permissions.
pub fn create_identity_file(path: &Path) -> Result<Recipient> {
    let parent = path
        .parent()
        .filter(|path| !path.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    let identity = Identity::generate();
    let recipient = identity.to_public();
    let mut output = NamedTempFile::new_in(parent)
        .context("recovery key parent directory must exist and be writable")?;
    writeln!(
        output,
        "# SPARC recovery identity — keep private and separate\n# public key: {recipient}\n{}",
        identity.to_string().expose_secret()
    )?;
    output.as_file().sync_all()?;
    output
        .persist_noclobber(path)
        .map_err(io::Error::from)
        .context("could not save recovery key; use a new file in a writable directory")?;
    Ok(recipient)
}

/// Read one private native age identity from a private regular file.
pub fn read_identity_file(path: &Path) -> Result<Identity> {
    let metadata = fs::symlink_metadata(path).context("cannot read recovery identity file")?;
    ensure!(
        metadata.is_file(),
        "recovery identity must be a regular file, not a symlink"
    );
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        ensure!(
            metadata.permissions().mode() & 0o077 == 0,
            "recovery identity is not private; restrict its permissions to 0600"
        );
    }
    let mut content = String::new();
    File::open(path)?
        .take(16_385)
        .read_to_string(&mut content)
        .context("cannot read a UTF-8 recovery identity")?;
    let secret = SecretString::from(content);
    ensure!(
        secret.expose_secret().len() <= 16_384,
        "recovery identity file is too large"
    );
    let mut lines = secret
        .expose_secret()
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with('#'));
    let line = lines.next().context("recovery identity file is empty")?;
    ensure!(
        lines.next().is_none(),
        "expected exactly one native age recovery identity"
    );
    line.parse()
        .map_err(|_| anyhow::anyhow!("invalid native age recovery identity"))
}

/// Encrypt a stable local directory into a new archive directory.
/// On failure, a partial ciphertext directory may remain without a manifest.
pub fn pack(source: &Path, archive: &Path, recipient: &Recipient) -> Result<Manifest> {
    require_directory(source)?;
    let source = fs::canonicalize(source)?;
    let archive = new_destination(archive)?;
    ensure!(
        !archive.starts_with(&source),
        "archive must be outside the source folder"
    );
    let mut manifest = Manifest {
        format: FORMAT.into(),
        version: 1,
        directories: Vec::new(),
        artifacts: Vec::new(),
    };
    collect(&source, &source, &mut manifest, &mut 0, &mut 0)?;
    manifest.directories.sort();
    manifest.artifacts.sort_by(|a, b| a.path.cmp(&b.path));
    for (index, artifact) in manifest.artifacts.iter_mut().enumerate() {
        artifact.id = index as u32;
    }
    validate_manifest(&manifest)?;
    // ponytail: bounded in-memory inventory; use an encrypted JSONL index if object counts demand it.
    ensure!(
        serde_json::to_vec(&manifest)?.len() <= MAX_MANIFEST,
        "manifest exceeds size limit"
    );
    private_directory(&archive)?;

    // ponytail: full-file, sequential transfers; add checkpointed chunks after live recovery is proven.
    for artifact in &mut manifest.artifacts {
        let path = source.join(&artifact.path);
        let mut input = regular_file(&path)?;
        let before = input.metadata()?;
        ensure!(
            before.len() == artifact.bytes,
            "source changed during packing"
        );
        let mut output = private_file(&archive.join(payload_name(artifact.id)))?;
        let (bytes, hash) = encrypt(&mut input, &mut output, recipient, artifact.bytes)?;
        let after = fs::symlink_metadata(&path)?;
        ensure!(
            after.is_file()
                && bytes == before.len()
                && after.len() == before.len()
                && after.modified()? == before.modified()?,
            "source changed during packing"
        );
        output.sync_all()?;
        artifact.sha256 = hash;
    }
    let bytes = serde_json::to_vec(&manifest)?;
    let mut final_manifest = NamedTempFile::new_in(&archive)?;
    encrypt(
        bytes.as_slice(),
        final_manifest.as_file_mut(),
        recipient,
        MAX_MANIFEST as u64,
    )?;
    final_manifest.as_file().sync_all()?;
    final_manifest
        .persist_noclobber(archive.join("manifest.age"))
        .map_err(io::Error::from)?;
    Ok(manifest)
}

/// Verify the complete manifest and every payload, without writing plaintext to disk.
pub fn verify(archive: &Path, identity: &Identity) -> Result<Manifest> {
    let manifest = read_manifest(archive, identity)?;
    for artifact in &manifest.artifacts {
        check_payload(archive, artifact, identity, io::sink())?;
    }
    Ok(manifest)
}

/// Restore supported local files into a new private directory, without executing them.
/// A failed restore may leave verified files in an incomplete output directory.
pub fn unpack(archive: &Path, destination: &Path, identity: &Identity) -> Result<Manifest> {
    let manifest = read_manifest(archive, identity)?;
    let destination = new_destination(destination)?;
    ensure!(
        !destination.starts_with(fs::canonicalize(archive)?),
        "restore destination must be outside the archive"
    );
    private_directory(&destination)?;
    let mut directories: Vec<_> = manifest.directories.iter().collect();
    directories.sort_by_key(|path| path.split('/').count());
    for directory in directories {
        private_directory(&destination.join(directory))
            .context("cannot create restored directory; possible filename collision")?;
    }
    for artifact in &manifest.artifacts {
        let path = destination.join(&artifact.path);
        let mut output = NamedTempFile::new_in(path.parent().context("missing file parent")?)?;
        check_payload(archive, artifact, identity, output.as_file_mut())?;
        output.as_file().sync_all()?;
        output
            .persist_noclobber(&path)
            // PersistError owns the temp file; converting it drops plaintext before returning an error.
            .map_err(io::Error::from)
            .context("cannot publish restored file; destination may have a filename collision")?;
    }
    Ok(manifest)
}

fn collect(
    root: &Path,
    directory: &Path,
    manifest: &mut Manifest,
    total_bytes: &mut u64,
    inventory_bytes: &mut usize,
) -> Result<()> {
    for entry in fs::read_dir(directory)? {
        let entry = entry?;
        let path = entry.path();
        let relative = path
            .strip_prefix(root)?
            .to_str()
            .context("non-UTF-8 source path")?
            .to_owned();
        validate_path(&relative)?;
        ensure!(
            manifest.directories.len() + manifest.artifacts.len() < MAX_ENTRIES,
            "too many archive entries"
        );
        // Account conservatively for JSON escaping and per-entry structure before allocating more inventory.
        *inventory_bytes += relative.len() * 2 + 256;
        ensure!(
            *inventory_bytes <= MAX_MANIFEST,
            "manifest inventory exceeds size limit"
        );
        let metadata = fs::symlink_metadata(&path)?;
        if metadata.is_dir() {
            manifest.directories.push(relative);
            collect(root, &path, manifest, total_bytes, inventory_bytes)?;
        } else {
            ensure!(
                metadata.is_file(),
                "source contains a symlink or unsupported special file"
            );
            *total_bytes = total_bytes
                .checked_add(metadata.len())
                .context("archive size overflow")?;
            ensure!(
                *total_bytes <= MAX_BYTES,
                "archive exceeds prototype plaintext size limit"
            );
            manifest.artifacts.push(Artifact {
                id: 0,
                path: relative,
                bytes: metadata.len(),
                sha256: "0".repeat(64),
            });
        }
    }
    Ok(())
}

fn read_manifest(archive: &Path, identity: &Identity) -> Result<Manifest> {
    require_directory(archive)?;
    let file = regular_file(&archive.join("manifest.age"))
        .context("archive has no readable manifest; it may be incomplete")?;
    ensure!(
        file.metadata()?.len() <= (MAX_MANIFEST * 2) as u64,
        "encrypted manifest exceeds size limit"
    );
    let mut plaintext = Vec::new();
    decrypt(file, identity)?
        .take((MAX_MANIFEST + 1) as u64)
        .read_to_end(&mut plaintext)?;
    ensure!(
        plaintext.len() <= MAX_MANIFEST,
        "manifest exceeds size limit"
    );
    let manifest: Manifest =
        serde_json::from_slice(&plaintext).context("invalid archive manifest")?;
    validate_manifest(&manifest)?;
    let mut expected: BTreeSet<_> = manifest
        .artifacts
        .iter()
        .map(|a| payload_name(a.id))
        .collect();
    expected.insert("manifest.age".into());
    for entry in fs::read_dir(archive)? {
        let entry = entry?;
        let name = entry
            .file_name()
            .into_string()
            .map_err(|_| anyhow::anyhow!("invalid archive filename"))?;
        ensure!(
            expected.remove(&name),
            "archive contains an unlisted file or directory"
        );
        ensure!(
            entry.file_type()?.is_file(),
            "archive payload is not a regular file"
        );
    }
    ensure!(expected.is_empty(), "archive is missing a payload");
    Ok(manifest)
}

fn validate_manifest(manifest: &Manifest) -> Result<()> {
    ensure!(
        manifest.format == FORMAT && manifest.version == 1,
        "unsupported archive format or version"
    );
    ensure!(
        manifest.directories.len() + manifest.artifacts.len() <= MAX_ENTRIES,
        "too many archive entries"
    );
    let mut paths = BTreeSet::new();
    let directories: BTreeSet<_> = manifest.directories.iter().map(String::as_str).collect();
    let mut total = 0_u64;
    for path in &manifest.directories {
        validate_path(path)?;
        ensure!(paths.insert(path.as_str()), "duplicate archive path");
    }
    for (index, artifact) in manifest.artifacts.iter().enumerate() {
        validate_path(&artifact.path)?;
        ensure!(
            artifact.id as usize == index,
            "invalid or duplicate artifact ID"
        );
        ensure!(
            paths.insert(artifact.path.as_str()),
            "duplicate or conflicting archive path"
        );
        ensure!(
            artifact.sha256.len() == 64
                && artifact
                    .sha256
                    .bytes()
                    .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)),
            "invalid artifact checksum"
        );
        total = total
            .checked_add(artifact.bytes)
            .context("archive size overflow")?;
        ensure!(
            total <= MAX_BYTES,
            "archive exceeds prototype plaintext size limit"
        );
    }
    for path in paths {
        let mut current = path;
        while let Some((parent, _)) = current.rsplit_once('/') {
            ensure!(
                directories.contains(parent),
                "archive is missing a parent directory"
            );
            current = parent;
        }
    }
    Ok(())
}

fn validate_path(path: &str) -> Result<()> {
    ensure!(
        !path.is_empty()
            && path.len() <= 4096
            && !path
                .chars()
                .any(|c| c.is_control() || c == '\\' || c == ':')
            && path.split('/').count() <= 64
            && path
                .split('/')
                .all(|part| !part.is_empty() && part != "." && part != ".."),
        "unsafe or unsupported relative archive path"
    );
    Ok(())
}

fn check_payload(
    archive: &Path,
    artifact: &Artifact,
    identity: &Identity,
    output: impl Write,
) -> Result<()> {
    let file = regular_file(&archive.join(payload_name(artifact.id)))?;
    let (bytes, hash) = copy_hash(decrypt(file, identity)?, output, artifact.bytes)?;
    ensure!(
        bytes == artifact.bytes && hash == artifact.sha256,
        "payload size or checksum mismatch"
    );
    Ok(())
}

fn encrypt(
    input: impl Read,
    output: impl Write,
    recipient: &Recipient,
    limit: u64,
) -> Result<(u64, String)> {
    let encryptor = age::Encryptor::with_recipients(iter::once(recipient as &dyn age::Recipient))?;
    let mut writer = encryptor.wrap_output(output)?;
    let result = copy_hash(input, &mut writer, limit)?;
    writer.finish()?;
    Ok(result)
}

fn decrypt(file: File, identity: &Identity) -> Result<impl Read> {
    Ok(age::Decryptor::new_buffered(BufReader::new(file))?
        .decrypt(iter::once(identity as &dyn age::Identity))?)
}

fn copy_hash(input: impl Read, mut output: impl Write, limit: u64) -> Result<(u64, String)> {
    let mut input = input.take(limit.checked_add(1).context("invalid byte limit")?);
    let mut buffer = [0_u8; 64 * 1024];
    let mut hash = Sha256::new();
    let mut bytes = 0_u64;
    loop {
        let count = match input.read(&mut buffer) {
            Err(error) if error.kind() == io::ErrorKind::Interrupted => continue,
            result => result?,
        };
        if count == 0 {
            break;
        }
        bytes += count as u64;
        ensure!(bytes <= limit, "payload exceeds its declared size");
        hash.update(&buffer[..count]);
        output.write_all(&buffer[..count])?;
    }
    Ok((bytes, format!("{:x}", hash.finalize())))
}

fn payload_name(id: u32) -> String {
    format!("{id:08x}.age")
}

fn require_directory(path: &Path) -> Result<()> {
    ensure!(
        fs::symlink_metadata(path)?.is_dir(),
        "expected a directory, not a symlink or file"
    );
    Ok(())
}

fn regular_file(path: &Path) -> Result<File> {
    ensure!(
        fs::symlink_metadata(path)?.is_file(),
        "expected a regular file, not a symlink or special file"
    );
    Ok(File::open(path)?)
}

fn new_destination(path: &Path) -> Result<PathBuf> {
    let name = path
        .file_name()
        .context("destination must name a new file or directory")?;
    let parent = path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .unwrap_or(Path::new("."));
    let path = fs::canonicalize(parent)
        .context("destination parent must already exist")?
        .join(name);
    match fs::symlink_metadata(&path) {
        Ok(_) => bail!("destination already exists; use a new destination"),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(path),
        Err(error) => Err(error.into()),
    }
}

fn private_directory(path: &Path) -> Result<()> {
    let mut builder = DirBuilder::new();
    #[cfg(unix)]
    {
        use std::os::unix::fs::DirBuilderExt;
        builder.mode(0o700);
    }
    builder.create(path)?;
    Ok(())
}

fn private_file(path: &Path) -> Result<File> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        options.mode(0o600);
    }
    Ok(options.open(path)?)
}
