package main

// ExitReason is a stable process-termination category.
type ExitReason string

// ExitCode is a stable operating-system process code.
type ExitCode int

// ExitCodeSpec is one documented reason-to-code mapping.
type ExitCodeSpec struct {
	Reason      ExitReason `json:"reason"`
	Code        ExitCode   `json:"code"`
	Description string     `json:"description"`
}

var exitCodeTable = []ExitCodeSpec{
	{Reason: "drain_complete", Code: 0, Description: "graceful drain completed"},
	{Reason: "fatal", Code: 1, Description: "unclassified fatal process failure"},
	{Reason: "validate_failure", Code: 2, Description: "configuration validation failed"},
	{Reason: "doctor_failure", Code: 3, Description: "one or more doctor checks failed"},
	{Reason: "bind_failure", Code: 4, Description: "required listener could not bind"},
}

// ExitCodeTable returns an immutable snapshot of the documented table.
func ExitCodeTable() []ExitCodeSpec {
	return append([]ExitCodeSpec(nil), exitCodeTable...)
}

// ExitCodeFor resolves a documented process-termination category.
func ExitCodeFor(reason ExitReason) (ExitCode, bool) {
	for _, entry := range exitCodeTable {
		if entry.Reason == reason {
			return entry.Code, true
		}
	}
	return 0, false
}
