// Package identity manages the emulator's persistent users and role hierarchy.
// Authentication supplies stable principals and effective roles to sessions.
package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
	"golang.org/x/crypto/bcrypt"
)

const (
	DemoAdminUser     = "ADMIN"
	DemoAdminPassword = "admin"
	RoleAccountAdmin  = "ACCOUNTADMIN"
	RoleSecurityAdmin = "SECURITYADMIN"
	RoleUserAdmin     = "USERADMIN"
	RoleSysAdmin      = "SYSADMIN"
	RolePublic        = "PUBLIC"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user disabled")
	ErrRoleInUse          = errors.New("role is in use")
	ErrSystemRole         = errors.New("system role cannot be modified")
	ErrRoleCycle          = errors.New("role grant would create a cycle")
	ErrRoleNotGranted     = errors.New("role is not granted to user")
)

type Principal struct {
	UserID        string
	Username      string
	DefaultRoleID string
	DefaultRole   string
}

type Service struct {
	repo *metadata.Repository
}

// ResolveActiveRole resolves the requested role from a user's effective role
// set. An empty request selects the user's configured default role. PUBLIC is
// effective for every user, including users without an explicit grant row.
func (s *Service) ResolveActiveRole(ctx context.Context, userID, requestedRole string) (*metadata.RoleRecord, error) {
	users, err := s.repo.ListUserRecords(ctx)
	if err != nil {
		return nil, err
	}
	var user *metadata.UserRecord
	for i := range users {
		if users[i].ID == userID {
			user = &users[i]
			break
		}
	}
	if user == nil {
		return nil, fmt.Errorf("%w: user", metadata.ErrIdentityNotFound)
	}
	roleName := NormalizeName(requestedRole)
	if roleName == "" {
		role, roleErr := s.roleByID(ctx, user.DefaultRoleID)
		if roleErr != nil {
			return nil, roleErr
		}
		roleName = role.Name
	}
	effective, err := s.EffectiveRoles(ctx, user.Name)
	if err != nil {
		return nil, err
	}
	for i := range effective {
		if effective[i].Name == roleName {
			return &effective[i], nil
		}
	}
	if _, err := s.repo.GetRoleRecordByName(ctx, roleName); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: %s", ErrRoleNotGranted, roleName)
}

// User is the public identity representation. Password hashes remain confined
// to the metadata repository and authentication path.
type User struct {
	ID                 string
	Name               string
	DefaultRoleID      string
	Disabled           bool
	MustChangePassword bool
	Comment            string
}

func NewService(ctx context.Context, repo *metadata.Repository) (*Service, error) {
	service := &Service{repo: repo}
	if err := service.bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("bootstrap identity catalog: %w", err)
	}
	return service, nil
}

func (s *Service) bootstrap(ctx context.Context) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(DemoAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash demo password: %w", err)
	}
	roles := []metadata.RoleRecord{
		{ID: uuid.NewString(), Name: RoleAccountAdmin, Comment: "System account administrator", SystemRole: true},
		{ID: uuid.NewString(), Name: RoleSecurityAdmin, Comment: "System security administrator", SystemRole: true},
		{ID: uuid.NewString(), Name: RoleUserAdmin, Comment: "System user administrator", SystemRole: true},
		{ID: uuid.NewString(), Name: RoleSysAdmin, Comment: "System object administrator", SystemRole: true},
		{ID: uuid.NewString(), Name: RolePublic, Comment: "Implicit role for every user", SystemRole: true},
	}
	// A parent inherits its child's privileges: GRANT ROLE child TO ROLE parent.
	hierarchy := [][2]string{
		{RoleSecurityAdmin, RoleAccountAdmin},
		{RoleUserAdmin, RoleSecurityAdmin},
		{RoleSysAdmin, RoleAccountAdmin},
		{RolePublic, RoleSysAdmin},
	}
	admin := metadata.UserRecord{ID: uuid.NewString(), Name: DemoAdminUser, PasswordHash: string(hash), Comment: "Local demonstration administrator"}
	return s.repo.BootstrapIdentity(ctx, roles, hierarchy, admin)
}

func NormalizeName(name string) string { return strings.ToUpper(strings.TrimSpace(name)) }

func (s *Service) Authenticate(ctx context.Context, username, password string) (*Principal, error) {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		if errors.Is(err, metadata.ErrIdentityNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if user.Disabled {
		return nil, ErrUserDisabled
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	role, err := s.roleByID(ctx, user.DefaultRoleID)
	if err != nil {
		return nil, err
	}
	return &Principal{UserID: user.ID, Username: user.Name, DefaultRoleID: role.ID, DefaultRole: role.Name}, nil
}

func (s *Service) CreateUser(ctx context.Context, name, password, defaultRole, comment string) (*User, error) {
	role, err := s.repo.GetRoleRecordByName(ctx, defaultRole)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user := &metadata.UserRecord{ID: uuid.NewString(), Name: NormalizeName(name), PasswordHash: string(hash), DefaultRoleID: role.ID, Comment: comment}
	if err := s.repo.CreateUserWithRoleRecord(ctx, *user); err != nil {
		return nil, err
	}
	created, err := s.repo.GetUserRecordByName(ctx, user.Name)
	if err != nil {
		return nil, err
	}
	result := publicUser(*created)
	return &result, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	records, err := s.repo.ListUserRecords(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(records))
	for _, record := range records {
		users = append(users, publicUser(record))
	}
	return users, nil
}

func (s *Service) ListRoles(ctx context.Context) ([]metadata.RoleRecord, error) {
	return s.repo.ListRoleRecords(ctx)
}

func (s *Service) SetPassword(ctx context.Context, username, password string) error {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user.PasswordHash = string(hash)
	return s.repo.UpdateUserRecord(ctx, *user)
}

func (s *Service) SetUserDisabled(ctx context.Context, username string, disabled bool) error {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	user.Disabled = disabled
	return s.repo.UpdateUserRecord(ctx, *user)
}

func (s *Service) SetDefaultRole(ctx context.Context, username, roleName string) error {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	return s.repo.SetDefaultUserRoleRecord(ctx, user.ID, role.ID)
}

func (s *Service) DeleteUser(ctx context.Context, username string) error {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	return s.repo.DeleteUserRecord(ctx, user.ID)
}

func (s *Service) CreateRole(ctx context.Context, name, comment string) (*metadata.RoleRecord, error) {
	return s.repo.CreateRoleRecord(ctx, NormalizeName(name), comment, false)
}

func (s *Service) GrantRoleToUser(ctx context.Context, roleName, username string) error {
	if NormalizeName(roleName) == RolePublic {
		return fmt.Errorf("%w: PUBLIC is implicit", ErrSystemRole)
	}
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	return s.repo.GrantRoleToUserRecord(ctx, user.ID, role.ID)
}

func (s *Service) RevokeRoleFromUser(ctx context.Context, roleName, username string) error {
	if NormalizeName(roleName) == RolePublic {
		return fmt.Errorf("%w: PUBLIC is implicit", ErrSystemRole)
	}
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	err = s.repo.RevokeRoleFromUserRecord(ctx, user.ID, role.ID)
	if errors.Is(err, metadata.ErrIdentityInUse) {
		return fmt.Errorf("%w: role is the user's default", ErrRoleInUse)
	}
	return err
}

func (s *Service) GrantRoleToRole(ctx context.Context, childName, parentName string) error {
	child, err := s.repo.GetRoleRecordByName(ctx, childName)
	if err != nil {
		return err
	}
	parent, err := s.repo.GetRoleRecordByName(ctx, parentName)
	if err != nil {
		return err
	}
	if child.ID == parent.ID {
		return fmt.Errorf("%w: self grant", ErrRoleCycle)
	}
	if child.Name == RolePublic || parent.Name == RolePublic {
		return fmt.Errorf("%w: PUBLIC hierarchy is reserved", ErrSystemRole)
	}
	err = s.repo.GrantRoleToRoleRecord(ctx, child.ID, parent.ID)
	if errors.Is(err, metadata.ErrIdentityCycle) {
		return ErrRoleCycle
	}
	return err
}

func (s *Service) RevokeRoleFromRole(ctx context.Context, childName, parentName string) error {
	child, err := s.repo.GetRoleRecordByName(ctx, childName)
	if err != nil {
		return err
	}
	parent, err := s.repo.GetRoleRecordByName(ctx, parentName)
	if err != nil {
		return err
	}
	if child.SystemRole || parent.SystemRole {
		return ErrSystemRole
	}
	return s.repo.RevokeRoleFromRoleRecord(ctx, child.ID, parent.ID)
}

func (s *Service) EffectiveRoles(ctx context.Context, username string) ([]metadata.RoleRecord, error) {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return nil, err
	}
	direct, err := s.repo.ListDirectUserRoleIDs(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	grants, err := s.repo.ListRoleGrantRecords(ctx)
	if err != nil {
		return nil, err
	}
	roles, err := s.repo.ListRoleRecords(ctx)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	var visit func(string)
	visit = func(parent string) {
		if wanted[parent] {
			return
		}
		wanted[parent] = true
		for _, grant := range grants {
			if grant.ParentRoleID == parent {
				visit(grant.ChildRoleID)
			}
		}
	}
	for _, id := range direct {
		visit(id)
	}
	for _, role := range roles {
		if role.Name == RolePublic {
			wanted[role.ID] = true
		}
	}
	result := make([]metadata.RoleRecord, 0, len(wanted))
	for _, role := range roles {
		if wanted[role.ID] {
			result = append(result, role)
		}
	}
	return result, nil
}

func (s *Service) DeleteRole(ctx context.Context, name string) error {
	role, err := s.repo.GetRoleRecordByName(ctx, name)
	if err != nil {
		return err
	}
	if role.SystemRole {
		return ErrSystemRole
	}
	err = s.repo.DeleteRoleRecord(ctx, role.ID)
	if errors.Is(err, metadata.ErrIdentityInUse) {
		return ErrRoleInUse
	}
	return err
}

func (s *Service) roleByID(ctx context.Context, id string) (*metadata.RoleRecord, error) {
	roles, err := s.repo.ListRoleRecords(ctx)
	if err != nil {
		return nil, err
	}
	for _, role := range roles {
		if role.ID == id {
			result := role
			return &result, nil
		}
	}
	return nil, fmt.Errorf("%w: default role", metadata.ErrIdentityNotFound)
}

func publicUser(record metadata.UserRecord) User {
	return User{
		ID:                 record.ID,
		Name:               record.Name,
		DefaultRoleID:      record.DefaultRoleID,
		Disabled:           record.Disabled,
		MustChangePassword: record.MustChangePassword,
		Comment:            record.Comment,
	}
}
