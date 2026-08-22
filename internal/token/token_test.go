package token_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"labnexus/internal/token"
)

const secret = "test-secret"

func TestGenerateAndParse(t *testing.T) {
	access, err := token.GenerateAccessToken(secret, "user-1", "student", 15*time.Minute)
	require.NoError(t, err)

	claims, err := token.ParseAccessToken(secret, access)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "student", claims.Role)
	assert.False(t, claims.ExpiresAt.Time.IsZero())
}

func TestParse_WrongSecret(t *testing.T) {
	access, err := token.GenerateAccessToken(secret, "user-1", "student", 15*time.Minute)
	require.NoError(t, err)

	_, err = token.ParseAccessToken("other-secret", access)
	assert.Error(t, err)
}

func TestParse_Expired(t *testing.T) {
	access, err := token.GenerateAccessToken(secret, "user-1", "student", -time.Minute)
	require.NoError(t, err)

	_, err = token.ParseAccessToken(secret, access)
	assert.Error(t, err)
}

func TestParse_Garbage(t *testing.T) {
	_, err := token.ParseAccessToken(secret, "not-a-token")
	assert.Error(t, err)
}

func TestParse_RejectsOtherAlgorithms(t *testing.T) {
	// 用非 HS256 算法签发(如 none 攻击路径)应被拒绝
	bad := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"uid": "user-1"})
	signed, err := bad.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = token.ParseAccessToken(secret, signed)
	assert.Error(t, err)
}
