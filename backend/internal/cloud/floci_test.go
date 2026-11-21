package cloud

import (
	"context"
	"os"
	"testing"
)

// Integration test against a running Floci (docker compose up floci):
//
//	FLOCI_ENDPOINT=http://localhost:4566 go test ./internal/cloud -run Floci
func TestVerifyAgainstFloci(t *testing.T) {
	endpoint := os.Getenv("FLOCI_ENDPOINT")
	if endpoint == "" {
		t.Skip("set FLOCI_ENDPOINT to run against Floci")
	}
	creds := AWSCredentials{
		AccountID: "000000000000", AccessKeyID: "test", SecretAccessKey: "test",
		Region: "us-east-1", EndpointURL: endpoint,
	}
	arn, err := creds.Verify(context.Background())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	t.Logf("caller: %s", arn)

	creds.AccountID = "111111111111"
	if _, err := creds.Verify(context.Background()); err == nil {
		t.Error("expected an account mismatch error")
	}
}
