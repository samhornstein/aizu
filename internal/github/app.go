// Package github provides GitHub API client and GitHub App authentication.
//
// GitHub App authentication flow:
//  1. Sign a JWT with the app's private key (ES256)
//  2. Exchange the JWT for an installation access token
//  3. Use the installation token for API calls (auto-refreshed before expiry)
package github

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AppAuth manages GitHub App authentication: JWT signing, installation token
// fetching, and automatic token rotation.
type AppAuth struct {
	appID        int64
	privateKey   *ecdsa.PrivateKey
	installID    int64
	http         *http.Client
	mu           sync.Mutex
	token        string
	expiresAt    time.Time
	refreshAfter time.Time // refresh when current time passes this
}

// NewAppAuth creates an AppAuth that will auto-refresh installation tokens.
// privateKeyPEM is the app's PEM-encoded EC private key (ES256).
func NewAppAuth(appID int64, privateKeyPEM string, installID int64) (*AppAuth, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return &AppAuth{
		appID:      appID,
		privateKey: key,
		installID:  installID,
		http:       &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Token returns a valid installation access token, refreshing automatically
// if the current token is expired or near expiry.
func (a *AppAuth) Token() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if time.Now().Before(a.refreshAfter) {
		return a.token
	}
	return a.refreshLocked()
}

func (a *AppAuth) refreshLocked() string {
	if a.token == "" || time.Now().After(a.expiresAt) {
		// Token is empty or fully expired — must refresh.
		token, expiresAt, err := a.fetchInstallTokenLocked()
		if err != nil {
			// If we have a token, keep using it. Otherwise return empty.
			if a.token == "" {
				return ""
			}
			return a.token
		}
		a.token = token
		a.expiresAt = expiresAt
		// Refresh 5 minutes before expiry.
		a.refreshAfter = expiresAt.Add(-5 * time.Minute)
	}
	return a.token
}

func (a *AppAuth) fetchInstallTokenLocked() (string, time.Time, error) {
	jwt, err := a.signJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign JWT: %w", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBase, a.installID),
		nil,
	)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", time.Time{}, fmt.Errorf("fetch install token: %s -> %d: %s",
			req.URL.Path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("decode install token response: %w", err)
	}

	return result.Token, result.ExpiresAt, nil
}

// signJWT creates a short-lived JWT signed with the app's private key (ES256).
// The JWT is valid for 10 minutes (GitHub requires ≤10 min).
func (a *AppAuth) signJWT() (string, error) {
	now := time.Now()
	header := map[string]any{
		"alg": "ES256",
		"typ": "JWT",
	}
	claims := map[string]any{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": a.appID,
	}

	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)

	// JWT = base64url(header).base64url(claims).base64url(signature)
	hb64 := base64URL(hb)
	cb64 := base64URL(cb)
	signingInput := hb64 + "." + cb64

	sig, err := ecdsaSignASN1(a.privateKey, []byte(signingInput))
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}

	return signingInput + "." + base64URL(sig), nil
}

// parsePrivateKey parses a PEM-encoded EC private key (ES256 / P-256).
func parsePrivateKey(pemData string) (*ecdsa.PrivateKey, error) {
	block, rest := pem.Decode([]byte(pemData))
	if block == nil {
		// Try without leading/trailing whitespace issues.
		block, _ = pem.Decode([]byte(strings.TrimSpace(pemData)))
		if block == nil {
			return nil, fmt.Errorf("no PEM block found (expected -----BEGIN EC PRIVATE KEY----- or -----BEGIN PRIVATE KEY-----)")
		}
	}
	_ = rest // silence unused warning

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		// PKCS#8 format
		ecKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is not ECDSA")
		}
		return ecKey, nil
	}

	// Try PKCS#1 EC format
	key2, err := x509.ParseECPrivateKey(block.Bytes)
	if err == nil {
		return key2, nil
	}

	return nil, fmt.Errorf("failed to parse private key: %v", err)
}

// ecdsaSignASN1 signs data using ECDSA with P-256, returning the DER-encoded
// ASN.1 signature (SEQUENCE { INTEGER r, INTEGER s }).
func ecdsaSignASN1(key *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return nil, err
	}
	return encodeASN1Signature(r, s), nil
}

// encodeASN1Signature encodes two big.Int values as a DER SEQUENCE { INTEGER, INTEGER }.
func encodeASN1Signature(r, s *big.Int) []byte {
	rBytes := encodeASN1Integer(r)
	sBytes := encodeASN1Integer(s)
	inner := make([]byte, 0, 2+len(rBytes)+len(sBytes))
	inner = append(inner, rBytes...)
	inner = append(inner, sBytes...)
	return append([]byte{0x30, byte(len(inner))}, inner...)
}

// encodeASN1Integer encodes a big.Int as a DER INTEGER.
func encodeASN1Integer(n *big.Int) []byte {
	b := n.Bytes()
	// DER INTEGER must have high bit 0 (positive). Prepend 0x00 if high bit is set.
	if len(b) > 0 && b[0]&0x80 != 0 {
		b = append([]byte{0x00}, b...)
	}
	return append([]byte{0x02, byte(len(b))}, b...)
}

// base64URL encodes bytes to base64url without padding.
func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
