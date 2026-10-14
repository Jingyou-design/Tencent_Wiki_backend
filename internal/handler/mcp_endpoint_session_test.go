package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type endpointSessionEndpointService struct {
	interfaces.MCPEndpointService
	ownerUserID string
}

func (s endpointSessionEndpointService) Get(_ context.Context, tenantID uint64, id string) (*types.MCPEndpoint, error) {
	return &types.MCPEndpoint{ID: id, TenantID: tenantID, OwnerUserID: s.ownerUserID}, nil
}

type endpointSessionSessionService struct {
	interfaces.SessionService
	rows      []*types.Session
	owned     *types.Session
	principal string
}

func (s *endpointSessionSessionService) GetSessionsByTenant(ctx context.Context) ([]*types.Session, error) {
	s.principal = types.SessionOwnerIDFromContext(ctx)
	return s.rows, nil
}

func (s *endpointSessionSessionService) GetOwnedSession(ctx context.Context, _ string) (*types.Session, error) {
	s.principal = types.SessionOwnerIDFromContext(ctx)
	return s.owned, nil
}

func endpointSessionRouter(h *MCPEndpointHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Next()
	})
	r.GET("/mcp-endpoints/:endpoint_id/sessions", h.ListMCPEndpointSessions)
	r.GET("/mcp-endpoints/:endpoint_id/sessions/:session_id", h.GetMCPEndpointSession)
	return r
}

func TestListMCPEndpointSessionsReturnsOnlyEndpointOwnedRows(t *testing.T) {
	owner := types.MCPEndpointPrincipal(7, "endpoint-1").StorageID()
	sessions := &endpointSessionSessionService{rows: []*types.Session{
		{ID: "owned", UserID: owner},
		{ID: "legacy", UserID: ""},
		{ID: "other", UserID: types.MCPEndpointPrincipal(7, "endpoint-2").StorageID()},
	}}
	h := NewMCPEndpointHandler(endpointSessionEndpointService{}, sessions, nil)
	recorder := httptest.NewRecorder()
	endpointSessionRouter(h).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "/mcp-endpoints/endpoint-1/sessions", nil,
	))

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Data []types.Session `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	require.Equal(t, "owned", body.Data[0].ID)
	require.Equal(t, owner, sessions.principal)
}

func TestGetMCPEndpointSessionRejectsAnotherOwner(t *testing.T) {
	sessions := &endpointSessionSessionService{owned: &types.Session{
		ID: "other", UserID: types.MCPEndpointPrincipal(7, "endpoint-2").StorageID(),
	}}
	h := NewMCPEndpointHandler(endpointSessionEndpointService{}, sessions, nil)
	recorder := httptest.NewRecorder()
	endpointSessionRouter(h).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "/mcp-endpoints/endpoint-1/sessions/other", nil,
	))

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestListMCPEndpointSessionsUsesBoundUser(t *testing.T) {
	sessions := &endpointSessionSessionService{rows: []*types.Session{{ID: "owned", UserID: "user-1"}}}
	h := NewMCPEndpointHandler(endpointSessionEndpointService{ownerUserID: "user-1"}, sessions, nil)
	recorder := httptest.NewRecorder()
	endpointSessionRouter(h).ServeHTTP(recorder, httptest.NewRequest(
		http.MethodGet, "/mcp-endpoints/endpoint-1/sessions", nil,
	))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "user-1", sessions.principal)
}
