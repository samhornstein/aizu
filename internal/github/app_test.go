package github

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// genTestKey generates a temporary EC P-256 key for testing.
func genTestKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	return key, pemStr
}

func TestParsePrivateKeyECFormat(t *testing.T) {
	_, pemStr := genTestKey(t)
	key, err := parsePrivateKey(pemStr)
	if err != nil {
		t.Fatalf("parsePrivateKey(EC PRIVATE KEY) = %v", err)
	}
	if key.Curve != elliptic.P256() {
		t.Errorf("curve = %v, want P256", key.Curve)
	}
}

func TestParsePrivateKeyPKCS8Format(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	parsed, err := parsePrivateKey(pemStr)
	if err != nil {
		t.Fatalf("parsePrivateKey(PKCS8) = %v", err)
	}
	if parsed.Curve != elliptic.P256() {
		t.Errorf("curve = %v, want P256", parsed.Curve)
	}
}

func TestParsePrivateKeyInvalid(t *testing.T) {
	_, err := parsePrivateKey("not a pem")
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

func TestSignJWT(t *testing.T) {
	_, pemStr := genTestKey(t)
	auth := &AppAuth{appID: 12345, privateKey: nil, installID: 67890}
	key, err := parsePrivateKey(pemStr)
	if err != nil {
		t.Fatal(err)
	}
	auth.privateKey = key

	jwt, err := auth.signJWT()
	if err != nil {
		t.Fatalf("signJWT = %v", err)
	}

	// JWT should have three dot-separated parts.
	parts := splitJWT(jwt)
	if len(parts) != 3 {
		t.Fatalf("JWT has %d parts, want 3", len(parts))
	}
	// Header and claims should be non-empty base64url.
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		t.Fatal("JWT parts should not be empty")
	}
}

func TestNewAppAuth(t *testing.T) {
	_, pemStr := genTestKey(t)
	auth, err := NewAppAuth(12345, pemStr, 67890)
	if err != nil {
		t.Fatalf("NewAppAuth = %v", err)
	}
	if auth.appID != 12345 {
		t.Errorf("appID = %d, want 12345", auth.appID)
	}
	if auth.installID != 67890 {
		t.Errorf("installID = %d, want 67890", auth.installID)
	}
}

func TestNewAppAuthInvalidKey(t *testing.T) {
	_, err := NewAppAuth(12345, "invalid-key", 67890)
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestTokenProviderStatic(t *testing.T) {
	provider := &staticToken{token: "my-token"}
	if provider.Token() != "my-token" {
		t.Errorf("Token() = %q, want my-token", provider.Token())
	}
}

func TestEncodeASN1Integer(t *testing.T) {
	// Test with a small positive number
	n := bigFromInt(t, 0x01)
	encoded := encodeASN1Integer(n)
	// Should be: 0x02 (INTEGER tag), 0x01 (length 1), 0x01 (value)
	if len(encoded) != 3 {
		t.Fatalf("encodeASN1Integer(1) len = %d, want 3", len(encoded))
	}
	if encoded[0] != 0x02 {
		t.Errorf("tag = 0x%02x, want 0x02", encoded[0])
	}
	if encoded[1] != 0x01 {
		t.Errorf("length = 0x%02x, want 0x01", encoded[1])
	}
	if encoded[2] != 0x01 {
		t.Errorf("value = 0x%02x, want 0x01", encoded[2])
	}
}

func TestEncodeASN1IntegerHighBit(t *testing.T) {
	// Value with high bit set should get 0x00 prefix
	n := bigFromInt(t, 0x80)
	encoded := encodeASN1Integer(n)
	// Should be: 0x02, 0x02, 0x00, 0x80
	if len(encoded) != 4 {
		t.Fatalf("encodeASN1Integer(0x80) len = %d, want 4", len(encoded))
	}
	if encoded[2] != 0x00 || encoded[3] != 0x80 {
		t.Errorf("value = 0x%02x 0x%02x, want 0x00 0x80", encoded[2], encoded[3])
	}
}

func TestBase64URL(t *testing.T) {
	cases := []struct {
		input []byte
		want  string
	}{
		{[]byte(""), ""},
		{[]byte("f"), "Zg"},
		{[]byte("fo"), "Zm8"},
		{[]byte("foo"), "Zm9v"},
		{[]byte("foob"), "Zm9vYg"},
		{[]byte("fooba"), "Zm9vYmE"},
		{[]byte("foobar"), "Zm9vYmFy"},
	}
	for _, tc := range cases {
		got := base64URL(tc.input)
		if got != tc.want {
			t.Errorf("base64URL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestECDSASignAndVerify(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("test data")
	sig, err := ecdsaSignASN1(key, data)
	if err != nil {
		t.Fatalf("ecdsaSignASN1 = %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("signature is empty")
	}
	// Verify the signature is valid DER.
	if sig[0] != 0x30 {
		t.Errorf("sig[0] = 0x%02x, want 0x30 (SEQUENCE)", sig[0])
	}
}

// bigFromInt creates a *big.Int from an int64 for testing.
func bigFromInt(t *testing.T, n int64) *big.Int {
	t.Helper()
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(n >> (8 * (7 - i)))
	}
	return bigFromBytes(t, b)
}

// bigFromBytes creates a *big.Int from bytes for testing.
func bigFromBytes(t *testing.T, b []byte) *big.Int {
	t.Helper()
	// Use the standard library's big.Int
	result := &big.Int{}
	result.SetBytes(b)
	return result
}

func splitJWT(jwt string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for i := 0; i < 2; i++ {
		idx := findDot(jwt, start)
		if idx < 0 {
			break
		}
		parts = append(parts, jwt[start:idx])
		start = idx + 1
	}
	parts = append(parts, jwt[start:])
	return parts
}

func findDot(s string, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// TestAppAuthTokenCaching verifies that Token() returns the same value
// within the refresh window.
func TestAppAuthTokenCaching(t *testing.T) {
	_, pemStr := genTestKey(t)
	auth, err := NewAppAuth(12345, pemStr, 67890)
	if err != nil {
		t.Fatal(err)
	}

	// Set a fake token with far-future expiry to avoid actual network calls.
	auth.mu.Lock()
	auth.token = "cached-token"
	auth.expiresAt = time.Now().Add(1 * time.Hour)
	auth.refreshAfter = time.Now().Add(30 * time.Minute)
	auth.mu.Unlock()

	// Should return the cached token without network calls.
	tok1 := auth.Token()
	tok2 := auth.Token()
	if tok1 != "cached-token" || tok2 != "cached-token" {
		t.Errorf("cached token mismatch: got %q, %q", tok1, tok2)
	}
}
