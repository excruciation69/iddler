package platform

// Vault keeps secrets (session tokens, .maFile secrets) in one AES-GCM encrypted file.
// Only its 32-byte master key goes into the OS secure storage, which keeps that part short and reliable.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type keyStore interface {
	load() (string, error)
	save(hexKey string) error
}

type vault struct {
	mu   sync.Mutex
	path string
	keys keyStore
	key  []byte
	data map[string]string
	// legacy lets old storage be read once and migrated
	legacy func(key string) (string, error)
}

func newVault(dir string, keys keyStore, legacy func(string) (string, error)) *vault {
	return &vault{path: filepath.Join(dir, "secrets.bin"), keys: keys, legacy: legacy}
}

func (v *vault) open() error {
	if v.data != nil {
		return nil
	}
	if v.key == nil {
		h, err := v.keys.load()
		if err != nil || len(h) != 64 {
			if _, statErr := os.Stat(v.path); statErr == nil {
				return errors.New("vault key is missing")
			}
			k := make([]byte, 32)
			rand.Read(k)
			h = hex.EncodeToString(k)
			if err := v.keys.save(h); err != nil {
				return err
			}
		}
		v.key, _ = hex.DecodeString(h)
	}
	v.data = map[string]string{}
	raw, err := os.ReadFile(v.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	gcm, err := v.gcm()
	if err != nil || len(raw) < gcm.NonceSize() {
		return errors.New("vault corrupted")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return errors.New("vault cannot be decrypted")
	}
	return json.Unmarshal(plain, &v.data)
}

func (v *vault) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (v *vault) flush() error {
	plain, _ := json.Marshal(v.data)
	gcm, err := v.gcm()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	os.MkdirAll(filepath.Dir(v.path), 0o700)
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, gcm.Seal(nonce, nonce, plain, nil), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

func (v *vault) Get(key string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.open(); err != nil {
		return "", err
	}
	if s, ok := v.data[key]; ok {
		return s, nil
	}
	if v.legacy != nil {
		if s, err := v.legacy(key); err == nil && s != "" {
			v.data[key] = s
			v.flush()
			return s, nil
		}
	}
	return "", os.ErrNotExist
}

func (v *vault) Set(key, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.open(); err != nil {
		return err
	}
	v.data[key] = value
	return v.flush()
}

func (v *vault) Delete(key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.open(); err != nil {
		return err
	}
	delete(v.data, key)
	return v.flush()
}

var (
	vaultsMu sync.Mutex
	vaults   = map[string]*vault{}
)

// sharedVault returns one vault per data directory.
func sharedVault(dir string, keys keyStore, legacy func(string) (string, error)) *vault {
	vaultsMu.Lock()
	defer vaultsMu.Unlock()
	if v := vaults[dir]; v != nil {
		return v
	}
	v := newVault(dir, keys, legacy)
	vaults[dir] = v
	return v
}

// fileKey keeps the master key next to the vault (used where no OS key store exists).
type fileKey struct{ path string }

func (f fileKey) load() (string, error) {
	b, err := os.ReadFile(f.path)
	return string(b), err
}

func (f fileKey) save(h string) error {
	os.MkdirAll(filepath.Dir(f.path), 0o700)
	return os.WriteFile(f.path, []byte(h), 0o600)
}
