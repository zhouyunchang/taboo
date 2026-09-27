package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

const envelopePrefix = "taboo:v1:"

// Context uses immutable IDs, never mutable display names or folder paths.
// Version identifies the immutable secret version (1 for singleton credentials).
type Context struct {
	Purpose     string
	Org         string
	Project     string
	Environment string
	Object      string
	Version     int
}

func (c Context) aad() ([]byte, error) {
	if c.Purpose == "" || c.Object == "" || c.Version < 1 {
		return nil, errors.New("invalid encryption context")
	}
	return json.Marshal(c)
}
func envelopeAEAD(key []byte, c Context) (cipher.AEAD, error) {
	if len(key) != KeyLen {
		return nil, errors.New("invalid envelope key")
	}
	k, err := hkdf.Key(sha256.New, key, nil, "taboo/envelope/v1/"+c.Purpose, KeyLen)
	if err != nil {
		return nil, err
	}
	b, err := aes.NewCipher(k)
	clear(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func Seal(key []byte, plaintext string, c Context) (string, error) {
	aad, err := c.aad()
	if err != nil {
		return "", err
	}
	gcm, err := envelopeAEAD(key, c)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return envelopePrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), aad)), nil
}

// Open permits the documented legacy nonce+ciphertext envelope only when there
// is no version marker. Invalid/unknown marked envelopes never downgrade.
func Open(key []byte, payload string, c Context) (string, error) {
	if !strings.HasPrefix(payload, "taboo:") {
		return Decrypt(key, payload)
	}
	if !strings.HasPrefix(payload, envelopePrefix) {
		return "", errors.New("unsupported envelope version")
	}
	aad, err := c.aad()
	if err != nil {
		return "", err
	}
	gcm, err := envelopeAEAD(key, c)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(payload, envelopePrefix))
	if err != nil || len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("invalid ciphertext")
	}
	pt, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], aad)
	if err != nil {
		return "", errors.New("ciphertext authentication failed")
	}
	return string(pt), nil
}

func SecretContext(org, project, env, secret string, version int) Context {
	return Context{Purpose: "secret", Org: org, Project: project, Environment: env, Object: secret, Version: version}
}
func DEKContext(org string) Context {
	return Context{Purpose: "org-dek", Org: org, Object: org, Version: 1}
}
