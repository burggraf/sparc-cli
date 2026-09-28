package database

import (
	"context"
	"io"

	"github.com/burggraf/sparc-cli/internal/archive"
	"github.com/burggraf/sparc-cli/internal/tools"
)

func restoreSplitWith(ctx context.Context, request RestoreRequest, ops recoveryOps) error {
	if ctx == nil || !validRecoveryConnection(request.Target, request.TargetPassword) || !validRecoveryArchivePath(request.ArchivePath) ||
		archive.ValidatePassphrase(request.ArchivePassphrase) != nil || !validEmptyTargetScope(request.EmptyScope) ||
		!sameRecoveryStrings(request.EmptyScope.RequiredPresent, []string{"public"}) || len(request.EmptyScope.RequiredAbsent) != 0 ||
		ops.verify == nil || ops.rootCert == nil || ops.observe == nil || ops.prepareTool == nil || ops.open == nil || ops.run == nil {
		return ErrRestore
	}
	manifest, err := ops.verify(request.ArchivePath, request.ArchivePassphrase)
	if err != nil || !validSplitCaptureManifest(manifest) {
		return ErrRestore
	}
	profileReader, err := ops.open(request.ArchivePath, manifest.Components[2], request.ArchivePassphrase)
	if err != nil || profileReader == nil {
		return ErrRestore
	}
	profileBytes, readErr := io.ReadAll(io.LimitReader(profileReader, maxRecoveryProfileBytes+1))
	closeErr := profileReader.Close()
	if readErr != nil || closeErr != nil || len(profileBytes) > maxRecoveryProfileBytes {
		return ErrRestore
	}
	profile, err := parseRecoveryProfileV2(profileBytes)
	clear(profileBytes)
	if err != nil || profile.SourceProjectRef == request.Target.ExpectedProjectRef {
		return ErrRestore
	}
	if ops.prepareTool(ctx, tools.PSQL) != nil {
		return ErrRestore
	}
	rootCert, err := ops.rootCert(request.Target)
	if err != nil || len(rootCert) == 0 || len(rootCert) > maxNativeRootBytes {
		return ErrRestore
	}
	observation, err := ops.observe(ctx, request.Target, request.TargetPassword, profile.DataDump.Schemas)
	if err != nil || observation.ServerMajor != supportedPostgresMajor || !observation.TLS || !observation.ReadOnly ||
		CheckEmptyTargetV1(observation, request.EmptyScope) != nil || !splitTargetSchemasPresent(observation.Schemas, profile.PublicSchemaOwner) || !sameProfileExtensions(profile.Extensions, observation.Extensions) {
		return ErrRestore
	}
	schema, err := ops.open(request.ArchivePath, manifest.Components[0], request.ArchivePassphrase)
	if err != nil || schema == nil {
		return ErrRestore
	}
	data, err := ops.open(request.ArchivePath, manifest.Components[1], request.ArchivePassphrase)
	if err != nil || data == nil {
		_ = schema.Close()
		return ErrRestore
	}
	connection := &tools.PGConnection{
		Host: request.Target.Host, Port: request.Target.Port, User: request.Target.User, Database: request.Target.Database,
		Password: request.TargetPassword, RootCertPEM: rootCert,
	}
	return restoreSchemaDataAndVerifyWith(ctx, ops.run, connection, schema, data, uint64(manifest.Components[0].Length), uint64(manifest.Components[1].Length), profile.TableFingerprints)
}

func splitTargetSchemasPresent(schemas []SchemaObservation, publicOwner string) bool {
	if len(schemas) != 3 {
		return false
	}
	for index, name := range []string{"public", "auth", "storage"} {
		if schemas[index].Name != name || !schemas[index].Present || index == 0 && schemas[index].Owner != publicOwner {
			return false
		}
	}
	return true
}

func sameProfileExtensions(expected []recoveryProfileExtension, actual []ExtensionObservation) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index, extension := range expected {
		if extension.Name != actual[index].Name || extension.Version != actual[index].Version || extension.Schema != actual[index].Schema {
			return false
		}
	}
	return true
}
