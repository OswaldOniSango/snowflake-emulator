// Package identity manages the emulator's persistent users and role hierarchy.
// Authentication supplies stable principals and effective roles to sessions.
package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	grantTargetUser   = "USER"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user disabled")
	ErrRoleInUse          = errors.New("role is in use")
	ErrSystemRole         = errors.New("system role cannot be modified")
	ErrRoleCycle          = errors.New("role grant would create a cycle")
	ErrRoleNotGranted     = errors.New("role is not granted to user")
	ErrPrivilegeDenied    = errors.New("insufficient privileges")
)

const (
	PrivilegeUsage       = "USAGE"
	PrivilegeOperate     = "OPERATE"
	PrivilegeSelect      = "SELECT"
	PrivilegeInsert      = "INSERT"
	PrivilegeUpdate      = "UPDATE"
	PrivilegeDelete      = "DELETE"
	PrivilegeCreateTable = "CREATE TABLE"
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

// WithRepository returns a service view bound to the supplied repository.
// It is used by pinned procedure execution so authorization uses the same
// DuckDB connection instead of waiting on the outer connection pool.
func (s *Service) WithRepository(repo *metadata.Repository) *Service {
	if s == nil {
		return nil
	}
	clone := *s
	clone.repo = repo
	return &clone
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

// UserChanges is one atomic ALTER USER operation. Nil fields are unchanged.
type UserChanges struct {
	Password    *string
	DefaultRole *string
	Disabled    *bool
	Comment     *string
}

// RoleAssignment describes one direct role grant for SHOW GRANTS output.
type RoleAssignment struct {
	RoleName  string
	GrantedTo string
	Grantee   string
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
	return s.CreateUserConfigured(ctx, name, password, defaultRole, false, comment)
}

// CreateUserConfigured creates the complete user row and default grant in one transaction.
func (s *Service) CreateUserConfigured(ctx context.Context, name, password, defaultRole string, disabled bool, comment string) (*User, error) {
	role, err := s.repo.GetRoleRecordByName(ctx, defaultRole)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	user := &metadata.UserRecord{ID: uuid.NewString(), Name: NormalizeName(name), PasswordHash: string(hash), DefaultRoleID: role.ID, Disabled: disabled, Comment: comment}
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

// AlterUser validates all requested values before committing one catalog transaction.
func (s *Service) AlterUser(ctx context.Context, username string, changes UserChanges) error {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	if changes.DefaultRole != nil {
		role, roleErr := s.repo.GetRoleRecordByName(ctx, *changes.DefaultRole)
		if roleErr != nil {
			return roleErr
		}
		user.DefaultRoleID = role.ID
	}
	if changes.Password != nil {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(*changes.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return fmt.Errorf("hash password: %w", hashErr)
		}
		user.PasswordHash = string(hash)
	}
	if changes.Disabled != nil {
		user.Disabled = *changes.Disabled
	}
	if changes.Comment != nil {
		user.Comment = *changes.Comment
	}
	return s.repo.UpdateUserConfigurationRecord(ctx, *user)
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

func (s *Service) SetUserComment(ctx context.Context, username, comment string) error {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return err
	}
	user.Comment = comment
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

// DirectGrantsToUser returns direct assignments plus PUBLIC, which is implicit.
func (s *Service) DirectGrantsToUser(ctx context.Context, username string) ([]RoleAssignment, error) {
	user, err := s.repo.GetUserRecordByName(ctx, username)
	if err != nil {
		return nil, err
	}
	ids, err := s.repo.ListDirectUserRoleIDs(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	roles, err := s.repo.ListRoleRecords(ctx)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	assignments := make([]RoleAssignment, 0, len(ids)+1)
	for _, role := range roles {
		if wanted[role.ID] || role.Name == RolePublic {
			assignments = append(assignments, RoleAssignment{RoleName: role.Name, GrantedTo: grantTargetUser, Grantee: user.Name})
		}
	}
	return assignments, nil
}

// DirectGrantsToRole returns child roles directly granted to a parent role.
func (s *Service) DirectGrantsToRole(ctx context.Context, roleName string) ([]RoleAssignment, error) {
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return nil, err
	}
	return s.roleAssignments(ctx, func(grant metadata.RoleGrantRecord) (string, string, bool) {
		return grant.ChildRoleID, grant.ParentRoleID, grant.ParentRoleID == role.ID
	})
}

// DirectGrantsOfRole returns users and roles that directly received a role.
func (s *Service) DirectGrantsOfRole(ctx context.Context, roleName string) ([]RoleAssignment, error) {
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return nil, err
	}
	assignments, err := s.roleAssignments(ctx, func(grant metadata.RoleGrantRecord) (string, string, bool) {
		return role.ID, grant.ParentRoleID, grant.ChildRoleID == role.ID
	})
	if err != nil {
		return nil, err
	}
	users, err := s.repo.ListUserRecords(ctx)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		if role.Name == RolePublic {
			assignments = append(assignments, RoleAssignment{RoleName: role.Name, GrantedTo: grantTargetUser, Grantee: user.Name})
			continue
		}
		ids, listErr := s.repo.ListDirectUserRoleIDs(ctx, user.ID)
		if listErr != nil {
			return nil, listErr
		}
		for _, id := range ids {
			if id == role.ID {
				assignments = append(assignments, RoleAssignment{RoleName: role.Name, GrantedTo: grantTargetUser, Grantee: user.Name})
			}
		}
	}
	slices.SortFunc(assignments, func(a, b RoleAssignment) int {
		return strings.Compare(a.GrantedTo+":"+a.Grantee, b.GrantedTo+":"+b.Grantee)
	})
	return assignments, nil
}

func (s *Service) roleAssignments(ctx context.Context, selectGrant func(metadata.RoleGrantRecord) (string, string, bool)) ([]RoleAssignment, error) {
	roles, err := s.repo.ListRoleRecords(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]string, len(roles))
	for _, role := range roles {
		byID[role.ID] = role.Name
	}
	grants, err := s.repo.ListRoleGrantRecords(ctx)
	if err != nil {
		return nil, err
	}
	var result []RoleAssignment
	for _, grant := range grants {
		childID, grantee, selected := selectGrant(grant)
		if selected {
			result = append(result, RoleAssignment{RoleName: byID[childID], GrantedTo: "ROLE", Grantee: byID[grantee]})
		}
	}
	slices.SortFunc(result, func(a, b RoleAssignment) int {
		return strings.Compare(a.RoleName+":"+a.Grantee, b.RoleName+":"+b.Grantee)
	})
	return result, nil
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
	if child.SystemRole || parent.SystemRole {
		return fmt.Errorf("%w: system role hierarchy is reserved", ErrSystemRole)
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

// RoleByID resolves a persisted role for internal owner-context execution.
func (s *Service) RoleByID(ctx context.Context, id string) (*metadata.RoleRecord, error) {
	return s.roleByID(ctx, id)
}

// WarehouseGrantsToRole returns direct warehouse privileges granted to a role.
func (s *Service) WarehouseGrantsToRole(ctx context.Context, roleName string) ([]metadata.WarehousePrivilegeRecord, error) {
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return nil, err
	}
	grants, err := s.repo.ListWarehousePrivilegeRecords(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]metadata.WarehousePrivilegeRecord, 0)
	for _, grant := range grants {
		if grant.RoleID == role.ID {
			result = append(result, grant)
		}
	}
	return result, nil
}

// GrantWarehousePrivilege persists a direct warehouse privilege for a role.
func (s *Service) GrantWarehousePrivilege(ctx context.Context, privilege, warehouseName, roleName string) error {
	privilege = strings.ToUpper(privilege)
	if privilege != PrivilegeUsage && privilege != PrivilegeOperate {
		return fmt.Errorf("unsupported warehouse privilege %s", privilege)
	}
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	return s.repo.GrantWarehousePrivilegeRecord(ctx, role.ID, warehouseName, privilege)
}

func (s *Service) RevokeWarehousePrivilege(ctx context.Context, privilege, warehouseName, roleName string) error {
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	return s.repo.RevokeWarehousePrivilegeRecord(ctx, role.ID, warehouseName, strings.ToUpper(privilege))
}

// AuthorizeWarehouse checks the active role and every role it inherits.
func (s *Service) AuthorizeWarehouse(ctx context.Context, activeRoleID, warehouseName, privilege string) error {
	active, err := s.roleByID(ctx, activeRoleID)
	if err != nil {
		return err
	}
	if active.Name == RoleAccountAdmin {
		return nil
	}
	grants, err := s.repo.ListRoleGrantRecords(ctx)
	if err != nil {
		return err
	}
	effective := map[string]bool{}
	var visit func(string)
	visit = func(roleID string) {
		if effective[roleID] {
			return
		}
		effective[roleID] = true
		for _, grant := range grants {
			if grant.ParentRoleID == roleID {
				visit(grant.ChildRoleID)
			}
		}
	}
	visit(activeRoleID)
	privileges, err := s.repo.ListWarehousePrivilegeRecords(ctx)
	if err != nil {
		return err
	}
	for _, grant := range privileges {
		if effective[grant.RoleID] && strings.EqualFold(grant.WarehouseName, warehouseName) && strings.EqualFold(grant.Privilege, privilege) {
			return nil
		}
	}
	return fmt.Errorf("%w: role %s lacks %s on warehouse %s", ErrPrivilegeDenied, active.Name, strings.ToUpper(privilege), strings.ToUpper(warehouseName))
}

func (s *Service) GrantObjectPrivilege(ctx context.Context, privilege, objectType, objectName, roleName string) error {
	if err := validateObjectPrivilege(privilege, objectType); err != nil {
		return err
	}
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	return s.repo.GrantObjectPrivilegeRecord(ctx, role.ID, objectType, objectName, privilege)
}

func (s *Service) RevokeObjectPrivilege(ctx context.Context, privilege, objectType, objectName, roleName string) error {
	if err := validateObjectPrivilege(privilege, objectType); err != nil {
		return err
	}
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return err
	}
	return s.repo.RevokeObjectPrivilegeRecord(ctx, role.ID, objectType, objectName, privilege)
}

func validateObjectPrivilege(privilege, objectType string) error {
	privilege, objectType = strings.ToUpper(strings.TrimSpace(privilege)), strings.ToUpper(strings.TrimSpace(objectType))
	valid := (privilege == PrivilegeUsage && (objectType == "DATABASE" || objectType == "SCHEMA")) ||
		(privilege == PrivilegeCreateTable && objectType == "SCHEMA") ||
		((privilege == PrivilegeSelect || privilege == PrivilegeInsert || privilege == PrivilegeUpdate || privilege == PrivilegeDelete) && objectType == "TABLE")
	if !valid {
		return fmt.Errorf("privilege %s is not valid on %s", privilege, objectType)
	}
	return nil
}

func (s *Service) ObjectGrantsToRole(ctx context.Context, roleName string) ([]metadata.ObjectPrivilegeRecord, error) {
	role, err := s.repo.GetRoleRecordByName(ctx, roleName)
	if err != nil {
		return nil, err
	}
	grants, err := s.repo.ListObjectPrivilegeRecords(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]metadata.ObjectPrivilegeRecord, 0)
	for _, grant := range grants {
		if grant.RoleID == role.ID {
			result = append(result, grant)
		}
	}
	return result, nil
}

func (s *Service) AuthorizeObject(ctx context.Context, activeRoleID, objectType, objectName, privilege string) error {
	active, err := s.roleByID(ctx, activeRoleID)
	if err != nil {
		return err
	}
	if active.Name == RoleAccountAdmin {
		return nil
	}
	roleGrants, err := s.repo.ListRoleGrantRecords(ctx)
	if err != nil {
		return err
	}
	effective := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if effective[id] {
			return
		}
		effective[id] = true
		for _, grant := range roleGrants {
			if grant.ParentRoleID == id {
				visit(grant.ChildRoleID)
			}
		}
	}
	visit(activeRoleID)
	grants, err := s.repo.ListObjectPrivilegeRecords(ctx)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if effective[grant.RoleID] && strings.EqualFold(grant.ObjectType, objectType) && strings.EqualFold(grant.ObjectName, objectName) && strings.EqualFold(grant.Privilege, privilege) {
			return nil
		}
	}
	return fmt.Errorf("%w: role %s lacks %s on %s %s", ErrPrivilegeDenied, active.Name, strings.ToUpper(privilege), strings.ToUpper(objectType), strings.ToUpper(objectName))
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
