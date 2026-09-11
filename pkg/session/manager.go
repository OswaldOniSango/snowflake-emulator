// Package session provides session management for the Snowflake emulator.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Session represents an active Snowflake session.
type Session struct {
	ID                      int64
	Token                   string
	MasterToken             string
	Username                string
	UserID                  string
	ActiveRoleID            string
	ActiveRole              string
	Database                string
	CurrentSchema           string
	Warehouse               string
	CreatedAt               time.Time
	LastAccessedAt          time.Time
	ExpiresAt               time.Time
	ValidityInSeconds       int64
	MasterValidityInSeconds int64
	Parameters              map[string]interface{}
}

// CreateInput contains the authenticated identity and initial SQL context.
type CreateInput struct {
	UserID, Username, ActiveRoleID, ActiveRole string
	Database, Schema, Warehouse                string
}

// Manager manages Snowflake sessions.
type Manager struct {
	sessions       map[string]*Session // token -> session
	masterTokens   map[string]*Session // masterToken -> session
	sessionTimeout time.Duration
	mu             sync.RWMutex
	store          *Store // optional persistent storage
}

// NewManager creates a new session manager.
func NewManager(sessionTimeout time.Duration) *Manager {
	return &Manager{
		sessions:       make(map[string]*Session),
		masterTokens:   make(map[string]*Session),
		sessionTimeout: sessionTimeout,
	}
}

// CreateSession creates a new session with a unique token.
func (m *Manager) CreateSession(ctx context.Context, username, database, schema string) (*Session, error) {
	return m.CreateAuthenticatedSession(ctx, CreateInput{Username: username, Database: database, Schema: schema})
}

// CreateAuthenticatedSession creates a session for an already authenticated principal.
func (m *Manager) CreateAuthenticatedSession(ctx context.Context, input CreateInput) (*Session, error) {
	username, database, schema := input.Username, input.Database, input.Schema
	if username == "" {
		return nil, fmt.Errorf("username cannot be empty")
	}
	if database == "" {
		return nil, fmt.Errorf("database cannot be empty")
	}
	if schema == "" {
		return nil, fmt.Errorf("schema cannot be empty")
	}

	// Generate unique int64 session ID using timestamp
	sessionID := time.Now().UnixNano()

	// Generate secure random token
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	// Generate master token
	masterToken, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate master token: %w", err)
	}

	now := time.Now()
	session := &Session{
		ID:                      sessionID,
		Token:                   token,
		MasterToken:             masterToken,
		Username:                username,
		UserID:                  input.UserID,
		ActiveRoleID:            input.ActiveRoleID,
		ActiveRole:              input.ActiveRole,
		Database:                database,
		CurrentSchema:           schema,
		Warehouse:               input.Warehouse,
		CreatedAt:               now,
		LastAccessedAt:          now,
		ExpiresAt:               now.Add(m.sessionTimeout),
		ValidityInSeconds:       int64(m.sessionTimeout.Seconds()),
		MasterValidityInSeconds: int64(m.sessionTimeout.Seconds()) * 4,
		Parameters:              make(map[string]interface{}),
	}

	m.mu.Lock()
	m.sessions[token] = session
	m.masterTokens[masterToken] = session
	m.mu.Unlock()

	// Persist to store if available
	if m.store != nil {
		if err := m.store.Save(ctx, session); err != nil {
			// Remove from memory if persistence failed
			m.mu.Lock()
			delete(m.sessions, token)
			delete(m.masterTokens, masterToken)
			m.mu.Unlock()
			return nil, fmt.Errorf("failed to persist session: %w", err)
		}
	}

	return session.Copy(), nil
}

// SetWarehouse records the warehouse selected during login.
func (m *Manager) SetWarehouse(token, warehouse string) error {
	return m.SetWarehouseContext(context.Background(), token, warehouse)
}

// SetWarehouseContext records and persists the selected warehouse.
func (m *Manager) SetWarehouseContext(ctx context.Context, token, warehouse string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[token]
	if !ok {
		return fmt.Errorf("session not found")
	}
	oldWarehouse := session.Warehouse
	session.Warehouse = warehouse
	if err := m.save(ctx, session); err != nil {
		session.Warehouse = oldWarehouse
		return err
	}
	return nil
}

// SetActiveRole changes a session role after the caller validates its grant.
func (m *Manager) SetActiveRole(ctx context.Context, token, roleID, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[token]
	if !ok {
		return fmt.Errorf("session not found")
	}
	oldID, oldRole := sess.ActiveRoleID, sess.ActiveRole
	sess.ActiveRoleID, sess.ActiveRole = roleID, role
	if err := m.save(ctx, sess); err != nil {
		sess.ActiveRoleID, sess.ActiveRole = oldID, oldRole
		return err
	}
	return nil
}

func (m *Manager) save(ctx context.Context, sess *Session) error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(ctx, sess)
}

// ValidateSession validates a session token and returns the session if valid.
// It also updates the LastAccessedAt timestamp.
func (m *Manager) ValidateSession(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, fmt.Errorf("token cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[token]
	if !exists {
		return nil, fmt.Errorf("invalid session token")
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		// Remove expired session
		delete(m.sessions, token)
		return nil, fmt.Errorf("session expired")
	}

	// Persist the touch before exposing it in memory, so a restart observes the
	// same session activity that callers observed.
	now := time.Now()
	if m.store != nil {
		if err := m.store.Touch(ctx, token, now); err != nil {
			return nil, fmt.Errorf("failed to persist session activity: %w", err)
		}
	}
	session.LastAccessedAt = now

	return session.Copy(), nil
}

// CloseSession closes a session (logout).
func (m *Manager) CloseSession(ctx context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get session to find master token
	session, exists := m.sessions[token]
	if exists {
		// Delete both session token and master token
		delete(m.sessions, token)
		delete(m.masterTokens, session.MasterToken)
	}

	// Delete from store if available
	if m.store != nil {
		if err := m.store.Delete(ctx, token); err != nil {
			return fmt.Errorf("failed to delete session from store: %w", err)
		}
	}

	return nil
}

// UpdateSessionContext updates the database and/or schema for a session.
func (m *Manager) UpdateSessionContext(ctx context.Context, token, database, schema string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[token]
	if !exists {
		return fmt.Errorf("invalid session token")
	}
	oldDatabase, oldSchema, oldAccess := session.Database, session.CurrentSchema, session.LastAccessedAt

	// Update database if provided
	if database != "" {
		session.Database = database
	}

	// Update schema if provided
	if schema != "" {
		session.CurrentSchema = schema
	}

	session.LastAccessedAt = time.Now()
	if err := m.save(ctx, session); err != nil {
		session.Database, session.CurrentSchema, session.LastAccessedAt = oldDatabase, oldSchema, oldAccess
		return err
	}
	return nil
}

// CleanupExpiredSessions removes all expired sessions and returns the count.
func (m *Manager) CleanupExpiredSessions(_ context.Context) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	count := 0

	for token, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			delete(m.sessions, token)
			count++
		}
	}

	return count
}

// RenewToken generates a new session token using master token
func (m *Manager) RenewToken(ctx context.Context, masterToken string) (*Session, string, error) {
	if masterToken == "" {
		return nil, "", fmt.Errorf("master token cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.masterTokens[masterToken]
	if !exists {
		return nil, "", fmt.Errorf("invalid master token")
	}

	// Check master token expiry (4x session timeout)
	masterExpiry := session.CreatedAt.Add(time.Duration(session.MasterValidityInSeconds) * time.Second)
	if time.Now().After(masterExpiry) {
		delete(m.masterTokens, masterToken)
		delete(m.sessions, session.Token)
		return nil, "", fmt.Errorf("master token expired")
	}

	// Construct and persist the replacement before changing the in-memory
	// indexes. Store.ReplaceToken performs the durable swap atomically.
	oldToken := session.Token
	newToken, err := generateToken()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate new token: %w", err)
	}

	replacement := session.Copy()
	replacement.Token = newToken
	replacement.LastAccessedAt = time.Now()
	replacement.ExpiresAt = time.Now().Add(m.sessionTimeout)
	if m.store != nil {
		if err := m.store.ReplaceToken(ctx, oldToken, replacement); err != nil {
			return nil, "", err
		}
	}
	delete(m.sessions, oldToken)
	*session = *replacement
	m.sessions[newToken] = session

	return session.Copy(), newToken, nil
}

// UpdateLastAccessed updates the last accessed time for a session (heartbeat)
func (m *Manager) UpdateLastAccessed(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[token]
	if !exists {
		return fmt.Errorf("session not found")
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		delete(m.sessions, token)
		return fmt.Errorf("session expired")
	}

	now := time.Now()
	if m.store != nil {
		if err := m.store.Touch(ctx, token, now); err != nil {
			return fmt.Errorf("failed to persist session activity: %w", err)
		}
	}
	session.LastAccessedAt = now

	return nil
}

// Copy creates a deep copy of the session.
func (s *Session) Copy() *Session {
	if s == nil {
		return nil
	}

	// Copy parameters map
	params := make(map[string]interface{})
	for k, v := range s.Parameters {
		params[k] = v
	}

	return &Session{
		ID:                      s.ID,
		Token:                   s.Token,
		MasterToken:             s.MasterToken,
		Username:                s.Username,
		UserID:                  s.UserID,
		ActiveRoleID:            s.ActiveRoleID,
		ActiveRole:              s.ActiveRole,
		Database:                s.Database,
		CurrentSchema:           s.CurrentSchema,
		Warehouse:               s.Warehouse,
		CreatedAt:               s.CreatedAt,
		LastAccessedAt:          s.LastAccessedAt,
		ExpiresAt:               s.ExpiresAt,
		ValidityInSeconds:       s.ValidityInSeconds,
		MasterValidityInSeconds: s.MasterValidityInSeconds,
		Parameters:              params,
	}
}

// generateToken generates a secure random token.
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
