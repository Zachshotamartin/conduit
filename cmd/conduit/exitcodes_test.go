package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestUNIT016_ExitCodeTableMatchesCheckedInContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(exitCodeFixturePath(t))
	if err != nil {
		t.Fatalf("ReadFile(exit-codes.json) error = %v", err)
	}
	var want []ExitCodeSpec
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("Unmarshal(exit-codes.json) error = %v", err)
	}
	got := ExitCodeTable()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExitCodeTable() = %#v, want checked-in contract %#v", got, want)
	}

	seenReasons := make(map[ExitReason]struct{}, len(got))
	seenCodes := make(map[ExitCode]ExitReason, len(got))
	for _, entry := range got {
		if entry.Description == "" {
			t.Errorf("exit reason %q has no documented description", entry.Reason)
		}
		if _, duplicate := seenReasons[entry.Reason]; duplicate {
			t.Errorf("duplicate exit reason %q", entry.Reason)
		}
		seenReasons[entry.Reason] = struct{}{}
		if previous, duplicate := seenCodes[entry.Code]; duplicate {
			t.Errorf("exit reasons %q and %q share code %d", previous, entry.Reason, entry.Code)
		}
		seenCodes[entry.Code] = entry.Reason

		code, ok := ExitCodeFor(entry.Reason)
		if !ok || code != entry.Code {
			t.Errorf("ExitCodeFor(%q) = %d, %t; want %d, true", entry.Reason, code, ok, entry.Code)
		}
	}

	wantReasons := []ExitReason{
		"drain_complete",
		"fatal",
		"validate_failure",
		"doctor_failure",
		"bind_failure",
	}
	if len(seenReasons) != len(wantReasons) {
		t.Fatalf("exit table has %d reasons, want %d", len(seenReasons), len(wantReasons))
	}
	for _, reason := range wantReasons {
		if _, ok := seenReasons[reason]; !ok {
			t.Errorf("exit table omits process path %q", reason)
		}
	}
	if code, _ := ExitCodeFor("drain_complete"); code != 0 {
		t.Errorf("drain_complete code = %d, want 0", code)
	}
	for _, reason := range []ExitReason{"fatal", "validate_failure", "doctor_failure", "bind_failure"} {
		if code, _ := ExitCodeFor(reason); code == 0 {
			t.Errorf("%s code = 0, want nonzero", reason)
		}
	}
	if _, ok := ExitCodeFor("not_a_process_path"); ok {
		t.Fatal("ExitCodeFor accepted an undocumented process path")
	}
}

func TestUNIT016_ExitCodeTableReturnsIndependentSnapshot(t *testing.T) {
	t.Parallel()

	first := ExitCodeTable()
	if len(first) == 0 {
		t.Fatal("ExitCodeTable() returned no entries")
	}
	original := first[0]
	first[0] = ExitCodeSpec{Reason: "caller_mutation", Code: 99, Description: "mutated"}
	if got := ExitCodeTable()[0]; got != original {
		t.Fatalf("ExitCodeTable() exposed mutable package state: %#v, want %#v", got, original)
	}
}

func exitCodeFixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate exitcodes_test.go")
	}
	return filepath.Join(filepath.Dir(file), "testdata", "exit-codes.json")
}
