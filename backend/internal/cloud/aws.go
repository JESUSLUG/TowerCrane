// Package cloud turns stored cloud accounts into what tofu needs to run.
package cloud

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AWSCredentials are static access-key credentials for one AWS account.
type AWSCredentials struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	// EndpointURL overrides every AWS endpoint (e.g. Floci at
	// http://localhost:4566). Empty for real AWS.
	EndpointURL string
}

var (
	accountIDRe = regexp.MustCompile(`^\d{12}$`)
	regionRe    = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)
)

// Validate checks the shape of the credentials before they are stored.
func (c AWSCredentials) Validate() error {
	switch {
	case !accountIDRe.MatchString(c.AccountID):
		return fmt.Errorf("AWS account ID must be 12 digits")
	case strings.TrimSpace(c.AccessKeyID) == "" || strings.TrimSpace(c.SecretAccessKey) == "":
		return fmt.Errorf("access key ID and secret access key are required")
	case !regionRe.MatchString(c.Region):
		return fmt.Errorf("invalid AWS region %q", c.Region)
	case c.EndpointURL != "" && !strings.HasPrefix(c.EndpointURL, "http://") && !strings.HasPrefix(c.EndpointURL, "https://"):
		return fmt.Errorf("endpoint URL must start with http:// or https://")
	}
	return nil
}

// Verify calls STS GetCallerIdentity with the credentials and checks they
// belong to the declared account. Returns the caller ARN.
func (c AWSCredentials) Verify(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(c.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, "")),
		// Never fall back to the server's own shared config/profile.
		config.WithSharedConfigFiles([]string{}),
		config.WithSharedCredentialsFiles([]string{}),
	)
	if err != nil {
		return "", err
	}
	client := sts.NewFromConfig(cfg, func(o *sts.Options) {
		if c.EndpointURL != "" {
			o.BaseEndpoint = aws.String(c.EndpointURL)
		}
	})

	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("AWS rejected the credentials: %w", err)
	}
	if got := aws.ToString(out.Account); got != c.AccountID {
		return "", fmt.Errorf("credentials belong to account %s, not %s", got, c.AccountID)
	}
	return aws.ToString(out.Arn), nil
}

// Env returns the environment variables tofu needs for these credentials.
func (c AWSCredentials) Env() []string {
	env := []string{
		"AWS_ACCESS_KEY_ID=" + c.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY=" + c.SecretAccessKey,
		"AWS_REGION=" + c.Region,
		"AWS_DEFAULT_REGION=" + c.Region,
	}
	if c.EndpointURL != "" {
		env = append(env, "AWS_ENDPOINT_URL="+c.EndpointURL)
	}
	return env
}

// BaseEnv is the server's environment with every cloud credential variable
// removed, so tofu can only ever use the credentials TowerCrane injects for
// the repository, never whatever the server process happens to have.
func BaseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "AWS_") || strings.HasPrefix(upper, "GOOGLE_") ||
			strings.HasPrefix(upper, "ARM_") || strings.HasPrefix(upper, "AZURE_") ||
			strings.HasPrefix(upper, "TF_VAR_") || upper == "JWT_SECRET" ||
			strings.HasPrefix(upper, "DB_") || strings.HasPrefix(upper, "GITHUB_") ||
			strings.HasPrefix(upper, "TOWERCRANE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// MaskKey shows only the last four characters of an access key ID.
func MaskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
}
