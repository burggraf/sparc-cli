use std::{env, path::Path, process::ExitCode};

use age::x25519::Recipient;
use anyhow::{Context, Result, anyhow, bail};

const HELP: &str = "SPARC local encrypted archive prototype — not a Supabase backup tool.

Usage:
  sparc keygen IDENTITY_FILE
  sparc pack SOURCE_DIR ARCHIVE_DIR RECIPIENT
  sparc verify ARCHIVE_DIR IDENTITY_FILE
  sparc unpack ARCHIVE_DIR DESTINATION_DIR IDENTITY_FILE
  sparc plan-database INPUT.json

Plan-database reads only non-secret declarations and prints an offline JSON plan.
Success means valid input, not export readiness or a verified restore.
All output destinations must be new; existing directories are never merged.
Keep the unencrypted recovery identity outside the source, archive, and Git.
Pack a quiet folder of synthetic artifacts first. Unpack never executes files.
Partial output may remain after failure; retry in a new destination.
";

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            // Print only the safe outer message, not parser/debug chains containing supplied data.
            eprintln!("sparc: {error}");
            ExitCode::FAILURE
        }
    }
}

fn run() -> Result<()> {
    let args: Vec<_> = env::args_os().skip(1).collect();
    match args.as_slice() {
        [flag] if flag == "--help" || flag == "-h" => println!("{HELP}"),
        [command, input] if command == "plan-database" => {
            let plan = sparc::database::plan_database_file(Path::new(input))?;
            serde_json::to_writer_pretty(std::io::stdout().lock(), &plan)
                .context("cannot write database plan")?;
            println!();
        }
        [command, path] if command == "keygen" => {
            let recipient = sparc::create_identity_file(Path::new(path))?;
            eprintln!(
                "Recovery key saved UNENCRYPTED. Protect it separately; losing it prevents recovery."
            );
            println!("{recipient}");
        }
        [command, source, archive, recipient] if command == "pack" => {
            let recipient: Recipient = recipient
                .to_str()
                .context("invalid age recipient")?
                .parse()
                .map_err(|_| anyhow!("invalid age recipient; supply the public age1 key"))?;
            let manifest = sparc::pack(Path::new(source), Path::new(archive), &recipient)?;
            summary(
                "Local archive created (run verify before relying on it)",
                &manifest,
            );
        }
        [command, archive, identity] if command == "verify" => {
            let identity = sparc::read_identity_file(Path::new(identity))?;
            let manifest = sparc::verify(Path::new(archive), &identity)?;
            summary("Local archive verified", &manifest);
        }
        [command, archive, destination, identity] if command == "unpack" => {
            let identity = sparc::read_identity_file(Path::new(identity))?;
            let manifest = sparc::unpack(Path::new(archive), Path::new(destination), &identity)?;
            summary(
                "Local files restored; no Supabase project was modified",
                &manifest,
            );
        }
        _ => bail!("invalid arguments; run sparc --help"),
    }
    Ok(())
}

fn summary(action: &str, manifest: &sparc::Manifest) {
    let bytes: u64 = manifest
        .artifacts
        .iter()
        .map(|artifact| artifact.bytes)
        .sum();
    println!(
        "{action}: {} files, {} directories, {bytes} plaintext bytes.",
        manifest.artifacts.len(),
        manifest.directories.len()
    );
}
