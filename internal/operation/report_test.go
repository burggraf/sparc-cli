package operation

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func completeReport() Report {
	report := Report{
		SchemaVersion: "v1",
		Components: []ComponentReport{{
			ID:                         "database.public",
			Support:                    SupportRequired,
			Observation:                ObservationObserved,
			ReadPermission:             PermissionGranted,
			Capture:                    CaptureCaptured,
			ByteIntegrity:              IntegrityPassed,
			RecoverabilityPrerequisite: RecoveryReady,
			Restore:                    RestoreRestored,
			BehaviorVerification:       BehaviorPassed,
		}},
	}
	report.Summary = report.ExpectedSummary()
	return report
}

func TestReportValidateComplete(t *testing.T) {
	report := completeReport()
	if err := report.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := report.Summary, (ReportSummary{Coverage: CoverageComplete, Restore: RestoreSuccess}); got != want {
		t.Fatalf("Summary = %#v, want %#v", got, want)
	}
}

func TestReportValidateRejectsBlockingStatesClaimedComplete(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ComponentReport)
	}{
		{"unknown", func(component *ComponentReport) { component.Observation = ObservationUnknown }},
		{"permission_denied", func(component *ComponentReport) { component.ReadPermission = PermissionDenied }},
		{"unsupported", func(component *ComponentReport) { component.Capture = CaptureUnsupported }},
		{"unsupported support", func(component *ComponentReport) { component.Support = SupportUnsupported }},
		{"partially_captured", func(component *ComponentReport) { component.Capture = CapturePartiallyCaptured }},
		{"manual_required", func(component *ComponentReport) {
			component.RecoverabilityPrerequisite = RecoveryManualRequired
			component.ManualRequirementIDs = []string{"auth.smtp.password"}
		}},
		{"missing_critical_material", func(component *ComponentReport) {
			component.RecoverabilityPrerequisite = RecoveryMissingCriticalMaterial
		}},
		{"failed", func(component *ComponentReport) { component.Restore = RestoreFailed }},
		{"inconclusive", func(component *ComponentReport) { component.ByteIntegrity = IntegrityInconclusive }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := completeReport()
			test.edit(&report.Components[0])
			if err := report.Validate(); err == nil {
				t.Fatal("Validate() succeeded for a complete/success claim with a blocking state")
			}
		})
	}
}

func TestReportValidateRejectsBlockingStateLabeledNotUsed(t *testing.T) {
	report := Report{SchemaVersion: "v1", Components: []ComponentReport{{
		ID:                         "storage.vector",
		Support:                    SupportExcluded,
		Observation:                ObservationObserved,
		ReadPermission:             PermissionDenied,
		Capture:                    CaptureNotUsed,
		ByteIntegrity:              IntegrityNotUsed,
		RecoverabilityPrerequisite: RecoveryNotUsed,
		Restore:                    RestoreNotUsed,
		BehaviorVerification:       BehaviorNotUsed,
		NotUsedReason:              NotUsedReasonOutsideDeclaredScope,
	}}}
	report.Summary = report.ExpectedSummary()

	err := report.Validate()
	if err == nil || !strings.Contains(err.Error(), "blocking state cannot be labeled not_used") {
		t.Fatalf("Validate() error = %v, want blocking-state not_used rejection", err)
	}
}

func TestReportValidateRejectsUnknownEnumsAndDuplicateOrEmptyIDs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Report)
	}{
		{"unknown enum", func(report *Report) { report.Components[0].Capture = CaptureState("invented") }},
		{"empty ID", func(report *Report) { report.Components[0].ID = "" }},
		{"duplicate ID", func(report *Report) { report.Components = append(report.Components, report.Components[0]) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := completeReport()
			test.edit(&report)
			if err := report.Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}
}

func TestReportJSONV1Shape(t *testing.T) {
	encoded, err := json.Marshal(completeReport())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal(actual) error = %v", err)
	}
	want := map[string]any{
		"schema_version": "v1",
		"components": []any{map[string]any{
			"id":                          "database.public",
			"support":                     "required",
			"observation":                 "observed",
			"read_permission":             "granted",
			"capture":                     "captured",
			"byte_integrity":              "passed",
			"recoverability_prerequisite": "ready",
			"restore":                     "restored",
			"behavior_verification":       "passed",
		}},
		"summary": map[string]any{
			"coverage": "complete",
			"restore":  "success",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON shape = %#v, want %#v", got, want)
	}
}

func TestReportValidateRequiresReasonForNotUsedOrExcludedComponent(t *testing.T) {
	report := completeReport()
	report.Components[0] = ComponentReport{
		ID:                         "storage.vector",
		Support:                    SupportDetectAndBlock,
		Observation:                ObservationNotObserved,
		ReadPermission:             PermissionNotRequired,
		Capture:                    CaptureNotUsed,
		ByteIntegrity:              IntegrityNotUsed,
		RecoverabilityPrerequisite: RecoveryNotUsed,
		Restore:                    RestoreNotUsed,
		BehaviorVerification:       BehaviorNotUsed,
	}
	report.Summary = report.ExpectedSummary()
	if err := report.Validate(); err == nil {
		t.Fatal("Validate() succeeded without a not_used reason")
	}

	report.Components[0].NotUsedReason = NotUsedReasonFeatureNotObserved
	report.Summary = report.ExpectedSummary()
	if err := report.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	report = completeReport()
	report.Components[0].Support = SupportExcluded
	report.Summary = report.ExpectedSummary()
	if got, want := report.Summary, (ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked}); got != want {
		t.Fatalf("excluded Summary = %#v, want %#v", got, want)
	}
	if err := report.Validate(); err == nil {
		t.Fatal("Validate() succeeded without an excluded-component reason")
	}
	report.Components[0].NotUsedReason = NotUsedReasonOutsideDeclaredScope
	if err := report.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestReportSummarySeparatesCoverageAndRestore(t *testing.T) {
	tests := []struct {
		name   string
		report Report
		want   ReportSummary
	}{
		{
			name:   "captured backup has not run restore",
			report: backupReport(),
			want:   ReportSummary{Coverage: CoverageComplete, Restore: RestoreNotRun},
		},
		{
			name: "capture failure before restore is blocked",
			report: func() Report {
				report := backupReport()
				report.Components[0].Capture = CaptureFailed
				report.Components[0].ByteIntegrity = IntegrityNotChecked
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "integrity failure before restore is blocked",
			report: func() Report {
				report := backupReport()
				report.Components[0].ByteIntegrity = IntegrityFailed
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "inconclusive integrity before restore is blocked",
			report: func() Report {
				report := backupReport()
				report.Components[0].ByteIntegrity = IntegrityInconclusive
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "failed restore preserves capture coverage",
			report: func() Report {
				report := backupReport()
				report.Components[0].Restore = RestoreFailed
				report.Components[0].BehaviorVerification = BehaviorNotChecked
				return report
			}(),
			want: ReportSummary{Coverage: CoverageComplete, Restore: RestoreOutcomeFailed},
		},
		{
			name: "mixed restore evidence is blocked",
			report: func() Report {
				report := backupReport()
				restored := report.Components[0]
				restored.ID = "database.auth"
				restored.Restore = RestoreRestored
				restored.BehaviorVerification = BehaviorPassed
				report.Components = append(report.Components, restored)
				return report
			}(),
			want: ReportSummary{Coverage: CoverageComplete, Restore: RestoreBlocked},
		},
		{
			name: "sampled behavior is inconclusive",
			report: func() Report {
				report := backupReport()
				report.Components[0].Restore = RestoreRestored
				report.Components[0].BehaviorVerification = BehaviorSampled
				return report
			}(),
			want: ReportSummary{Coverage: CoverageComplete, Restore: RestoreInconclusive},
		},
		{
			name:   "all absent required components have not run restore",
			report: absentReport(SupportRequired),
			want:   ReportSummary{Coverage: CoverageComplete, Restore: RestoreNotRun},
		},
		{
			name:   "all absent unsupported components have not run restore",
			report: absentReport(SupportUnsupported),
			want:   ReportSummary{Coverage: CoverageComplete, Restore: RestoreNotRun},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.report.Summary = test.want
			if got := test.report.ExpectedSummary(); got != test.want {
				t.Fatalf("ExpectedSummary() = %#v, want %#v", got, test.want)
			}
			if err := test.report.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestReportUnsupportedAndDetectComponentsRemainExplicit(t *testing.T) {
	tests := []struct {
		name   string
		report Report
		want   ReportSummary
	}{
		{
			name: "unsupported observed",
			report: Report{SchemaVersion: "v1", Components: []ComponentReport{{
				ID:                         "storage.vector",
				Support:                    SupportUnsupported,
				Observation:                ObservationObserved,
				ReadPermission:             PermissionGranted,
				Capture:                    CaptureUnsupported,
				ByteIntegrity:              IntegrityNotChecked,
				RecoverabilityPrerequisite: RecoveryUnknown,
				Restore:                    RestoreUnsupported,
				BehaviorVerification:       BehaviorNotChecked,
			}}},
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "detect and block permission denied",
			report: Report{SchemaVersion: "v1", Components: []ComponentReport{{
				ID:                         "database.foreign_data",
				Support:                    SupportDetectAndBlock,
				Observation:                ObservationUnknown,
				ReadPermission:             PermissionDenied,
				Capture:                    CapturePermissionDenied,
				ByteIntegrity:              IntegrityNotChecked,
				RecoverabilityPrerequisite: RecoveryUnknown,
				Restore:                    RestoreNotAttempted,
				BehaviorVerification:       BehaviorNotChecked,
			}}},
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name:   "detect and block reliably absent",
			report: absentReport(SupportDetectAndBlock),
			want:   ReportSummary{Coverage: CoverageComplete, Restore: RestoreNotRun},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.report.Summary = test.want
			if got := test.report.ExpectedSummary(); got != test.want {
				t.Fatalf("ExpectedSummary() = %#v, want %#v", got, test.want)
			}
			if err := test.report.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestReportValidateRejectsInvalidUTF8IDs(t *testing.T) {
	invalidA := string([]byte{0xff})
	invalidB := string([]byte{0xfe})
	encoded, err := json.Marshal([]string{invalidA, invalidB})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if got, want := string(encoded), `["\ufffd","\ufffd"]`; got != want {
		t.Fatalf("JSON = %s, want %s", got, want)
	}

	tests := []struct {
		name string
		edit func(*Report)
	}{
		{"first invalid component ID", func(report *Report) { report.Components[0].ID = invalidA }},
		{"second invalid component ID", func(report *Report) { report.Components[0].ID = invalidB }},
		{"invalid structured requirement ID", func(report *Report) { report.Components[0].StructuredRequirementIDs = []string{invalidA} }},
		{"invalid manual requirement ID", func(report *Report) { report.Components[0].ManualRequirementIDs = []string{invalidB} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := completeReport()
			test.edit(&report)
			if err := report.Validate(); err == nil {
				t.Fatal("Validate() succeeded")
			}
		})
	}

	report := completeReport()
	report.Components[0].ID = invalidA
	second := report.Components[0]
	second.ID = invalidB
	report.Components = append(report.Components, second)
	if err := report.Validate(); err == nil || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Fatalf("Validate() error = %v, want invalid UTF-8 before duplicate handling", err)
	}
}

func TestReportJSONRoundTripPreservesIDs(t *testing.T) {
	report := backupReport()
	report.Components[0].ID = "database.public/π"
	report.Components[0].StructuredRequirementIDs = []string{"config.auth.redirect_url"}
	report.Components[0].ManualRequirementIDs = []string{"auth.smtp.password"}
	report.Summary = report.ExpectedSummary()

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := decoded.Components[0].ID, report.Components[0].ID; got != want {
		t.Fatalf("component ID = %q, want %q", got, want)
	}
	if got, want := decoded.Components[0].StructuredRequirementIDs, report.Components[0].StructuredRequirementIDs; !reflect.DeepEqual(got, want) {
		t.Fatalf("structured IDs = %#v, want %#v", got, want)
	}
	if got, want := decoded.Components[0].ManualRequirementIDs, report.Components[0].ManualRequirementIDs; !reflect.DeepEqual(got, want) {
		t.Fatalf("manual IDs = %#v, want %#v", got, want)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("decoded Validate() error = %v", err)
	}
}

func TestReportRestorePrecedence(t *testing.T) {
	tests := []struct {
		name   string
		report Report
		want   ReportSummary
	}{
		{
			name: "actual restore failure wins over incomplete coverage",
			report: func() Report {
				report := backupReport()
				report.Components[0].Capture = CaptureFailed
				report.Components[0].ByteIntegrity = IntegrityNotChecked
				report.Components[0].Restore = RestoreFailed
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreOutcomeFailed},
		},
		{
			name: "incomplete coverage blocks sampled behavior",
			report: func() Report {
				report := backupReport()
				report.Components[0].Capture = CaptureFailed
				report.Components[0].ByteIntegrity = IntegrityNotChecked
				report.Components[0].Restore = RestoreRestored
				report.Components[0].BehaviorVerification = BehaviorSampled
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "untouched component blocks sampled restored component",
			report: func() Report {
				report := backupReport()
				restored := report.Components[0]
				restored.ID = "database.auth"
				restored.Restore = RestoreRestored
				restored.BehaviorVerification = BehaviorSampled
				report.Components = append(report.Components, restored)
				return report
			}(),
			want: ReportSummary{Coverage: CoverageComplete, Restore: RestoreBlocked},
		},
		{
			name: "permission denied component blocks sampled behavior",
			report: func() Report {
				report := completeReport()
				report.Components[0].BehaviorVerification = BehaviorSampled
				denied := report.Components[0]
				denied.ID = "database.restricted"
				denied.ReadPermission = PermissionDenied
				denied.Capture = CapturePermissionDenied
				denied.ByteIntegrity = IntegrityNotChecked
				denied.RecoverabilityPrerequisite = RecoveryUnknown
				denied.Restore = RestoreNotAttempted
				denied.BehaviorVerification = BehaviorNotChecked
				report.Components = append(report.Components, denied)
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "unsupported component blocks sampled behavior",
			report: func() Report {
				report := completeReport()
				report.Components[0].BehaviorVerification = BehaviorSampled
				unsupported := report.Components[0]
				unsupported.ID = "storage.vector"
				unsupported.Support = SupportUnsupported
				unsupported.Capture = CaptureUnsupported
				unsupported.ByteIntegrity = IntegrityNotChecked
				unsupported.RecoverabilityPrerequisite = RecoveryUnknown
				unsupported.Restore = RestoreUnsupported
				unsupported.BehaviorVerification = BehaviorNotChecked
				report.Components = append(report.Components, unsupported)
				return report
			}(),
			want: ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked},
		},
		{
			name: "all restored unchecked behavior is blocked",
			report: func() Report {
				report := completeReport()
				report.Components[0].BehaviorVerification = BehaviorNotChecked
				return report
			}(),
			want: ReportSummary{Coverage: CoverageComplete, Restore: RestoreBlocked},
		},
		{
			name: "all restored sampled behavior is inconclusive",
			report: func() Report {
				report := completeReport()
				report.Components[0].BehaviorVerification = BehaviorSampled
				return report
			}(),
			want: ReportSummary{Coverage: CoverageComplete, Restore: RestoreInconclusive},
		},
		{
			name:   "all restored passed behavior succeeds",
			report: completeReport(),
			want:   ReportSummary{Coverage: CoverageComplete, Restore: RestoreSuccess},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.report.Summary = test.want
			if got := test.report.ExpectedSummary(); got != test.want {
				t.Fatalf("ExpectedSummary() = %#v, want %#v", got, test.want)
			}
			if err := test.report.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestReportValidateCoherence(t *testing.T) {
	tests := []struct {
		name string
		edit func(*ComponentReport)
	}{
		{"not observed requires absence tuple", func(component *ComponentReport) { component.Observation = ObservationNotObserved }},
		{"integrity passed requires captured bytes", func(component *ComponentReport) { component.Capture = CaptureUnknown }},
		{"captured bytes cannot have unused integrity", func(component *ComponentReport) { component.ByteIntegrity = IntegrityNotUsed }},
		{"not attempted restore requires unchecked behavior", func(component *ComponentReport) { component.Restore = RestoreNotAttempted }},
		{"unused restore requires unused behavior", func(component *ComponentReport) { component.Restore = RestoreNotUsed }},
		{"sampled behavior requires restored component", func(component *ComponentReport) {
			component.Restore = RestoreNotAttempted
			component.BehaviorVerification = BehaviorSampled
		}},
		{"failed restore cannot claim passed behavior", func(component *ComponentReport) { component.Restore = RestoreFailed }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := completeReport()
			test.edit(&report.Components[0])
			report.Summary = report.ExpectedSummary()
			if err := report.Validate(); err == nil {
				t.Fatal("Validate() succeeded for contradictory component states")
			}
		})
	}

	partial := backupReport()
	partial.Components[0].Capture = CapturePartiallyCaptured
	partial.Summary = ReportSummary{Coverage: CoverageIncomplete, Restore: RestoreBlocked}
	if err := partial.Validate(); err != nil {
		t.Fatalf("partial capture with passed integrity should validate: %v", err)
	}
}

func TestReportRestoreBehaviorReductionIsOrderIndependent(t *testing.T) {
	for _, sampledFirst := range []bool{true, false} {
		name := "unchecked first"
		if sampledFirst {
			name = "sampled first"
		}
		t.Run(name, func(t *testing.T) {
			report := completeReport()
			sampled := report.Components[0]
			sampled.ID = "database.auth"
			sampled.BehaviorVerification = BehaviorSampled
			unchecked := report.Components[0]
			unchecked.ID = "database.storage"
			unchecked.BehaviorVerification = BehaviorNotChecked
			if sampledFirst {
				report.Components = []ComponentReport{sampled, unchecked}
			} else {
				report.Components = []ComponentReport{unchecked, sampled}
			}
			report.Summary = ReportSummary{Coverage: CoverageComplete, Restore: RestoreBlocked}
			if got := report.ExpectedSummary(); got != report.Summary {
				t.Fatalf("ExpectedSummary() = %#v, want %#v", got, report.Summary)
			}
			if err := report.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestReportValidateReasonCodes(t *testing.T) {
	tests := []struct {
		name    string
		report  func() Report
		wantErr string
	}{
		{"known feature absence", func() Report { return absentReport(SupportDetectAndBlock) }, ""},
		{"known exclusion", func() Report {
			report := completeReport()
			report.Components[0].Support = SupportExcluded
			report.Components[0].NotUsedReason = NotUsedReasonOutsideDeclaredScope
			return report
		}, ""},
		{"empty", func() Report {
			report := absentReport(SupportDetectAndBlock)
			report.Components[0].NotUsedReason = ""
			return report
		}, "not_used or excluded component requires a reason code"},
		{"secret canary", func() Report {
			report := absentReport(SupportDetectAndBlock)
			report.Components[0].NotUsedReason = NotUsedReason("secret=canary")
			return report
		}, "unknown not_used reason code"},
		{"terminal control", func() Report {
			report := absentReport(SupportDetectAndBlock)
			report.Components[0].NotUsedReason = NotUsedReason("\x1b[31m")
			return report
		}, "unknown not_used reason code"},
		{"raw error", func() Report {
			report := absentReport(SupportDetectAndBlock)
			report.Components[0].NotUsedReason = NotUsedReason("connection refused")
			return report
		}, "unknown not_used reason code"},
		{"green component reason is extraneous", func() Report {
			report := completeReport()
			report.Components[0].NotUsedReason = NotUsedReasonFeatureNotObserved
			return report
		}, "not_used reason code is extraneous"},
		{"feature reason requires absence", func() Report {
			report := completeReport()
			report.Components[0].Support = SupportExcluded
			report.Components[0].NotUsedReason = NotUsedReasonFeatureNotObserved
			return report
		}, "feature_not_observed requires the reliable absence tuple"},
		{"outside reason requires exclusion", func() Report {
			report := absentReport(SupportDetectAndBlock)
			report.Components[0].NotUsedReason = NotUsedReasonOutsideDeclaredScope
			return report
		}, "outside_declared_scope requires excluded support"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := test.report()
			report.Summary = report.ExpectedSummary()
			err := report.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func backupReport() Report {
	report := completeReport()
	report.Components[0].Restore = RestoreNotAttempted
	report.Components[0].BehaviorVerification = BehaviorNotChecked
	report.Summary = ReportSummary{Coverage: CoverageComplete, Restore: RestoreNotRun}
	return report
}

func absentReport(support SupportState) Report {
	report := Report{SchemaVersion: "v1", Components: []ComponentReport{{
		ID:                         "storage.vector",
		Support:                    support,
		Observation:                ObservationNotObserved,
		ReadPermission:             PermissionNotRequired,
		Capture:                    CaptureNotUsed,
		ByteIntegrity:              IntegrityNotUsed,
		RecoverabilityPrerequisite: RecoveryNotUsed,
		Restore:                    RestoreNotUsed,
		BehaviorVerification:       BehaviorNotUsed,
		NotUsedReason:              NotUsedReasonFeatureNotObserved,
	}}}
	report.Summary = ReportSummary{Coverage: CoverageComplete, Restore: RestoreNotRun}
	return report
}
