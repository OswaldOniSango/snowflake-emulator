package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nnnkkk7/snowflake-emulator/pkg/identity"
	"github.com/nnnkkk7/snowflake-emulator/pkg/session"
	"github.com/nnnkkk7/snowflake-emulator/server/types"
)

// Validate the user independently of the selected role so USE ROLE can recover
// from a revoked role. Disabled or deleted users cannot use an existing token.
func validateSessionUser(ctx context.Context, service *identity.Service, sess *session.Session) error {
	if service == nil {
		return nil
	}
	_, err := service.ResolveActiveRole(ctx, sess.UserID, identity.RolePublic)
	return err
}

func validateSessionRole(ctx context.Context, service *identity.Service, sess *session.Session) error {
	if service == nil {
		return nil
	}
	role, err := service.ResolveActiveRole(ctx, sess.UserID, sess.ActiveRole)
	if err != nil {
		return err
	}
	if role.ID != sess.ActiveRoleID {
		return fmt.Errorf("active role has changed; select an available role again")
	}
	return nil
}

func (h *RestAPIv2Handler) statementSession(w http.ResponseWriter, r *http.Request) (*session.Session, bool) {
	// Handler instances without session services preserve the standalone local API.
	if h.sessionMgr == nil {
		return nil, true
	}
	sess, err := h.sessionMgr.ValidateSession(r.Context(), extractToken(r))
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "Session expired or invalid", types.SQLState42000)
		return nil, false
	}
	if err := validateSessionUser(r.Context(), h.identity, sess); err != nil {
		h.sendError(w, http.StatusUnauthorized, "Authenticated user is no longer available", types.SQLState42000)
		return nil, false
	}
	return sess, true
}

func (h *RestAPIv2Handler) statementUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.sessionMgr == nil {
		return "", true
	}
	sess, err := h.sessionMgr.InspectSession(extractToken(r))
	if err != nil {
		h.sendError(w, http.StatusUnauthorized, "Session expired or invalid", types.SQLState42000)
		return "", false
	}
	// Canceling one's own admitted work is allowed even after role revocation
	// or user disablement. It must not wait for DuckDB to authenticate it.
	if r.Method != http.MethodPost {
		if err := validateSessionUser(r.Context(), h.identity, sess); err != nil {
			h.sendError(w, http.StatusUnauthorized, "Authenticated user is no longer available", types.SQLState42000)
			return "", false
		}
	}
	return sess.UserID, true
}
