package handler

import (
	"context"
	"fmt"

	"github.com/dbre-maestro/maestro/internal/model"
	"github.com/dbre-maestro/maestro/internal/repository"
)

func requireAllPermissionsAdmin(ctx context.Context, users *repository.UserRepo, actorID uint64) error {
	if actorID == 0 {
		return fmt.Errorf("all-permissions admin is required")
	}
	ok, err := users.HasAllPermissions(ctx, actorID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("all-permissions admin is required")
	}
	return nil
}

func requireProtectedUserAdmin(ctx context.Context, users *repository.UserRepo, actorID uint64, target *model.User) error {
	if target == nil || !target.IsProtected {
		return nil
	}
	if err := requireAllPermissionsAdmin(ctx, users, actorID); err != nil {
		return fmt.Errorf("protected user can only be changed by all-permissions admin")
	}
	return nil
}

func requireAuthGroupGrantAllowed(ctx context.Context, users *repository.UserRepo, actorID uint64, group *repository.AuthGroupEntity) error {
	if group == nil {
		return nil
	}
	if group.IsProtected || group.IsAllPermissions || group.GroupKey == string(model.AuthGroupAdmin) {
		if err := requireAllPermissionsAdmin(ctx, users, actorID); err != nil {
			return fmt.Errorf("cannot grant protected or all-permissions auth group")
		}
	}
	return nil
}

func requireAuthGroupContentsGrantAllowed(ctx context.Context, users *repository.UserRepo, authGroups *repository.AuthGroupRepo, actorID uint64, group *repository.AuthGroupEntity) error {
	if err := requireAuthGroupGrantAllowed(ctx, users, actorID, group); err != nil {
		return err
	}
	hasAll, err := users.HasAllPermissions(ctx, actorID)
	if err != nil {
		return err
	}
	if hasAll || group == nil || authGroups == nil {
		return nil
	}
	permissionKeys, err := authGroups.ListPermissionKeys(ctx, group.ID)
	if err != nil {
		return err
	}
	for _, permissionKey := range permissionKeys {
		if err := requirePermissionGrantAllowed(ctx, users, actorID, permissionKey); err != nil {
			return err
		}
	}
	dbConnectionIDs, err := authGroups.ListDBConnectionIDs(ctx, group.ID)
	if err != nil {
		return err
	}
	for _, dbConnectionID := range dbConnectionIDs {
		if err := requireDBConnectionGrantAllowed(ctx, users, actorID, dbConnectionID); err != nil {
			return err
		}
	}
	return nil
}

func requireAuthGroupMutationAllowed(ctx context.Context, users *repository.UserRepo, actorID uint64, group *repository.AuthGroupEntity) error {
	if group == nil {
		return nil
	}
	if group.IsProtected || group.IsAllPermissions || group.GroupKey == string(model.AuthGroupAdmin) {
		if err := requireAllPermissionsAdmin(ctx, users, actorID); err != nil {
			return fmt.Errorf("protected auth group can only be changed by all-permissions admin")
		}
	}
	return nil
}

func requirePermissionGrantAllowed(ctx context.Context, users *repository.UserRepo, actorID uint64, permissionKey string) error {
	if actorID == 0 {
		return fmt.Errorf("cannot grant permission")
	}
	hasAll, err := users.HasAllPermissions(ctx, actorID)
	if err != nil {
		return err
	}
	if hasAll {
		return nil
	}
	keys, err := users.GetEffectivePermissionKeys(ctx, actorID)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if key == permissionKey {
			return nil
		}
	}
	return fmt.Errorf("cannot grant permission not held by actor")
}

func requireDBConnectionGrantAllowed(ctx context.Context, users *repository.UserRepo, actorID, connectionID uint64) error {
	if actorID == 0 {
		return fmt.Errorf("cannot grant db connection scope")
	}
	hasAll, err := users.HasAllPermissions(ctx, actorID)
	if err != nil {
		return err
	}
	if hasAll {
		return nil
	}
	ids, err := users.GetEffectiveDBConnectionIDs(ctx, actorID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if id == connectionID {
			return nil
		}
	}
	return fmt.Errorf("cannot grant db connection scope not held by actor")
}
