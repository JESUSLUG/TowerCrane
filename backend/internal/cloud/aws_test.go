package cloud

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := AWSCredentials{AccountID: "000000000000", AccessKeyID: "test", SecretAccessKey: "test", Region: "us-east-1", EndpointURL: "http://localhost:4566"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid Floci credentials rejected: %v", err)
	}
	bad := []AWSCredentials{
		{AccountID: "123", AccessKeyID: "a", SecretAccessKey: "b", Region: "us-east-1"},
		{AccountID: "000000000000", AccessKeyID: "", SecretAccessKey: "b", Region: "us-east-1"},
		{AccountID: "000000000000", AccessKeyID: "a", SecretAccessKey: "b", Region: "mars"},
		{AccountID: "000000000000", AccessKeyID: "a", SecretAccessKey: "b", Region: "us-east-1", EndpointURL: "file:///etc"},
	}
	for i, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestBaseEnvStripsCredentialsAndServerSecrets(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "server-secret")
	t.Setenv("JWT_SECRET", "jwt")
	t.Setenv("DB_PASSWORD", "db")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "pem")
	t.Setenv("TF_VAR_x", "1")
	t.Setenv("KEEP_ME", "yes")
	env := strings.Join(BaseEnv(), "\n")
	for _, leaked := range []string{"server-secret", "JWT_SECRET", "DB_PASSWORD", "GITHUB_APP_PRIVATE_KEY", "TF_VAR_x"} {
		if strings.Contains(env, leaked) {
			t.Errorf("%s leaked into tofu's environment", leaked)
		}
	}
	if !strings.Contains(env, "KEEP_ME=yes") {
		t.Error("unrelated variables must be kept")
	}
}

func TestMaskKey(t *testing.T) {
	if got := MaskKey("AKIAIOSFODNN7EXAMPLE"); got != "****************MPLE" {
		t.Errorf("MaskKey = %q", got)
	}
}
