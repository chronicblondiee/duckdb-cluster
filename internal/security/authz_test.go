package security

import (
	"context"
	"testing"
)

func TestNewAuthorizer(t *testing.T) {
	authz := NewAuthorizer()
	if authz == nil {
		t.Fatal("NewAuthorizer() returned nil")
	}
	
	// Check that default roles are registered
	if _, exists := authz.roles["admin"]; !exists {
		t.Error("admin role not registered")
	}
	if _, exists := authz.roles["writer"]; !exists {
		t.Error("writer role not registered")
	}
	if _, exists := authz.roles["reader"]; !exists {
		t.Error("reader role not registered")
	}
}

func TestAuthorize(t *testing.T) {
	authz := NewAuthorizer()
	
	tests := []struct {
		name       string
		user       *User
		permission Permission
		wantErr    bool
	}{
		{
			name: "admin can query read",
			user: &User{
				ID:       "admin-1",
				Username: "admin",
				Roles:    []string{"admin"},
			},
			permission: PermissionQueryRead,
			wantErr:    false,
		},
		{
			name: "admin can query write",
			user: &User{
				ID:       "admin-1",
				Username: "admin",
				Roles:    []string{"admin"},
			},
			permission: PermissionQueryWrite,
			wantErr:    false,
		},
		{
			name: "admin can manage shards",
			user: &User{
				ID:       "admin-1",
				Username: "admin",
				Roles:    []string{"admin"},
			},
			permission: PermissionAdminShards,
			wantErr:    false,
		},
		{
			name: "writer can query read",
			user: &User{
				ID:       "writer-1",
				Username: "writer",
				Roles:    []string{"writer"},
			},
			permission: PermissionQueryRead,
			wantErr:    false,
		},
		{
			name: "writer can query write",
			user: &User{
				ID:       "writer-1",
				Username: "writer",
				Roles:    []string{"writer"},
			},
			permission: PermissionQueryWrite,
			wantErr:    false,
		},
		{
			name: "writer cannot manage shards",
			user: &User{
				ID:       "writer-1",
				Username: "writer",
				Roles:    []string{"writer"},
			},
			permission: PermissionAdminShards,
			wantErr:    true,
		},
		{
			name: "reader can query read",
			user: &User{
				ID:       "reader-1",
				Username: "reader",
				Roles:    []string{"reader"},
			},
			permission: PermissionQueryRead,
			wantErr:    false,
		},
		{
			name: "reader cannot query write",
			user: &User{
				ID:       "reader-1",
				Username: "reader",
				Roles:    []string{"reader"},
			},
			permission: PermissionQueryWrite,
			wantErr:    true,
		},
		{
			name: "read-only can query read",
			user: &User{
				ID:       "readonly-1",
				Username: "readonly",
				Roles:    []string{"read"},
			},
			permission: PermissionQueryRead,
			wantErr:    false,
		},
		{
			name: "read-only cannot query write",
			user: &User{
				ID:       "readonly-1",
				Username: "readonly",
				Roles:    []string{"read"},
			},
			permission: PermissionQueryWrite,
			wantErr:    true,
		},
		{
			name: "nil user fails",
			user: nil,
			permission: PermissionQueryRead,
			wantErr:    true,
		},
		{
			name: "unknown role fails",
			user: &User{
				ID:       "unknown-1",
				Username: "unknown",
				Roles:    []string{"unknown-role"},
			},
			permission: PermissionQueryRead,
			wantErr:    true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authz.Authorize(tt.user, tt.permission)
			if (err != nil) != tt.wantErr {
				t.Errorf("Authorize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthorizeContext(t *testing.T) {
	authz := NewAuthorizer()
	
	user := &User{
		ID:       "ctx-user-1",
		Username: "contextuser",
		Roles:    []string{"admin"},
	}
	
	// Test with user in context
	ctx := WithUser(context.Background(), user)
	err := authz.AuthorizeContext(ctx, PermissionQueryRead)
	if err != nil {
		t.Errorf("AuthorizeContext() error = %v, want nil", err)
	}
	
	// Test without user in context
	ctx = context.Background()
	err = authz.AuthorizeContext(ctx, PermissionQueryRead)
	if err == nil {
		t.Error("AuthorizeContext() should fail without user in context")
	}
}

func TestHasPermission(t *testing.T) {
	authz := NewAuthorizer()
	
	admin := &User{
		ID:       "admin-1",
		Username: "admin",
		Roles:    []string{"admin"},
	}
	
	reader := &User{
		ID:       "reader-1",
		Username: "reader",
		Roles:    []string{"reader"},
	}
	
	if !authz.HasPermission(admin, PermissionQueryWrite) {
		t.Error("admin should have write permission")
	}
	
	if authz.HasPermission(reader, PermissionQueryWrite) {
		t.Error("reader should not have write permission")
	}
}

func TestHasAnyPermission(t *testing.T) {
	authz := NewAuthorizer()
	
	writer := &User{
		ID:       "writer-1",
		Username: "writer",
		Roles:    []string{"writer"},
	}
	
	// Writer has read OR write
	if !authz.HasAnyPermission(writer, PermissionQueryRead, PermissionQueryWrite) {
		t.Error("writer should have at least one permission")
	}
	
	// Writer doesn't have system permissions
	if authz.HasAnyPermission(writer, PermissionSystemConfig, PermissionSystemShutdown) {
		t.Error("writer should not have any system permissions")
	}
}

func TestHasAllPermissions(t *testing.T) {
	authz := NewAuthorizer()
	
	admin := &User{
		ID:       "admin-1",
		Username: "admin",
		Roles:    []string{"admin"},
	}
	
	reader := &User{
		ID:       "reader-1",
		Username: "reader",
		Roles:    []string{"reader"},
	}
	
	// Admin has all permissions
	if !authz.HasAllPermissions(admin, PermissionQueryRead, PermissionQueryWrite) {
		t.Error("admin should have all permissions")
	}
	
	// Reader doesn't have all permissions
	if authz.HasAllPermissions(reader, PermissionQueryRead, PermissionQueryWrite) {
		t.Error("reader should not have write permission")
	}
}

func TestGetUserPermissions(t *testing.T) {
	authz := NewAuthorizer()
	
	admin := &User{
		ID:       "admin-1",
		Username: "admin",
		Roles:    []string{"admin"},
	}
	
	perms := authz.GetUserPermissions(admin)
	if len(perms) == 0 {
		t.Error("GetUserPermissions() returned empty list for admin")
	}
	
	// Admin should have at least read and write permissions
	hasRead := false
	hasWrite := false
	for _, perm := range perms {
		if perm == PermissionQueryRead {
			hasRead = true
		}
		if perm == PermissionQueryWrite {
			hasWrite = true
		}
	}
	
	if !hasRead || !hasWrite {
		t.Error("admin should have read and write permissions")
	}
}

func TestRegisterCustomRole(t *testing.T) {
	authz := NewAuthorizer()
	
	customRole := &Role{
		Name: "custom",
		Permissions: []Permission{
			PermissionQueryRead,
		},
	}
	
	authz.RegisterRole(customRole)
	
	user := &User{
		ID:       "custom-user-1",
		Username: "customuser",
		Roles:    []string{"custom"},
	}
	
	// User with custom role should have read permission
	if err := authz.Authorize(user, PermissionQueryRead); err != nil {
		t.Errorf("Authorize() error = %v, want nil for custom role", err)
	}
	
	// User with custom role should not have write permission
	if err := authz.Authorize(user, PermissionQueryWrite); err == nil {
		t.Error("Authorize() should fail for permission not in custom role")
	}
}
