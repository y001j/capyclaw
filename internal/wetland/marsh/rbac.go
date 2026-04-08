package marsh

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2"
)

// RBACManager manages role-based access control via Apache Casbin.
type RBACManager struct {
	enforcer *casbin.Enforcer
}

// NewRBACManager creates a new RBAC manager.
func NewRBACManager(modelPath, policyPath string) (*RBACManager, error) {
	e, err := casbin.NewEnforcer(modelPath, policyPath)
	if err != nil {
		return nil, fmt.Errorf("creating Casbin enforcer: %w", err)
	}
	return &RBACManager{enforcer: e}, nil
}

// CheckPermission evaluates whether a subject can perform an action on an object.
func (m *RBACManager) CheckPermission(ctx context.Context, sub, obj, act string) (bool, error) {
	return m.enforcer.Enforce(sub, obj, act)
}

// AddRoleForUser assigns a role to a user within a tenant.
func (m *RBACManager) AddRoleForUser(user, role, tenant string) error {
	_, err := m.enforcer.AddGroupingPolicy(user, role, tenant)
	return err
}

// RemoveRoleForUser removes a role from a user.
func (m *RBACManager) RemoveRoleForUser(user, role, tenant string) error {
	_, err := m.enforcer.RemoveGroupingPolicy(user, role, tenant)
	return err
}

// GetRolesForUser returns all roles for a user in a tenant.
func (m *RBACManager) GetRolesForUser(user, tenant string) ([]string, error) {
	return m.enforcer.GetRolesForUser(user, tenant)
}
