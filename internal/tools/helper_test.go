package tools

import (
	"bytes"
	"context"
	"encoding/gob"
	"io"
	"os"
	"testing"

	"github.com/burggraf/sparc-cli/internal/platform"
)

func TestExtractHelperProcess(t *testing.T) {
	if os.Getenv("SPARC_EXTRACT_HELPER") != "1" {
		t.Skip("helper process")
	}
	var gate [1]byte
	if _, err := io.ReadFull(os.Stdin, gate[:]); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := platform.ReadPrivateFile(os.Getenv("SPARC_EXTRACT_MANIFEST"), 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	var manifest packageManifest
	if err := gob.NewDecoder(bytes.NewReader(manifestBytes)).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	archive, err := os.Open(os.Getenv("SPARC_EXTRACT_ARCHIVE"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	ops := defaultExtractionOps()
	mode := os.Getenv("SPARC_EXTRACT_KILL_GATE")
	if mode != "" {
		publish := ops.publish
		ops.publish = func(staging, destination string) (bool, error) {
			if mode == "before" {
				if _, err := os.Stdout.Write([]byte{'B'}); err != nil {
					return false, err
				}
				if _, err := io.ReadFull(os.Stdin, gate[:]); err != nil {
					return false, err
				}
			}
			published, err := publish(staging, destination)
			if mode == "after" && err == nil && published {
				if _, signalErr := os.Stdout.Write([]byte{'A'}); signalErr != nil {
					return false, signalErr
				}
				if _, signalErr := io.ReadFull(os.Stdin, gate[:]); signalErr != nil {
					return false, signalErr
				}
			}
			return published, err
		}
	}
	path, err := preparePayloadWith(context.Background(), os.Getenv("SPARC_EXTRACT_ROOT"), archive, manifest, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePayloadPackage(context.Background(), path, manifest); err != nil {
		t.Fatal(err)
	}
}
