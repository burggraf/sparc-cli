// Package config loads bounded, non-secret v1 profiles. References are validated
// but never resolved while loading configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/burggraf/sparc-cli/internal/credentials"
	"github.com/burggraf/sparc-cli/internal/platform"
)

type Config struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

type Profile struct {
	Name            string                 `json:"name"`
	ProjectRef      string                 `json:"project_ref,omitempty"`
	Destination     string                 `json:"destination,omitempty"`
	ManagementToken *credentials.Reference `json:"management_token,omitempty"`
}

var ErrConfig = errors.New("invalid configuration")

func Load(path string) (Config, error) {
	data, err := platform.ReadPrivateFile(path, 64*1024)
	if err != nil || !utf8.Valid(data) || !json.Valid(data) || !pairedSurrogates(data) {
		return Config{}, ErrConfig
	}
	p := parser{decoder: json.NewDecoder(bytes.NewReader(data))}
	p.decoder.UseNumber()
	var result Config
	keys, err := p.object(func(key string) error {
		switch key {
		case "version":
			token, err := p.token()
			if err != nil || token != json.Number("1") {
				return ErrConfig
			}
			result.Version = 1
		case "profiles":
			if p.delimiter('[') != nil {
				return ErrConfig
			}
			result.Profiles = make([]Profile, 0)
			names := make(map[string]bool)
			for p.decoder.More() {
				if len(result.Profiles) == 32 {
					return ErrConfig
				}
				profile, err := p.profile()
				if err != nil || names[profile.Name] {
					return ErrConfig
				}
				names[profile.Name] = true
				result.Profiles = append(result.Profiles, profile)
			}
			return p.delimiter(']')
		default:
			return ErrConfig
		}
		return nil
	})
	if err != nil || !keys["version"] || !keys["profiles"] {
		return Config{}, ErrConfig
	}
	if _, err := p.decoder.Token(); err != io.EOF {
		return Config{}, ErrConfig
	}
	return result, nil
}

type parser struct {
	decoder *json.Decoder
	depth   int
}

func (p *parser) token() (json.Token, error) {
	token, err := p.decoder.Token()
	if err != nil {
		return nil, ErrConfig
	}
	if delimiter, ok := token.(json.Delim); ok {
		if delimiter == '{' || delimiter == '[' {
			p.depth++
		} else {
			p.depth--
		}
		if p.depth < 0 || p.depth > 8 {
			return nil, ErrConfig
		}
	}
	return token, nil
}

func (p *parser) delimiter(want json.Delim) error {
	token, err := p.token()
	if err != nil || token != want {
		return ErrConfig
	}
	return nil
}

func (p *parser) object(field func(string) error) (map[string]bool, error) {
	if p.delimiter('{') != nil {
		return nil, ErrConfig
	}
	keys := make(map[string]bool)
	for p.decoder.More() {
		token, err := p.token()
		key, ok := token.(string)
		if err != nil || !ok || keys[key] {
			return nil, ErrConfig
		}
		keys[key] = true
		if field(key) != nil {
			return nil, ErrConfig
		}
	}
	return keys, p.delimiter('}')
}

func (p *parser) text(limit int) (string, error) {
	token, err := p.token()
	value, ok := token.(string)
	if err != nil || !ok || len(value) > limit || strings.TrimSpace(value) == "" {
		return "", ErrConfig
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", ErrConfig
		}
	}
	return value, nil
}

func (p *parser) profile() (Profile, error) {
	var result Profile
	keys, err := p.object(func(key string) error {
		var err error
		switch key {
		case "name":
			result.Name, err = p.text(256)
		case "project_ref":
			result.ProjectRef, err = p.text(256)
		case "destination":
			result.Destination, err = p.text(4096)
			if err == nil && (strings.Contains(result.Destination, "://") || strings.HasPrefix(result.Destination, "//")) {
				destination, e := url.Parse(result.Destination)
				if e != nil || destination.User != nil {
					return ErrConfig
				}
			}
		case "management_token":
			result.ManagementToken, err = p.reference()
		default:
			return ErrConfig
		}
		return err
	})
	if err != nil || !keys["name"] {
		return Profile{}, ErrConfig
	}
	return result, nil
}

func (p *parser) reference() (*credentials.Reference, error) {
	var result credentials.Reference
	keys, err := p.object(func(key string) error {
		var err error
		switch key {
		case "env":
			result.Env, err = p.text(128)
		case "file":
			result.File, err = p.text(4096)
		default:
			return ErrConfig
		}
		return err
	})
	if err != nil || len(keys) != 1 || result.Validate() != nil {
		return nil, ErrConfig
	}
	return &result, nil
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Reject them before
// decoding so a malformed identifier cannot silently change identity. JSON syntax
// is already checked; skipping escaped backslashes preserves literal "\\uD800".
func pairedSurrogates(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, _ := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
