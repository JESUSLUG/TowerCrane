//go:build unix

package iac

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunTimeoutStopsTheCommand(t *testing.T) {
	t.Setenv("TOWERCRANE_RUN_TIMEOUT", "200ms")
	e := NewExecutor(ExecutorConfig{})
	start := time.Now()
	err := e.run(exec.Command("sh", "-c", "sleep 30 & wait"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "TOWERCRANE_RUN_TIMEOUT") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the command was not stopped")
	}
}
