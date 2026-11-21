package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
)

func TestVerifyPlanFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.tfplan")
	if err := os.WriteFile(p, []byte("reviewed plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	plan := &db.Operation{PlanFile: p, PlanSHA256: sum}
	if err := verifyPlanFile(plan); err != nil {
		t.Fatalf("unchanged plan refused: %v", err)
	}
	if err := os.WriteFile(p, []byte("tampered plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if verifyPlanFile(plan) == nil {
		t.Fatal("a changed plan file must be refused")
	}
	if verifyPlanFile(&db.Operation{PlanFile: p}) == nil {
		t.Fatal("a plan without a checksum must be refused")
	}
}
