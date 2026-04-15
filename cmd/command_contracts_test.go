package cmd

import (
	"strings"
	"testing"
)

func TestCommandContractsExposeOneMeaningPerCommand(t *testing.T) {
	contracts := CommandContracts()

	if len(contracts) != 3 {
		t.Fatalf("expected 3 command contracts, got %d", len(contracts))
	}

	assertContractFlags(t, contracts["add"], true, true, false, true)
	assertContractFlags(t, contracts["remove"], true, true, false, true)
	assertContractFlags(t, contracts["install"], false, true, true, true)

	if contracts["add"].MutatesRuntime {
		t.Fatalf("add must not be the runtime sync command")
	}
	if contracts["remove"].MutatesRuntime {
		t.Fatalf("remove must not be the runtime sync command")
	}
	if !contracts["install"].MutatesRuntime {
		t.Fatalf("install must own managed runtime synchronization")
	}
	if contracts["install"].MutatesManifest {
		t.Fatalf("install must not mutate manifest intent")
	}
}

func TestCommandContractsCaptureRequiredIntentAndPruningRules(t *testing.T) {
	contracts := CommandContracts()

	if !strings.Contains(strings.ToLower(contracts["add"].Summary), "required intent") {
		t.Fatalf("add summary must mention required intent, got %q", contracts["add"].Summary)
	}
	if !strings.Contains(strings.ToLower(contracts["add"].LockfileEffect), "closure") {
		t.Fatalf("add lockfile effect must mention closure resolution, got %q", contracts["add"].LockfileEffect)
	}

	removeText := strings.ToLower(contracts["remove"].LockfileEffect)
	if !strings.Contains(removeText, "transitive") || !strings.Contains(removeText, "no longer") {
		t.Fatalf("remove lockfile effect must mention pruning no-longer-needed transitive dependencies, got %q", contracts["remove"].LockfileEffect)
	}
}

func TestInstallContractProtectsIgnoredAndUnmanagedContent(t *testing.T) {
	install := CommandContracts()["install"]
	guardrails := strings.ToLower(strings.Join(install.Guardrails, " "))
	runtimeEffect := strings.ToLower(install.RuntimeEffect)

	if !strings.Contains(guardrails, "delete everything not mentioned in the manifest") {
		t.Fatalf("install guardrails must explicitly reject delete-everything semantics: %#v", install.Guardrails)
	}
	if !strings.Contains(guardrails, "ignored") || !strings.Contains(guardrails, "unmanaged") {
		t.Fatalf("install guardrails must protect ignored and unmanaged content: %#v", install.Guardrails)
	}
	if !strings.Contains(runtimeEffect, "managed-scope") {
		t.Fatalf("install runtime effect must limit sync to managed scope, got %q", install.RuntimeEffect)
	}
}

func assertContractFlags(t *testing.T, contract CommandContract, wantManifest, wantLock, wantRuntime, wantObserved bool) {
	t.Helper()

	if contract.MutatesManifest != wantManifest {
		t.Fatalf("%s manifest mutation mismatch: got %v want %v", contract.Name, contract.MutatesManifest, wantManifest)
	}
	if contract.MutatesLockfile != wantLock {
		t.Fatalf("%s lockfile mutation mismatch: got %v want %v", contract.Name, contract.MutatesLockfile, wantLock)
	}
	if contract.MutatesRuntime != wantRuntime {
		t.Fatalf("%s runtime mutation mismatch: got %v want %v", contract.Name, contract.MutatesRuntime, wantRuntime)
	}
	if contract.ObservesRuntime != wantObserved {
		t.Fatalf("%s observed-state mismatch: got %v want %v", contract.Name, contract.ObservesRuntime, wantObserved)
	}
}
