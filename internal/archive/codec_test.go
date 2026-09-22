package archive

import (
	"bytes"
	"io"
	"testing"

	"filippo.io/age"
)

func TestPassphraseCodecStreamsZeroSmallMultipleAndLargeArtifacts(t *testing.T) {
	for _, size := range []int64{0, 1, 8193, 8 << 20} {
		t.Run("size", func(t *testing.T) {
			var encrypted bytes.Buffer
			if err := Encrypt(&encrypted, &zeroReader{remaining: size}, "correct horse battery staple"); err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}
			plain, err := Decrypt(bytes.NewReader(encrypted.Bytes()), "correct horse battery staple")
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			got, err := io.Copy(io.Discard, plain)
			if err != nil {
				t.Fatalf("Copy() error = %v", err)
			}
			if got != size {
				t.Fatalf("plaintext size = %d, want %d", got, size)
			}
		})
	}
}

func TestPassphraseCodecRejectsUnsafePassphrase(t *testing.T) {
	for _, passphrase := range []string{"", "line\nbreak", string([]byte{0xff})} {
		if err := Encrypt(io.Discard, stringsReader("secret"), passphrase); err == nil {
			t.Fatalf("Encrypt() accepted unsafe passphrase %q", passphrase)
		}
	}
}

func TestPassphraseCodecInteroperatesWithAge(t *testing.T) {
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, stringsReader("interoperable"), "passphrase"); err != nil {
		t.Fatal(err)
	}
	identity, err := age.NewScryptIdentity("passphrase")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := age.Decrypt(bytes.NewReader(encrypted.Bytes()), identity)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(plain)
	if err != nil || string(got) != "interoperable" {
		t.Fatalf("age decrypt = %q, %v", got, err)
	}
}

func TestPassphraseCodecRejectsWrongPassphrase(t *testing.T) {
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, stringsReader("secret"), "right"); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(bytes.NewReader(encrypted.Bytes()), "wrong"); err == nil {
		t.Fatal("Decrypt() accepted wrong passphrase")
	}
}

func TestPassphraseCodecRejectsTruncatedAndCorruptCiphertext(t *testing.T) {
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, stringsReader("secret"), "passphrase"); err != nil {
		t.Fatal(err)
	}
	for _, ciphertext := range [][]byte{encrypted.Bytes()[:len(encrypted.Bytes())-1], corrupt(encrypted.Bytes())} {
		plain, err := Decrypt(bytes.NewReader(ciphertext), "passphrase")
		if err == nil {
			_, err = io.Copy(io.Discard, plain)
		}
		if err == nil {
			t.Fatal("Decrypt() accepted corrupt ciphertext")
		}
	}
}

func TestEncryptReturnsFinalizationWriteError(t *testing.T) {
	if err := Encrypt(failingWriter{}, stringsReader("secret"), "passphrase"); err == nil {
		t.Fatal("Encrypt() accepted failing destination")
	}
}

func corrupt(ciphertext []byte) []byte {
	out := append([]byte(nil), ciphertext...)
	out[len(out)-1] ^= 1
	return out
}

func stringsReader(s string) io.Reader { return bytes.NewBufferString(s) }

type zeroReader struct{ remaining int64 }

func (r *zeroReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = 0
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func BenchmarkEncryptLargeStream(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		if err := Encrypt(io.Discard, &zeroReader{remaining: 8 << 20}, "benchmark passphrase"); err != nil {
			b.Fatal(err)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }
