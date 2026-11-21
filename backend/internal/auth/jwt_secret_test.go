package auth

import "testing"

func TestCheckJWTSecret(t *testing.T) {
	for _, bad := range []string{"", "short", "your-secret-key-change-this-in-production", "  YOUR-SECRET-KEY  "} {
		t.Setenv("JWT_SECRET", bad)
		if CheckJWTSecret() == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	t.Setenv("JWT_SECRET", "kq3n8Vx2Lr0Zp9Tm4Ws7Yb1Hc6Fd5Ge0Ja2Ku")
	if err := CheckJWTSecret(); err != nil {
		t.Error(err)
	}
}
