package health

import "testing"

func TestRegistryKeepsRegistrationOrderAndRefreshesCachedValues(t *testing.T) {
	resetForTesting()
	t.Cleanup(resetForTesting)
	first, second := "ok", "degraded"
	Register("blocklist_one", false, func() string { return first })
	Register("trivy_db", false, func() string { return second })
	checks := Snapshot()
	if len(checks) != 2 || checks[0].Key != "blocklist_one" || checks[1].Key != "trivy_db" {
		t.Fatalf("registration order=%v", checks)
	}
	first = "error"
	if Snapshot()[0].Status != "ok" {
		t.Fatal("Snapshot should return cached values")
	}
	Refresh()
	if Snapshot()[0].Status != "error" {
		t.Fatal("Refresh did not update check result")
	}
}

func TestRegistryUnregistersDisabledDatasetChecks(t *testing.T) {
	resetForTesting()
	t.Cleanup(resetForTesting)
	Register("blocklist_one", false, func() string { return "ok" })
	Register("blocklist_two", false, func() string { return "ok" })
	UnregisterPrefix("blocklist_")
	if checks := Snapshot(); len(checks) != 0 {
		t.Fatalf("disabled checks remain: %v", checks)
	}
}

func TestRegistryMarksLastGoodDataDegradedAfterUpdateFailure(t *testing.T) {
	resetForTesting()
	t.Cleanup(resetForTesting)
	Register("cve_nvd", false, func() string { return "ok" })
	MarkUpdateFailure("cve_nvd", true)
	Refresh()
	if got := Snapshot()[0].Status; got != "degraded" {
		t.Fatalf("failed update status=%q", got)
	}
	MarkUpdateFailure("cve_nvd", false)
	Refresh()
	if got := Snapshot()[0].Status; got != "ok" {
		t.Fatalf("recovered update status=%q", got)
	}
}
