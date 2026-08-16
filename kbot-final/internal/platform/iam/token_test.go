package iam

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestValidateTokenRequiresHS256AndIssuer(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	service := &Service{jwtKey: key}

	sign := func(method jwt.SigningMethod, issuer string) string {
		t.Helper()
		claims := &Claims{
			UserID: "user-1",
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    issuer,
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		}
		token, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	if _, err := service.ValidateToken(sign(jwt.SigningMethodHS256, "kbot")); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if _, err := service.ValidateToken(sign(jwt.SigningMethodHS512, "kbot")); err == nil {
		t.Fatal("HS512 token accepted")
	}
	if _, err := service.ValidateToken(sign(jwt.SigningMethodHS256, "other")); err == nil {
		t.Fatal("token with wrong issuer accepted")
	}
}
