package auth

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	GitHubID    int64  `json:"github_id"`
	GitHubLogin string `json:"github_login"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
	jwt.RegisteredClaims
}

type User struct {
	ID          string
	GitHubID    sql.NullInt64
	GitHubLogin sql.NullString
	Email       string
	Name        string
	AvatarURL   sql.NullString
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type TokenResponse struct {
	Token     string    `json:"token"`
	User      User      `json:"user"`
	ExpiresAt time.Time `json:"expires_at"`
}

func GenerateJWT(user *User) (string, time.Time, error) {
	expiresAt := time.Now().Add(24 * time.Hour)

	// Get nullable values
	githubID := int64(0)
	if user.GitHubID.Valid {
		githubID = user.GitHubID.Int64
	}

	githubLogin := ""
	if user.GitHubLogin.Valid {
		githubLogin = user.GitHubLogin.String
	}

	avatarURL := ""
	if user.AvatarURL.Valid {
		avatarURL = user.AvatarURL.String
	}

	claims := Claims{
		GitHubID:    githubID,
		GitHubLogin: githubLogin,
		Email:       user.Email,
		Name:        user.Name,
		AvatarURL:   avatarURL,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   user.ID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
}

func VerifyJWT(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("JWT_SECRET")), nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	return claims, nil
}

func NewUser(gitHubID int64, gitHubLogin, email, name, avatarURL string) *User {
	return &User{
		ID:          uuid.New().String(),
		GitHubID:    sql.NullInt64{Int64: gitHubID, Valid: true},
		GitHubLogin: sql.NullString{String: gitHubLogin, Valid: true},
		Email:       email,
		Name:        name,
		AvatarURL:   sql.NullString{String: avatarURL, Valid: true},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// knownPlaceholders are example JWT secrets from docs and .env templates:
// anyone could sign admin tokens with them.
var knownPlaceholders = []string{
	"your-secret-key-change-this-in-production",
	"your-secret-key-change-in-production",
	"your-secret-key",
	"changeme",
	"secret",
}

// CheckJWTSecret refuses secrets anyone could guess: with JWT_SECRET, anyone
// can mint a token for any user, admins included.
func CheckJWTSecret() error {
	s := os.Getenv("JWT_SECRET")
	for _, p := range knownPlaceholders {
		if strings.EqualFold(strings.TrimSpace(s), p) {
			return fmt.Errorf("JWT_SECRET is an example value; set a random one, e.g. `openssl rand -base64 48`")
		}
	}
	if len(s) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters (got %d); e.g. `openssl rand -base64 48`", len(s))
	}
	return nil
}
