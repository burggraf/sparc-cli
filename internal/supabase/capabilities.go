package supabase

// ObservationState distinguishes observed values from capabilities that remain unknown.
type ObservationState string

const (
	CapabilityUnknown  ObservationState = "unknown"
	CapabilityObserved ObservationState = "observed"
)

// CapabilityObservation never treats a missing API field as an unsupported capability.
type CapabilityObservation struct {
	State ObservationState
	Value string
}

// ProjectCapabilities records only observations supported by the candidate fixture contract.
type ProjectCapabilities struct {
	DatabaseVersion       CapabilityObservation
	FeatureInventoryState ObservationState
}
