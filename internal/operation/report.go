// Package operation defines operation-level status reports. It does not perform
// backup or restore work.
package operation

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ObservationState records whether a feature was discovered, separately from
// whether it could be read.
type ObservationState string

const (
	ObservationObserved    ObservationState = "observed"
	ObservationNotObserved ObservationState = "not_observed"
	ObservationUnknown     ObservationState = "unknown"
)

// PermissionState records permission to read a component.
type PermissionState string

const (
	PermissionGranted     PermissionState = "granted"
	PermissionDenied      PermissionState = "permission_denied"
	PermissionNotRequired PermissionState = "not_required"
	PermissionUnknown     PermissionState = "unknown"
)

// CaptureState records the result of source capture.
type CaptureState string

const (
	CaptureNotUsed           CaptureState = "not_used"
	CaptureCaptured          CaptureState = "captured"
	CapturePartiallyCaptured CaptureState = "partially_captured"
	CaptureManualRequired    CaptureState = "manual_required"
	CaptureUnsupported       CaptureState = "unsupported"
	CapturePermissionDenied  CaptureState = "permission_denied"
	CaptureUnknown           CaptureState = "unknown"
	CaptureFailed            CaptureState = "failed"
)

// IntegrityState records byte-level artifact validation, not recovery proof.
type IntegrityState string

const (
	IntegrityNotUsed      IntegrityState = "not_used"
	IntegrityNotChecked   IntegrityState = "not_checked"
	IntegrityPassed       IntegrityState = "passed"
	IntegrityFailed       IntegrityState = "failed"
	IntegrityInconclusive IntegrityState = "inconclusive"
)

// RecoveryState records whether required recovery material is available.
type RecoveryState string

const (
	RecoveryNotUsed                 RecoveryState = "not_used"
	RecoveryReady                   RecoveryState = "ready"
	RecoveryMissingCriticalMaterial RecoveryState = "missing_critical_material"
	RecoveryManualRequired          RecoveryState = "manual_required"
	RecoveryUnknown                 RecoveryState = "unknown"
)

// RestoreState records application of a component to the destination.
type RestoreState string

const (
	RestoreNotUsed        RestoreState = "not_used"
	RestoreNotAttempted   RestoreState = "not_attempted"
	RestoreRestored       RestoreState = "restored"
	RestoreManualRequired RestoreState = "manual_required"
	RestoreUnsupported    RestoreState = "unsupported"
	RestoreFailed         RestoreState = "failed"
	RestoreUnknown        RestoreState = "unknown"
)

// BehaviorState records behavioral verification after restore.
type BehaviorState string

const (
	BehaviorNotUsed      BehaviorState = "not_used"
	BehaviorNotChecked   BehaviorState = "not_checked"
	BehaviorSampled      BehaviorState = "sampled"
	BehaviorPassed       BehaviorState = "passed"
	BehaviorFailed       BehaviorState = "failed"
	BehaviorInconclusive BehaviorState = "inconclusive"
)

// SupportState is the initially declared handling for an inventory component.
type SupportState string

const (
	SupportRequired       SupportState = "required"
	SupportManual         SupportState = "manual"
	SupportUnsupported    SupportState = "unsupported"
	SupportDetectAndBlock SupportState = "detect_and_block"
	SupportExcluded       SupportState = "excluded"
)

// NotUsedReason is an allowlisted non-secret explanation for omitted work.
type NotUsedReason string

const (
	NotUsedReasonFeatureNotObserved   NotUsedReason = "feature_not_observed"
	NotUsedReasonOutsideDeclaredScope NotUsedReason = "outside_declared_scope"
)

// CoverageState is the report-wide coverage conclusion.
type CoverageState string

const (
	CoverageComplete   CoverageState = "complete"
	CoverageIncomplete CoverageState = "incomplete"
)

// RestoreOutcome is the report-wide restore conclusion.
type RestoreOutcome string

const (
	RestoreNotRun        RestoreOutcome = "not_run"
	RestoreSuccess       RestoreOutcome = "success"
	RestoreBlocked       RestoreOutcome = "blocked"
	RestoreOutcomeFailed RestoreOutcome = "failed"
	RestoreInconclusive  RestoreOutcome = "inconclusive"
)

// Report is safe to marshal to stdout JSON: it has states, IDs, and allowlisted
// non-secret reason codes only. Payloads, credentials, and source data do not
// belong in this contract.
type Report struct {
	SchemaVersion string            `json:"schema_version"`
	Components    []ComponentReport `json:"components"`
	Summary       ReportSummary     `json:"summary"`
}

// ComponentReport keeps each gate separate so a successful command cannot hide
// a permission, capture, recovery, integrity, restore, or behavior gap.
type ComponentReport struct {
	ID                         string           `json:"id"`
	Support                    SupportState     `json:"support"`
	Observation                ObservationState `json:"observation"`
	ReadPermission             PermissionState  `json:"read_permission"`
	Capture                    CaptureState     `json:"capture"`
	ByteIntegrity              IntegrityState   `json:"byte_integrity"`
	RecoverabilityPrerequisite RecoveryState    `json:"recoverability_prerequisite"`
	Restore                    RestoreState     `json:"restore"`
	BehaviorVerification       BehaviorState    `json:"behavior_verification"`
	StructuredRequirementIDs   []string         `json:"structured_requirement_ids,omitempty"`
	ManualRequirementIDs       []string         `json:"manual_requirement_ids,omitempty"`
	NotUsedReason              NotUsedReason    `json:"not_used_reason,omitempty"`
}

// ReportSummary is derived from component gates and must not be hand-waved.
type ReportSummary struct {
	Coverage CoverageState  `json:"coverage"`
	Restore  RestoreOutcome `json:"restore"`
}

// ExpectedSummary derives independent capture coverage and restore conclusions.
func (r Report) ExpectedSummary() ReportSummary {
	coverageComplete := len(r.Components) > 0
	for _, component := range r.Components {
		if !componentCoverageComplete(component) {
			coverageComplete = false
		}
	}
	coverage := CoverageIncomplete
	if coverageComplete {
		coverage = CoverageComplete
	}
	return ReportSummary{Coverage: coverage, Restore: restoreOutcome(r.Components, coverageComplete)}
}

func restoreOutcome(components []ComponentReport, coverageComplete bool) RestoreOutcome {
	for _, component := range components {
		if componentFailed(component) {
			return RestoreOutcomeFailed
		}
	}
	if !coverageComplete {
		return RestoreBlocked
	}

	applicable, untouched, restored := 0, true, true
	for _, component := range components {
		if reliablyAbsent(component) {
			continue
		}
		applicable++
		if component.Restore != RestoreNotAttempted {
			untouched = false
		}
		if component.Restore != RestoreRestored {
			restored = false
		}
	}
	if applicable == 0 || untouched {
		return RestoreNotRun
	}
	if !restored {
		return RestoreBlocked
	}
	inconclusive := false
	for _, component := range components {
		if reliablyAbsent(component) {
			continue
		}
		switch component.BehaviorVerification {
		case BehaviorPassed:
		case BehaviorSampled, BehaviorInconclusive:
			inconclusive = true
		default:
			return RestoreBlocked
		}
	}
	if inconclusive {
		return RestoreInconclusive
	}
	return RestoreSuccess
}

// Validate rejects unknown states, duplicate or empty component IDs, missing
// not-used/excluded reasons, and summaries that overstate recovery.
func (r Report) Validate() error {
	if r.SchemaVersion != "v1" {
		return fmt.Errorf("unsupported report schema version %q", r.SchemaVersion)
	}
	if len(r.Components) == 0 {
		return fmt.Errorf("report has no components")
	}
	seen := make(map[string]struct{}, len(r.Components))
	for _, component := range r.Components {
		if !utf8.ValidString(component.ID) {
			return fmt.Errorf("component ID is not valid UTF-8")
		}
		if strings.TrimSpace(component.ID) == "" {
			return fmt.Errorf("component ID is empty")
		}
		if _, exists := seen[component.ID]; exists {
			return fmt.Errorf("duplicate component ID %q", component.ID)
		}
		seen[component.ID] = struct{}{}
		if err := component.validate(); err != nil {
			return fmt.Errorf("component %q: %w", component.ID, err)
		}
	}
	if expected := r.ExpectedSummary(); r.Summary != expected {
		return fmt.Errorf("summary %#v does not match component gates %#v", r.Summary, expected)
	}
	return nil
}

func (c ComponentReport) validate() error {
	if !validSupport(c.Support) || !validObservation(c.Observation) || !validPermission(c.ReadPermission) ||
		!validCapture(c.Capture) || !validIntegrity(c.ByteIntegrity) || !validRecovery(c.RecoverabilityPrerequisite) ||
		!validRestore(c.Restore) || !validBehavior(c.BehaviorVerification) {
		return fmt.Errorf("contains an unknown state")
	}
	if c.Observation == ObservationNotObserved && !reliablyAbsent(c) {
		return fmt.Errorf("not_observed requires the reliable absence tuple")
	}
	if c.ByteIntegrity == IntegrityPassed && c.Capture != CaptureCaptured && c.Capture != CapturePartiallyCaptured {
		return fmt.Errorf("passed integrity requires captured bytes")
	}
	if c.Capture == CaptureCaptured && c.ByteIntegrity == IntegrityNotUsed {
		return fmt.Errorf("captured bytes cannot have unused integrity")
	}
	if c.Restore == RestoreNotAttempted && c.BehaviorVerification != BehaviorNotChecked {
		return fmt.Errorf("not_attempted restore requires not_checked behavior")
	}
	if c.Restore == RestoreNotUsed && c.BehaviorVerification != BehaviorNotUsed {
		return fmt.Errorf("not_used restore requires not_used behavior")
	}
	if c.Restore == RestoreFailed && c.BehaviorVerification == BehaviorPassed {
		return fmt.Errorf("failed restore cannot have passed behavior")
	}
	if activeBehavior(c.BehaviorVerification) && c.Restore != RestoreRestored {
		return fmt.Errorf("active behavior evidence requires restored component")
	}
	requiresReason := hasNotUsed(c) || c.Support == SupportExcluded
	if c.NotUsedReason != "" && !validNotUsedReason(c.NotUsedReason) {
		return fmt.Errorf("unknown not_used reason code")
	}
	if requiresReason && c.NotUsedReason == "" {
		return fmt.Errorf("not_used or excluded component requires a reason code")
	}
	if !requiresReason && c.NotUsedReason != "" {
		return fmt.Errorf("not_used reason code is extraneous")
	}
	if c.NotUsedReason == NotUsedReasonFeatureNotObserved && !reliablyAbsent(c) {
		return fmt.Errorf("feature_not_observed requires the reliable absence tuple")
	}
	if c.NotUsedReason == NotUsedReasonOutsideDeclaredScope && c.Support != SupportExcluded {
		return fmt.Errorf("outside_declared_scope requires excluded support")
	}
	if c.Capture == CaptureNotUsed && !reliablyAbsent(c) && componentBlocking(c) {
		return fmt.Errorf("blocking state cannot be labeled not_used")
	}
	if hasManualRequirement(c) && len(c.ManualRequirementIDs) == 0 {
		return fmt.Errorf("manual_required requires a manual requirement ID")
	}
	if err := validateRequirementIDs("structured", c.StructuredRequirementIDs); err != nil {
		return err
	}
	return validateRequirementIDs("manual", c.ManualRequirementIDs)
}

func componentCoverageComplete(c ComponentReport) bool {
	// Exclusions stay visible as an incomplete declared scope, even when absent.
	if c.Support == SupportExcluded {
		return false
	}
	if reliablyAbsent(c) {
		return true
	}
	if c.Support == SupportUnsupported || c.Support == SupportDetectAndBlock {
		return false
	}
	return c.Observation == ObservationObserved && c.ReadPermission == PermissionGranted &&
		c.Capture == CaptureCaptured && c.ByteIntegrity == IntegrityPassed &&
		c.RecoverabilityPrerequisite == RecoveryReady
}

func reliablyAbsent(c ComponentReport) bool {
	return c.Observation == ObservationNotObserved && c.ReadPermission == PermissionNotRequired &&
		c.Capture == CaptureNotUsed && c.ByteIntegrity == IntegrityNotUsed &&
		c.RecoverabilityPrerequisite == RecoveryNotUsed && c.Restore == RestoreNotUsed &&
		c.BehaviorVerification == BehaviorNotUsed
}

func componentBlocking(c ComponentReport) bool {
	return c.Support == SupportUnsupported || c.Support == SupportExcluded ||
		(c.Support == SupportDetectAndBlock && c.Observation == ObservationObserved) ||
		c.Observation == ObservationUnknown || c.ReadPermission == PermissionDenied || c.ReadPermission == PermissionUnknown ||
		c.Capture == CapturePartiallyCaptured || c.Capture == CaptureManualRequired || c.Capture == CaptureUnsupported ||
		c.Capture == CapturePermissionDenied || c.Capture == CaptureUnknown || c.Capture == CaptureFailed ||
		c.ByteIntegrity == IntegrityFailed || c.ByteIntegrity == IntegrityInconclusive ||
		c.RecoverabilityPrerequisite == RecoveryMissingCriticalMaterial || c.RecoverabilityPrerequisite == RecoveryManualRequired ||
		c.RecoverabilityPrerequisite == RecoveryUnknown || c.Restore == RestoreManualRequired || c.Restore == RestoreUnsupported ||
		c.Restore == RestoreFailed || c.Restore == RestoreUnknown || c.BehaviorVerification == BehaviorFailed ||
		c.BehaviorVerification == BehaviorInconclusive
}

func componentFailed(c ComponentReport) bool {
	return c.Restore == RestoreFailed || c.BehaviorVerification == BehaviorFailed
}

func activeBehavior(value BehaviorState) bool {
	return value == BehaviorSampled || value == BehaviorPassed || value == BehaviorFailed || value == BehaviorInconclusive
}

func hasNotUsed(c ComponentReport) bool {
	return c.Capture == CaptureNotUsed || c.ByteIntegrity == IntegrityNotUsed ||
		c.RecoverabilityPrerequisite == RecoveryNotUsed || c.Restore == RestoreNotUsed || c.BehaviorVerification == BehaviorNotUsed
}

func hasManualRequirement(c ComponentReport) bool {
	return c.Capture == CaptureManualRequired || c.RecoverabilityPrerequisite == RecoveryManualRequired || c.Restore == RestoreManualRequired
}

func validateRequirementIDs(kind string, ids []string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !utf8.ValidString(id) {
			return fmt.Errorf("%s requirement ID is not valid UTF-8", kind)
		}
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%s requirement ID is empty", kind)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate %s requirement ID %q", kind, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validNotUsedReason(value NotUsedReason) bool {
	return value == NotUsedReasonFeatureNotObserved || value == NotUsedReasonOutsideDeclaredScope
}

func validObservation(value ObservationState) bool {
	return value == ObservationObserved || value == ObservationNotObserved || value == ObservationUnknown
}
func validPermission(value PermissionState) bool {
	return value == PermissionGranted || value == PermissionDenied || value == PermissionNotRequired || value == PermissionUnknown
}
func validCapture(value CaptureState) bool {
	return value == CaptureNotUsed || value == CaptureCaptured || value == CapturePartiallyCaptured || value == CaptureManualRequired || value == CaptureUnsupported || value == CapturePermissionDenied || value == CaptureUnknown || value == CaptureFailed
}
func validIntegrity(value IntegrityState) bool {
	return value == IntegrityNotUsed || value == IntegrityNotChecked || value == IntegrityPassed || value == IntegrityFailed || value == IntegrityInconclusive
}
func validRecovery(value RecoveryState) bool {
	return value == RecoveryNotUsed || value == RecoveryReady || value == RecoveryMissingCriticalMaterial || value == RecoveryManualRequired || value == RecoveryUnknown
}
func validRestore(value RestoreState) bool {
	return value == RestoreNotUsed || value == RestoreNotAttempted || value == RestoreRestored || value == RestoreManualRequired || value == RestoreUnsupported || value == RestoreFailed || value == RestoreUnknown
}
func validBehavior(value BehaviorState) bool {
	return value == BehaviorNotUsed || value == BehaviorNotChecked || value == BehaviorSampled || value == BehaviorPassed || value == BehaviorFailed || value == BehaviorInconclusive
}
func validSupport(value SupportState) bool {
	return value == SupportRequired || value == SupportManual || value == SupportUnsupported || value == SupportDetectAndBlock || value == SupportExcluded
}
