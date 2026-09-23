package database

import (
	"errors"
	"testing"
)

func TestCheckTargetSecurityV1RefusesIncompleteObservations(t *testing.T) {
	base := CatalogObservation{
		ServerMajor: supportedPostgresMajor,
		TLS:         true,
		ReadOnly:    true,
		Schemas:     []SchemaObservation{{Name: "public", Present: true}},
		Security:    SecurityObservation{Observed: true},
	}
	for name, observation := range map[string]CatalogObservation{
		"missing security facts": func() CatalogObservation { o := base; o.Security.Observed = false; return o }(),
		"missing schemas":        func() CatalogObservation { o := base; o.Schemas = nil; return o }(),
		"wrong server major":     func() CatalogObservation { o := base; o.ServerMajor++; return o }(),
		"no TLS":                 func() CatalogObservation { o := base; o.TLS = false; return o }(),
		"not read only":          func() CatalogObservation { o := base; o.ReadOnly = false; return o }(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckTargetSecurityV1(observation); !errors.Is(err, ErrTargetSecurityUnknown) {
				t.Fatalf("profile check error = %v, want unknown-profile refusal", err)
			}
		})
	}
}

func TestCheckTargetSecurityV1RejectsPublicSelect(t *testing.T) {
	base := CatalogObservation{
		ServerMajor: supportedPostgresMajor,
		TLS:         true,
		ReadOnly:    true,
		Schemas:     []SchemaObservation{{Name: "public", Present: true}},
		Security:    SecurityObservation{Observed: true},
	}
	for name, security := range map[string]SecurityObservation{
		"global default": {Observed: true, PublicSelectDefaults: true},
		"relation grant": {Observed: true, PublicSelectObjects: true},
		"column grant":   {Observed: true, PublicSelectColumns: true},
	} {
		t.Run(name, func(t *testing.T) {
			observation := base
			observation.Security = security
			if err := CheckTargetSecurityV1(observation); !errors.Is(err, ErrUnsafeTargetSecurityProfile) {
				t.Fatalf("profile check error = %v, want unsafe-profile refusal", err)
			}
		})
	}
}
