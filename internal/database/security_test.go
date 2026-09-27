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

func TestCheckEmptyTargetV1AcceptsEmptyDeclaredScope(t *testing.T) {
	observation := CatalogObservation{
		ServerMajor: supportedPostgresMajor,
		TLS:         true,
		ReadOnly:    true,
		Schemas: []SchemaObservation{
			{Name: "public", Present: true},
			{Name: "app", Present: false},
		},
		Security: SecurityObservation{Observed: true},
	}
	scope := EmptyTargetScopeV1{RequiredPresent: []string{"public"}, RequiredAbsent: []string{"app"}}
	if err := CheckEmptyTargetV1(observation, scope); err != nil {
		t.Fatalf("empty scope = %v", err)
	}
}

func TestCheckEmptyTargetV1RefusesInvalidScope(t *testing.T) {
	observation := CatalogObservation{
		ServerMajor: supportedPostgresMajor,
		TLS:         true,
		ReadOnly:    true,
		Schemas:     []SchemaObservation{{Name: "public", Present: true}},
		Security:    SecurityObservation{Observed: true},
	}
	for name, scope := range map[string]EmptyTargetScopeV1{
		"missing required present schema": {},
		"duplicate schema":                {RequiredPresent: []string{"public", "public"}},
		"overlapping schema":              {RequiredPresent: []string{"public"}, RequiredAbsent: []string{"public"}},
		"control character":               {RequiredPresent: []string{"public\n"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckEmptyTargetV1(observation, scope); !errors.Is(err, ErrTargetSecurityUnknown) {
				t.Fatalf("invalid scope error = %v, want unknown-profile refusal", err)
			}
		})
	}
}

func TestCheckEmptyTargetV1RejectsObservedApplicationState(t *testing.T) {
	scope := EmptyTargetScopeV1{RequiredPresent: []string{"public"}, RequiredAbsent: []string{"app"}}
	base := CatalogObservation{
		ServerMajor: supportedPostgresMajor,
		TLS:         true,
		ReadOnly:    true,
		Schemas: []SchemaObservation{
			{Name: "public", Present: true},
			{Name: "app", Present: false},
		},
		Security: SecurityObservation{Observed: true},
	}
	for name, observation := range map[string]CatalogObservation{
		"missing required schema": func() CatalogObservation { o := base; o.Schemas[0].Present = false; return o }(),
		"present custom schema":   func() CatalogObservation { o := base; o.Schemas[1].Present = true; return o }(),
		"public relation": func() CatalogObservation {
			o := base
			o.Relations = []RelationObservation{{Schema: "public", Name: "profiles"}}
			return o
		}(),
		"public routine": func() CatalogObservation {
			o := base
			o.Routines = []RoutineObservation{{Schema: "public", Name: "current_user"}}
			return o
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckEmptyTargetV1(observation, scope); !errors.Is(err, ErrUnsafeTargetSecurityProfile) {
				t.Fatalf("nonempty target error = %v, want unsafe-profile refusal", err)
			}
		})
	}
}

func TestCheckEmptyTargetV1RefusesMalformedObservation(t *testing.T) {
	scope := EmptyTargetScopeV1{RequiredPresent: []string{"public"}, RequiredAbsent: []string{"app"}}
	base := CatalogObservation{
		ServerMajor: supportedPostgresMajor,
		TLS:         true,
		ReadOnly:    true,
		Schemas: []SchemaObservation{
			{Name: "public", Present: true},
			{Name: "app", Present: false},
		},
		Security: SecurityObservation{Observed: true},
	}
	for name, observation := range map[string]CatalogObservation{
		"missing app observation": func() CatalogObservation { o := base; o.Schemas = o.Schemas[:1]; return o }(),
		"duplicate public observation": func() CatalogObservation {
			o := base
			o.Schemas = append(o.Schemas, SchemaObservation{Name: "public", Present: true})
			return o
		}(),
		"invalid relation identity": func() CatalogObservation {
			o := base
			o.Relations = []RelationObservation{{Schema: "public\n", Name: "profiles"}}
			return o
		}(),
		"invalid routine identity": func() CatalogObservation {
			o := base
			o.Routines = []RoutineObservation{{Schema: "public", Name: "current\nuser"}}
			return o
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckEmptyTargetV1(observation, scope); !errors.Is(err, ErrTargetSecurityUnknown) {
				t.Fatalf("malformed observation error = %v, want unknown-profile refusal", err)
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
