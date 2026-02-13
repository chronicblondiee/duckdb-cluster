package security

import (
	"context"
	"fmt"
)

// Permission represents a specific permission
type Permission string

const (
	// Query permissions
	PermissionQueryRead   Permission = "query:read"
	PermissionQueryWrite  Permission = "query:write"
	PermissionQueryDelete Permission = "query:delete"
	
	// Admin permissions
	PermissionAdminShards  Permission = "admin:shards"
	PermissionAdminTables  Permission = "admin:tables"
	PermissionAdminStats   Permission = "admin:stats"
	PermissionAdminHealth  Permission = "admin:health"
	PermissionAdminUsers   Permission = "admin:users"
	
	// System permissions
	PermissionSystemConfig Permission = "system:config"
	PermissionSystemShutdown Permission = "system:shutdown"
)

// Role represents a named set of permissions
type Role struct {
	Name        string
	Permissions []Permission
}

// Predefined roles
var (
	RoleAdmin = &Role{
		Name: "admin",
		Permissions: []Permission{
			PermissionQueryRead,
			PermissionQueryWrite,
			PermissionQueryDelete,
			PermissionAdminShards,
			PermissionAdminTables,
			PermissionAdminStats,
			PermissionAdminHealth,
			PermissionAdminUsers,
			PermissionSystemConfig,
			PermissionSystemShutdown,
		},
	}
	
	RoleWriter = &Role{
		Name: "writer",
		Permissions: []Permission{
			PermissionQueryRead,
			PermissionQueryWrite,
			PermissionAdminTables,
			PermissionAdminStats,
			PermissionAdminHealth,
		},
	}
	
	RoleReader = &Role{
		Name: "reader",
		Permissions: []Permission{
			PermissionQueryRead,
			PermissionAdminTables,
			PermissionAdminStats,
			PermissionAdminHealth,
		},
	}
	
	RoleReadOnly = &Role{
		Name: "read",
		Permissions: []Permission{
			PermissionQueryRead,
			PermissionAdminHealth,
		},
	}
)

// Authorizer manages authorization decisions
type Authorizer struct {
	roles map[string]*Role
}

// NewAuthorizer creates a new authorizer with default roles
func NewAuthorizer() *Authorizer {
	return &Authorizer{
		roles: map[string]*Role{
			"admin":  RoleAdmin,
			"writer": RoleWriter,
			"reader": RoleReader,
			"read":   RoleReadOnly,
		},
	}
}

// RegisterRole registers a custom role
func (a *Authorizer) RegisterRole(role *Role) {
	a.roles[role.Name] = role
}

// Authorize checks if a user has the required permission
func (a *Authorizer) Authorize(user *User, permission Permission) error {
	if user == nil {
		return fmt.Errorf("unauthorized: no user provided")
	}
	
	// Check each of the user's roles
	for _, roleName := range user.Roles {
		role, exists := a.roles[roleName]
		if !exists {
			continue
		}
		
		// Check if role has the required permission
		for _, perm := range role.Permissions {
			if perm == permission {
				return nil // Authorized
			}
		}
	}
	
	return fmt.Errorf("unauthorized: user '%s' lacks permission '%s'", user.Username, permission)
}

// AuthorizeContext authorizes using user from context
func (a *Authorizer) AuthorizeContext(ctx context.Context, permission Permission) error {
	user, ok := UserFromContext(ctx)
	if !ok {
		return fmt.Errorf("unauthorized: no user in context")
	}
	return a.Authorize(user, permission)
}

// HasPermission checks if a user has a specific permission
func (a *Authorizer) HasPermission(user *User, permission Permission) bool {
	return a.Authorize(user, permission) == nil
}

// HasAnyPermission checks if a user has any of the specified permissions
func (a *Authorizer) HasAnyPermission(user *User, permissions ...Permission) bool {
	for _, perm := range permissions {
		if a.HasPermission(user, perm) {
			return true
		}
	}
	return false
}

// HasAllPermissions checks if a user has all of the specified permissions
func (a *Authorizer) HasAllPermissions(user *User, permissions ...Permission) bool {
	for _, perm := range permissions {
		if !a.HasPermission(user, perm) {
			return false
		}
	}
	return true
}

// GetUserPermissions returns all permissions for a user
func (a *Authorizer) GetUserPermissions(user *User) []Permission {
	permSet := make(map[Permission]bool)
	
	for _, roleName := range user.Roles {
		role, exists := a.roles[roleName]
		if !exists {
			continue
		}
		
		for _, perm := range role.Permissions {
			permSet[perm] = true
		}
	}
	
	permissions := make([]Permission, 0, len(permSet))
	for perm := range permSet {
		permissions = append(permissions, perm)
	}
	
	return permissions
}
