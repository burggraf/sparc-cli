package archive

import (
	"errors"
	"io"
	"unicode"
	"unicode/utf8"

	"filippo.io/age"
)

const maxPassphraseLength = 4096

func Encrypt(dst io.Writer, src io.Reader, passphrase string) error {
	if err := validatePassphrase(passphrase); err != nil {
		return err
	}
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	writer, err := age.Encrypt(dst, recipient)
	if err != nil {
		return err
	}
	if _, err := io.Copy(writer, src); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func Decrypt(src io.Reader, passphrase string) (io.Reader, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	return age.Decrypt(src, identity)
}

func validatePassphrase(passphrase string) error {
	if passphrase == "" || len(passphrase) > maxPassphraseLength || !utf8.ValidString(passphrase) {
		return errors.New("invalid archive passphrase")
	}
	for _, r := range passphrase {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return errors.New("invalid archive passphrase")
		}
	}
	return nil
}
