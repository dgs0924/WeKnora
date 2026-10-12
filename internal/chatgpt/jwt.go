package chatgpt

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type verifiedIdentity struct {
	subject string
	email   string
	plan    string
}

func (m *Manager) verifyIDToken(ctx context.Context, raw, clientID, nonce string) (verifiedIdentity, error) {
	if len(raw) == 0 || len(raw) > 128<<10 || !validIssuedClientID(clientID) {
		return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, errors.New("unexpected signing method")
		}
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" || len(kid) > 256 {
			return nil, errors.New("missing signing key id")
		}
		return m.signingKey(ctx, kid)
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(m.config.issuer),
		jwt.WithAudience(clientID),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(30*time.Second),
		jwt.WithTimeFunc(m.config.now),
	)
	if err != nil || token == nil || !token.Valid {
		return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
	}
	subject, ok := claimString(claims, "sub")
	if !ok || subject == "" || len(subject) > 1024 {
		return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
	}
	if _, ok := numericClaim(claims, "iat"); !ok {
		return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
	}
	if nonce != "" {
		actual, ok := claimString(claims, "nonce")
		if !ok || !constantTimeEqual(actual, nonce) {
			return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
		}
	}
	if azp, exists := claims["azp"]; exists {
		value, ok := azp.(string)
		if !ok || value != clientID {
			return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
		}
	}
	if audience, ok := claims["aud"].([]any); ok && len(audience) > 1 {
		azp, ok := claimString(claims, "azp")
		if !ok || azp != clientID {
			return verifiedIdentity{}, errors.New("ChatGPT identity verification failed")
		}
	}
	email, _ := claimString(claims, "email")
	if email == "" {
		email = nestedString(claims, "https://api.openai.com/profile", "email")
	}
	plan := nestedString(claims, "https://api.openai.com/auth", "chatgpt_plan_type")
	if plan == "" {
		plan = nestedString(claims, "https://api.openai.com/auth", "plan_type")
	}
	if plan == "" {
		plan, _ = claimString(claims, "plan")
	}
	return verifiedIdentity{subject: subject, email: bounded(email, 512), plan: bounded(plan, 256)}, nil
}

func (m *Manager) signingKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	m.jwksMu.Lock()
	defer m.jwksMu.Unlock()
	if key := m.jwks[kid]; key != nil && m.config.now().Sub(m.jwksLoadedAt) < time.Hour {
		return key, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.config.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	var document struct {
		Keys []struct {
			KID string `json:"kid"`
			KTY string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := m.doJSON(req, &document); err != nil {
		return nil, errors.New("ChatGPT signing keys are unavailable")
	}
	parsed := make(map[string]*rsa.PublicKey, len(document.Keys))
	for _, item := range document.Keys {
		if item.KID == "" || len(item.KID) > 256 || item.KTY != "RSA" ||
			(item.Use != "" && item.Use != "sig") || (item.Alg != "" && item.Alg != "RS256") {
			continue
		}
		if _, duplicate := parsed[item.KID]; duplicate {
			return nil, errors.New("ChatGPT signing key set is invalid")
		}
		key, err := parseRSAJWK(item.N, item.E)
		if err != nil {
			continue
		}
		parsed[item.KID] = key
	}
	if len(parsed) == 0 {
		return nil, errors.New("ChatGPT signing key set is invalid")
	}
	m.jwks = parsed
	m.jwksLoadedAt = m.config.now()
	key := parsed[kid]
	if key == nil {
		return nil, errors.New("ChatGPT signing key was not found")
	}
	return key, nil
}

func parseRSAJWK(modulus, exponent string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(modulus)
	if err != nil || len(nBytes) == 0 {
		return nil, errors.New("invalid RSA modulus")
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(exponent)
	if err != nil || len(eBytes) == 0 || len(eBytes) > 4 {
		return nil, errors.New("invalid RSA exponent")
	}
	e := 0
	for _, value := range eBytes {
		e = e<<8 | int(value)
	}
	key := &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}
	if key.N.BitLen() < 2048 || key.E < 3 || key.E%2 == 0 {
		return nil, errors.New("unsafe RSA key")
	}
	return key, nil
}

func claimString(claims jwt.MapClaims, name string) (string, bool) {
	value, ok := claims[name].(string)
	return value, ok
}

func numericClaim(claims jwt.MapClaims, name string) (int64, bool) {
	switch value := claims[name].(type) {
	case float64:
		return int64(value), value == float64(int64(value))
	case json.Number:
		n, err := value.Int64()
		return n, err == nil
	case int64:
		return value, true
	case string:
		n, err := strconv.ParseInt(value, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func nestedString(claims jwt.MapClaims, objectName, field string) string {
	object, ok := claims[objectName].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := object[field].(string)
	return value
}

func bounded(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return ""
	}
	return value
}
