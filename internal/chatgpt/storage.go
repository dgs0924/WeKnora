package chatgpt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	credentialVersion = 1
	maxStoredJSON     = 1 << 20
)

type hostRecord struct {
	Version int    `json:"version"`
	HostID  string `json:"host_id"`
}

type credential struct {
	Version      int       `json:"version"`
	UserHash     string    `json:"user_hash"`
	HostID       string    `json:"host_id"`
	ClientID     string    `json:"client_id"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email,omitempty"`
	Plan         string    `json:"plan,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	GrantedScope string    `json:"granted_scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	ConnectedAt  time.Time `json:"connected_at"`
}

func validateUserID(userID string) (string, error) {
	if strings.TrimSpace(userID) == "" || len(userID) > 1024 {
		return "", ErrInvalidLocalUser
	}
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:]), nil
}

func (m *Manager) userDir(userHash string) string {
	return filepath.Join(m.storageDir, "users", userHash)
}

func (m *Manager) credentialPath(userHash string) string {
	return filepath.Join(m.userDir(userHash), "credential.json")
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private storage: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private storage: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private storage path is not a directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure private storage: %w", err)
	}
	return nil
}

func readPrivateJSON(path string, dst any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxStoredJSON {
		return errors.New("stored credential is not a valid private file")
	}
	if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure stored credential: %w", err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, maxStoredJSON+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("stored credential is invalid")
	}
	if err := ensureJSONEOF(dec); err != nil {
		return err
	}
	return nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("stored credential contains trailing data")
	}
	return nil
}

func atomicWriteJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".credential-*")
	if err != nil {
		return fmt.Errorf("create credential file: %w", err)
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("secure credential file: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return fmt.Errorf("encode credential file: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync credential file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close credential file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace credential file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure credential file: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	ok = true
	return nil
}

func (m *Manager) loadOrCreateHost() (hostRecord, error) {
	path := filepath.Join(m.storageDir, "host.json")
	var record hostRecord
	err := readPrivateJSON(path, &record)
	if err == nil {
		if record.Version != credentialVersion || !validHostID(record.HostID) {
			return hostRecord{}, errors.New("stored ChatGPT host identity is invalid")
		}
		return record, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return hostRecord{}, err
	}
	record = hostRecord{Version: credentialVersion, HostID: "urn:uuid:" + uuid.NewString()}
	if err := atomicWriteJSON(path, record); err != nil {
		return hostRecord{}, err
	}
	return record, nil
}

func validHostID(value string) bool {
	const prefix = "urn:uuid:"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(value, prefix))
	return err == nil
}

func (m *Manager) loadCredential(userHash string) (*credential, error) {
	var value credential
	err := readPrivateJSON(m.credentialPath(userHash), &value)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if value.Version != credentialVersion || value.UserHash != userHash || value.HostID != m.host.HostID ||
		!validIssuedClientID(value.ClientID) || value.Subject == "" || value.AccessToken == "" ||
		value.RefreshToken == "" || value.IDToken == "" || value.ExpiresAt.IsZero() || !hasRequiredScopes(value.GrantedScope) {
		return nil, errors.New("stored ChatGPT credential is invalid")
	}
	return &value, nil
}

func (m *Manager) saveCredential(value *credential) error {
	return atomicWriteJSON(m.credentialPath(value.UserHash), value)
}

func (m *Manager) deleteCredential(userHash string) error {
	path := m.credentialPath(userHash)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove ChatGPT credential: %w", err)
	}
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
