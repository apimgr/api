package crypto

import (
	"strings"
	"testing"
)

func TestBcryptHashVerify(t *testing.T) {
	hash, err := BcryptHash("s3cret!", 4)
	if err != nil {
		t.Fatalf("BcryptHash error: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("BcryptHash() = %q, want bcrypt prefix", hash)
	}
	if !BcryptVerify("s3cret!", hash) {
		t.Errorf("BcryptVerify() should succeed for correct password")
	}
	if BcryptVerify("wrong", hash) {
		t.Errorf("BcryptVerify() should fail for incorrect password")
	}

	// out-of-range cost clamps to 12
	hash2, err := BcryptHash("s3cret!", 1)
	if err != nil {
		t.Fatalf("BcryptHash(cost=1) error: %v", err)
	}
	if cost := BcryptCost(hash2); cost != 12 {
		t.Errorf("BcryptCost() after clamp = %d, want 12", cost)
	}

	hash3, err := BcryptHash("s3cret!", 40)
	if err != nil {
		t.Fatalf("BcryptHash(cost=40) error: %v", err)
	}
	if cost := BcryptCost(hash3); cost != 12 {
		t.Errorf("BcryptCost() after clamp = %d, want 12", cost)
	}
}

func TestBcryptCostInvalidHash(t *testing.T) {
	if cost := BcryptCost("not-a-bcrypt-hash"); cost != 0 {
		t.Errorf("BcryptCost(invalid) = %d, want 0", cost)
	}
}

func TestGeneratePassword(t *testing.T) {
	opts := DefaultPasswordOptions()
	pw, err := GeneratePassword(20, opts)
	if err != nil {
		t.Fatalf("GeneratePassword error: %v", err)
	}
	if len(pw) != 20 {
		t.Errorf("GeneratePassword(20) len = %d, want 20", len(pw))
	}

	// length < 4 clamps to 16
	pw, err = GeneratePassword(1, opts)
	if err != nil {
		t.Fatalf("GeneratePassword(1) error: %v", err)
	}
	if len(pw) != 16 {
		t.Errorf("GeneratePassword(1) len = %d, want clamped 16", len(pw))
	}

	// length > 256 clamps to 256
	pw, err = GeneratePassword(1000, opts)
	if err != nil {
		t.Fatalf("GeneratePassword(1000) error: %v", err)
	}
	if len(pw) != 256 {
		t.Errorf("GeneratePassword(1000) len = %d, want clamped 256", len(pw))
	}

	// no charset options selected falls back to alnum default charset
	pw, err = GeneratePassword(30, PasswordOptions{})
	if err != nil {
		t.Fatalf("GeneratePassword(no opts) error: %v", err)
	}
	if len(pw) != 30 {
		t.Errorf("GeneratePassword(no opts) len = %d, want 30", len(pw))
	}
	for _, r := range pw {
		if strings.ContainsRune("!@#$%^&*()_+-=[]{}|;:,.<>?", r) {
			t.Errorf("GeneratePassword(no opts) produced symbol %q, want alnum only", r)
		}
	}

	// numbers-only charset with similar characters excluded should never
	// contain 0 or 1
	numOnly := PasswordOptions{Numbers: true, ExcludeSimilar: true}
	pw, err = GeneratePassword(200, numOnly)
	if err != nil {
		t.Fatalf("GeneratePassword(numOnly) error: %v", err)
	}
	if strings.ContainsAny(pw, "01") {
		t.Errorf("GeneratePassword(ExcludeSimilar) = %q, contains excluded 0/1", pw)
	}
}

func TestGeneratePIN(t *testing.T) {
	pin, err := GeneratePIN(6)
	if err != nil {
		t.Fatalf("GeneratePIN error: %v", err)
	}
	if len(pin) != 6 {
		t.Errorf("GeneratePIN(6) len = %d, want 6", len(pin))
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			t.Errorf("GeneratePIN() = %q contains non-digit", pin)
		}
	}

	// length < 3 clamps to 4
	pin, err = GeneratePIN(1)
	if err != nil {
		t.Fatalf("GeneratePIN(1) error: %v", err)
	}
	if len(pin) != 4 {
		t.Errorf("GeneratePIN(1) len = %d, want clamped 4", len(pin))
	}

	// length > 12 clamps to 12
	pin, err = GeneratePIN(50)
	if err != nil {
		t.Fatalf("GeneratePIN(50) error: %v", err)
	}
	if len(pin) != 12 {
		t.Errorf("GeneratePIN(50) len = %d, want clamped 12", len(pin))
	}
}

func TestRandomBytes(t *testing.T) {
	b, err := RandomBytes(16)
	if err != nil {
		t.Fatalf("RandomBytes error: %v", err)
	}
	if len(b) != 16 {
		t.Errorf("RandomBytes(16) len = %d, want 16", len(b))
	}

	// count <= 0 clamps to 32
	b, err = RandomBytes(0)
	if err != nil {
		t.Fatalf("RandomBytes(0) error: %v", err)
	}
	if len(b) != 32 {
		t.Errorf("RandomBytes(0) len = %d, want clamped 32", len(b))
	}

	// count > 10000 clamps to 10000
	b, err = RandomBytes(20000)
	if err != nil {
		t.Fatalf("RandomBytes(20000) error: %v", err)
	}
	if len(b) != 10000 {
		t.Errorf("RandomBytes(20000) len = %d, want clamped 10000", len(b))
	}

	// two successive calls should not be identical
	b1, _ := RandomBytes(32)
	b2, _ := RandomBytes(32)
	if string(b1) == string(b2) {
		t.Errorf("RandomBytes() produced identical output on successive calls")
	}
}

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret(20)
	if err != nil {
		t.Fatalf("GenerateTOTPSecret error: %v", err)
	}
	if len(secret) == 0 {
		t.Fatalf("GenerateTOTPSecret() returned empty secret")
	}

	// length < 16 clamps to 20, length > 64 clamps to 64: verify no error
	if _, err := GenerateTOTPSecret(4); err != nil {
		t.Errorf("GenerateTOTPSecret(4) error: %v", err)
	}
	if _, err := GenerateTOTPSecret(1000); err != nil {
		t.Errorf("GenerateTOTPSecret(1000) error: %v", err)
	}

	code, err := GenerateTOTP(secret, 6, 30)
	if err != nil {
		t.Fatalf("GenerateTOTP error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("GenerateTOTP() = %q, want length 6", code)
	}

	if !VerifyTOTP(secret, code, 6, 30, 1) {
		t.Errorf("VerifyTOTP() should accept a freshly generated code")
	}

	// mutate the last digit to guarantee a mismatching code
	wrongDigit := byte('1')
	if code[len(code)-1] == '1' {
		wrongDigit = '2'
	}
	wrongCode := code[:len(code)-1] + string(wrongDigit)
	if VerifyTOTP(secret, wrongCode, 6, 30, 1) {
		t.Errorf("VerifyTOTP() accepted a wrong code %q for correct %q", wrongCode, code)
	}

	// digits clamps: <6 -> 6, >8 -> 8
	code8, err := GenerateTOTP(secret, 20, 30)
	if err != nil {
		t.Fatalf("GenerateTOTP(digits=20) error: %v", err)
	}
	if len(code8) != 8 {
		t.Errorf("GenerateTOTP(digits=20) = %q, want length 8 (clamped)", code8)
	}

	code6, err := GenerateTOTP(secret, 1, 30)
	if err != nil {
		t.Fatalf("GenerateTOTP(digits=1) error: %v", err)
	}
	if len(code6) != 6 {
		t.Errorf("GenerateTOTP(digits=1) = %q, want length 6 (clamped)", code6)
	}

	if _, err := GenerateTOTP("not valid base32!!!", 6, 30); err == nil {
		t.Errorf("GenerateTOTP() with invalid secret should error")
	}
}

func TestVerifyTOTPWrongSecret(t *testing.T) {
	secret, _ := GenerateTOTPSecret(20)
	if VerifyTOTP("invalid-secret-!!", "123456", 6, 30, 1) {
		t.Errorf("VerifyTOTP() with an undecodable secret should fail closed")
	}
	if VerifyTOTP(secret, "", 6, 30, 1) {
		t.Errorf("VerifyTOTP() with an empty code should fail")
	}
}

func TestGenerateTOTPURI(t *testing.T) {
	uri := GenerateTOTPURI("JBSWY3DPEHPK3PXP", "MyApp", "user@example.com")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Errorf("GenerateTOTPURI() = %q, want otpauth prefix", uri)
	}
	if !strings.Contains(uri, "secret=JBSWY3DPEHPK3PXP") {
		t.Errorf("GenerateTOTPURI() missing secret param: %q", uri)
	}
	if !strings.Contains(uri, "issuer=MyApp") {
		t.Errorf("GenerateTOTPURI() missing issuer param: %q", uri)
	}
}

func TestHMACGenerate(t *testing.T) {
	sig1, err := HMACGenerate("sha256", "key", "message")
	if err != nil {
		t.Fatalf("HMACGenerate(sha256) error: %v", err)
	}
	if len(sig1) != 64 {
		t.Errorf("HMACGenerate(sha256) = %q, want 64 hex chars", sig1)
	}

	sig2, err := HMACGenerate("sha256", "key", "message")
	if err != nil || sig1 != sig2 {
		t.Errorf("HMACGenerate() not deterministic: %q vs %q", sig1, sig2)
	}

	sig3, err := HMACGenerate("sha256", "key", "different message")
	if err != nil || sig1 == sig3 {
		t.Errorf("HMACGenerate() produced same signature for different messages")
	}

	sig4, err := HMACGenerate("sha1", "key", "message")
	if err != nil {
		t.Fatalf("HMACGenerate(sha1) error: %v", err)
	}
	if len(sig4) != 40 {
		t.Errorf("HMACGenerate(sha1) = %q, want 40 hex chars", sig4)
	}

	if _, err := HMACGenerate("md5", "key", "message"); err == nil {
		t.Errorf("HMACGenerate(md5) should be unsupported and error")
	}
}

func TestPasswordStrength(t *testing.T) {
	weak := PasswordStrength("abc")
	if weak["strength"] != "weak" {
		t.Errorf("PasswordStrength(abc)[strength] = %v, want weak", weak["strength"])
	}
	if weak["has_uppercase"] != false || weak["has_numbers"] != false {
		t.Errorf("PasswordStrength(abc) = %v, unexpected charset flags", weak)
	}

	strong := PasswordStrength("Tr0ub4dor&3xtraLongPassphrase!!")
	if strong["strength"] == "weak" {
		t.Errorf("PasswordStrength(long complex) should not be weak: %v", strong)
	}
	if strong["has_uppercase"] != true || strong["has_lowercase"] != true ||
		strong["has_numbers"] != true || strong["has_symbols"] != true {
		t.Errorf("PasswordStrength() charset flags wrong: %v", strong)
	}

	empty := PasswordStrength("")
	if empty["length"] != 0 || empty["strength"] != "weak" {
		t.Errorf("PasswordStrength(empty) = %v, want length 0 and weak", empty)
	}
}

func TestGenerateRSAKeys(t *testing.T) {
	priv, pub, err := GenerateRSAKeys(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeys error: %v", err)
	}
	if !strings.Contains(priv, "RSA PRIVATE KEY") {
		t.Errorf("GenerateRSAKeys() private key missing PEM header: %q", priv)
	}
	if !strings.Contains(pub, "PUBLIC KEY") {
		t.Errorf("GenerateRSAKeys() public key missing PEM header: %q", pub)
	}

	// bits below 2048 clamps up to 2048 rather than erroring
	priv2, pub2, err := GenerateRSAKeys(512)
	if err != nil {
		t.Fatalf("GenerateRSAKeys(512) error: %v", err)
	}
	if priv2 == "" || pub2 == "" {
		t.Errorf("GenerateRSAKeys(512) returned empty keys")
	}
}

func TestGenerateECDSAKeys(t *testing.T) {
	priv, pub, err := GenerateECDSAKeys()
	if err != nil {
		t.Fatalf("GenerateECDSAKeys error: %v", err)
	}
	if !strings.Contains(priv, "EC PRIVATE KEY") {
		t.Errorf("GenerateECDSAKeys() private key missing PEM header: %q", priv)
	}
	if !strings.Contains(pub, "PUBLIC KEY") {
		t.Errorf("GenerateECDSAKeys() public key missing PEM header: %q", pub)
	}
}

func TestAESEncryptDecryptRoundTrip(t *testing.T) {
	plaintext := "the quick brown fox jumps over the lazy dog"
	ciphertext, err := AESEncrypt(plaintext, "correct horse battery staple")
	if err != nil {
		t.Fatalf("AESEncrypt error: %v", err)
	}
	if ciphertext == "" {
		t.Fatalf("AESEncrypt() returned empty ciphertext")
	}

	decrypted, err := AESDecrypt(ciphertext, "correct horse battery staple")
	if err != nil {
		t.Fatalf("AESDecrypt error: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("AESDecrypt() = %q, want %q", decrypted, plaintext)
	}

	// wrong key must fail to decrypt
	if _, err := AESDecrypt(ciphertext, "wrong key"); err == nil {
		t.Errorf("AESDecrypt() with wrong key should error")
	}

	// empty plaintext should round-trip too
	emptyCipher, err := AESEncrypt("", "key")
	if err != nil {
		t.Fatalf("AESEncrypt(empty) error: %v", err)
	}
	emptyPlain, err := AESDecrypt(emptyCipher, "key")
	if err != nil || emptyPlain != "" {
		t.Errorf("AESDecrypt(AESEncrypt(empty)) = %q, %v, want empty", emptyPlain, err)
	}
}

func TestAESDecryptInvalidInput(t *testing.T) {
	if _, err := AESDecrypt("not valid base64!!", "key"); err == nil {
		t.Errorf("AESDecrypt() with invalid base64 should error")
	}
	if _, err := AESDecrypt("YQ==", "key"); err == nil {
		t.Errorf("AESDecrypt() with too-short ciphertext should error")
	}
}

func TestRSAEncryptDecryptRoundTrip(t *testing.T) {
	priv, pub, err := GenerateRSAKeys(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeys error: %v", err)
	}

	plaintext := "small secret"
	ciphertext, err := RSAEncrypt(plaintext, pub)
	if err != nil {
		t.Fatalf("RSAEncrypt error: %v", err)
	}

	decrypted, err := RSADecrypt(ciphertext, priv)
	if err != nil {
		t.Fatalf("RSADecrypt error: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("RSADecrypt() = %q, want %q", decrypted, plaintext)
	}
}

func TestRSAEncryptDecryptErrors(t *testing.T) {
	if _, err := RSAEncrypt("data", "not a pem key"); err == nil {
		t.Errorf("RSAEncrypt() with invalid PEM should error")
	}

	priv, pub, err := GenerateRSAKeys(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeys error: %v", err)
	}

	if _, err := RSADecrypt("not a pem key", priv); err == nil {
		t.Errorf("RSADecrypt() with invalid PEM private key should error")
	}

	ciphertext, err := RSAEncrypt("data", pub)
	if err != nil {
		t.Fatalf("RSAEncrypt error: %v", err)
	}
	if _, err := RSADecrypt("not-base64!!", priv); err == nil {
		t.Errorf("RSADecrypt() with invalid base64 ciphertext should error")
	}

	// decrypting with a mismatched private key should fail
	otherPriv, _, err := GenerateRSAKeys(2048)
	if err != nil {
		t.Fatalf("GenerateRSAKeys error: %v", err)
	}
	if _, err := RSADecrypt(ciphertext, otherPriv); err == nil {
		t.Errorf("RSADecrypt() with mismatched private key should error")
	}
}
