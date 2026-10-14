package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// MCPEndpointHandler manages the workspace's published MCP server endpoints.
type MCPEndpointHandler struct {
	svc      interfaces.MCPEndpointService
	sessions interfaces.SessionService
	messages interfaces.MessageService
}

// NewMCPEndpointHandler creates the MCP endpoint management handler.
func NewMCPEndpointHandler(
	svc interfaces.MCPEndpointService,
	sessions interfaces.SessionService,
	messages interfaces.MessageService,
) *MCPEndpointHandler {
	return &MCPEndpointHandler{svc: svc, sessions: sessions, messages: messages}
}

type mcpEndpointRequest struct {
	OwnerUserID        *string   `json:"owner_user_id"`
	Name               *string   `json:"name"`
	Description        *string   `json:"description"`
	Enabled            *bool     `json:"enabled"`
	KnowledgeBaseIDs   *[]string `json:"knowledge_base_ids"`
	Tools              *[]string `json:"tools"`
	DefaultAgentID     *string   `json:"default_agent_id"`
	RateLimitPerMinute *int      `json:"rate_limit_per_minute"`
}

type mcpEndpointSessionUpdateRequest struct {
	Title string `json:"title" binding:"required,max=100"`
}

func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ListToolCatalog returns the tools an endpoint can expose, grouped.
//
// @Summary      获取 MCP 端点工具目录
// @Description  返回工作空间 MCP 端点可勾选的工具清单、分组和默认勾选项
// @Tags         MCP端点
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "工具目录"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints/tools [get]
func (h *MCPEndpointHandler) ListToolCatalog(c *gin.Context) {
	groups := types.MCPEndpointToolGroups()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"groups":        groups,
			"tools":         types.MCPEndpointToolCatalog(),
			"default_tools": types.DefaultMCPEndpointTools(),
		},
	})
}

// CreateMCPEndpoint creates an endpoint and returns it with the one-time token.
//
// @Summary      创建 MCP 端点
// @Description  为当前工作空间发布一个 MCP 端点，响应中的 token 只返回一次
// @Tags         MCP端点
// @Accept       json
// @Produce      json
// @Param        request  body      object  true  "端点配置：name、description、enabled、knowledge_base_ids、tools 等"
// @Success      201      {object}  map[string]interface{}  "创建的端点，含一次性 token"
// @Failure      400      {object}  map[string]interface{}  "请求参数错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints [post]
func (h *MCPEndpointHandler) CreateMCPEndpoint(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	var req mcpEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	tools := types.DefaultMCPEndpointTools()
	if req.Tools != nil {
		tools = *req.Tools
	}
	var kbIDs []string
	if req.KnowledgeBaseIDs != nil {
		kbIDs = *req.KnowledgeBaseIDs
	}
	rate := 0
	if req.RateLimitPerMinute != nil {
		rate = *req.RateLimitPerMinute
	}
	ep, token, err := h.svc.Create(c.Request.Context(), tenantID, &types.MCPEndpoint{
		OwnerUserID:        stringValue(req.OwnerUserID),
		Name:               stringValue(req.Name),
		Description:        stringValue(req.Description),
		Enabled:            enabled,
		KnowledgeBaseIDs:   types.StringArray(kbIDs),
		Tools:              types.StringArray(tools),
		DefaultAgentID:     stringValue(req.DefaultAgentID),
		RateLimitPerMinute: rate,
	})
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": mcpEndpointResponse(ep, token)})
}

// ListMCPEndpoints lists the workspace endpoints without tokens.
//
// @Summary      获取 MCP 端点列表
// @Tags         MCP端点
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "端点列表"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints [get]
func (h *MCPEndpointHandler) ListMCPEndpoints(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	rows, err := h.svc.List(c.Request.Context(), tenantID)
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, ep := range rows {
		out = append(out, mcpEndpointResponse(ep, ""))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

// GetMCPEndpoint returns one endpoint.
//
// @Summary      获取 MCP 端点详情
// @Tags         MCP端点
// @Produce      json
// @Param        endpoint_id  path      string  true  "端点 ID"
// @Success      200          {object}  map[string]interface{}  "端点详情"
// @Failure      404          {object}  map[string]interface{}  "端点不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints/{endpoint_id} [get]
func (h *MCPEndpointHandler) GetMCPEndpoint(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	id := secutils.SanitizeForLog(c.Param("endpoint_id"))
	ep, err := h.svc.Get(c.Request.Context(), tenantID, id)
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": mcpEndpointResponse(ep, "")})
}

// UpdateMCPEndpoint applies a partial update; omitted fields are kept.
//
// @Summary      更新 MCP 端点
// @Tags         MCP端点
// @Accept       json
// @Produce      json
// @Param        endpoint_id  path      string  true  "端点 ID"
// @Param        request      body      object  true  "要更新的字段，未提供的字段保持不变"
// @Success      200          {object}  map[string]interface{}  "更新后的端点"
// @Failure      400          {object}  map[string]interface{}  "请求参数错误"
// @Failure      404          {object}  map[string]interface{}  "端点不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints/{endpoint_id} [put]
func (h *MCPEndpointHandler) UpdateMCPEndpoint(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	id := secutils.SanitizeForLog(c.Param("endpoint_id"))
	var req mcpEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ep, err := h.svc.Update(c.Request.Context(), tenantID, id, interfaces.MCPEndpointUpdate{
		OwnerUserID:        req.OwnerUserID,
		Name:               req.Name,
		Description:        req.Description,
		Enabled:            req.Enabled,
		KnowledgeBaseIDs:   req.KnowledgeBaseIDs,
		Tools:              req.Tools,
		DefaultAgentID:     req.DefaultAgentID,
		RateLimitPerMinute: req.RateLimitPerMinute,
	})
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": mcpEndpointResponse(ep, "")})
}

// DeleteMCPEndpoint soft-deletes an endpoint; connected clients lose access.
//
// @Summary      删除 MCP 端点
// @Tags         MCP端点
// @Produce      json
// @Param        endpoint_id  path      string  true  "端点 ID"
// @Success      200          {object}  map[string]interface{}  "删除成功"
// @Failure      404          {object}  map[string]interface{}  "端点不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints/{endpoint_id} [delete]
func (h *MCPEndpointHandler) DeleteMCPEndpoint(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	id := secutils.SanitizeForLog(c.Param("endpoint_id"))
	if err := h.svc.Delete(c.Request.Context(), tenantID, id); err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RotateMCPEndpointToken issues a fresh token and returns it once.
//
// @Summary      轮换 MCP 端点令牌
// @Description  生成新令牌并立即作废旧令牌，响应中的 token 只返回一次
// @Tags         MCP端点
// @Produce      json
// @Param        endpoint_id  path      string  true  "端点 ID"
// @Success      200          {object}  map[string]interface{}  "含新 token 的端点"
// @Failure      404          {object}  map[string]interface{}  "端点不存在"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /mcp-endpoints/{endpoint_id}/rotate-token [post]
func (h *MCPEndpointHandler) RotateMCPEndpointToken(c *gin.Context) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	id := secutils.SanitizeForLog(c.Param("endpoint_id"))
	ep, token, err := h.svc.RotateToken(c.Request.Context(), tenantID, id)
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": mcpEndpointResponse(ep, token)})
}

// endpointSessionContext validates the endpoint and scopes session operations
// to the same principal used by its MCP ask tool.
func (h *MCPEndpointHandler) endpointSessionContext(c *gin.Context) (context.Context, string, bool) {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	endpointID := secutils.SanitizeForLog(c.Param("endpoint_id"))
	ep, err := h.svc.Get(c.Request.Context(), tenantID, endpointID)
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return nil, "", false
	}
	ownerID := ep.SessionOwnerID()
	ctx := types.WithPrincipal(c.Request.Context(), ep.SessionPrincipal())
	if ep.OwnerUserID != "" {
		ctx = context.WithValue(ctx, types.UserIDContextKey, ownerID)
	}
	return ctx, ownerID, true
}

func (h *MCPEndpointHandler) endpointSession(c *gin.Context) (context.Context, *types.Session, bool) {
	ctx, ownerID, ok := h.endpointSessionContext(c)
	if !ok {
		return nil, nil, false
	}
	session, err := h.sessions.GetOwnedSession(ctx, secutils.SanitizeForLog(c.Param("session_id")))
	if err != nil || session.UserID != ownerID {
		if err == nil {
			err = apperrors.ErrSessionNotFound
		}
		writeMCPEndpointMgmtError(c, err)
		return nil, nil, false
	}
	return ctx, session, true
}

// ListMCPEndpointSessions lists sessions owned by this endpoint's bound identity.
func (h *MCPEndpointHandler) ListMCPEndpointSessions(c *gin.Context) {
	ctx, ownerID, ok := h.endpointSessionContext(c)
	if !ok {
		return
	}
	sessions, err := h.sessions.GetSessionsByTenant(ctx)
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	owned := make([]*types.Session, 0, len(sessions))
	for _, session := range sessions {
		if session != nil && session.UserID == ownerID {
			owned = append(owned, session)
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": owned})
}

// GetMCPEndpointSession returns one session created by this endpoint.
func (h *MCPEndpointHandler) GetMCPEndpointSession(c *gin.Context) {
	_, session, ok := h.endpointSession(c)
	if ok {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": session})
	}
}

// ListMCPEndpointSessionMessages returns one endpoint session's messages.
func (h *MCPEndpointHandler) ListMCPEndpointSessionMessages(c *gin.Context) {
	ctx, session, ok := h.endpointSession(c)
	if !ok {
		return
	}
	var page types.Pagination
	if err := c.ShouldBindQuery(&page); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	messages, err := h.messages.GetMessagesBySession(ctx, session.ID, page.GetPage(), page.GetPageSize())
	if err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": messages})
}

// UpdateMCPEndpointSession updates the title of one endpoint session.
func (h *MCPEndpointHandler) UpdateMCPEndpointSession(c *gin.Context) {
	ctx, session, ok := h.endpointSession(c)
	if !ok {
		return
	}
	var req mcpEndpointSessionUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title is required"})
		return
	}
	session.Title = strings.TrimSpace(req.Title)
	if err := h.sessions.UpdateSession(ctx, session); err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": session})
}

// DeleteMCPEndpointSession deletes one endpoint session through the normal
// session service so related knowledge and sandbox state are also cleaned up.
func (h *MCPEndpointHandler) DeleteMCPEndpointSession(c *gin.Context) {
	ctx, session, ok := h.endpointSession(c)
	if !ok {
		return
	}
	if err := h.sessions.DeleteSession(ctx, session.ID); err != nil {
		writeMCPEndpointMgmtError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// MCPEndpointPath is the public path prefix the MCP server is mounted on.
const MCPEndpointPath = "/mcp/"

func mcpEndpointResponse(ep *types.MCPEndpoint, token string) gin.H {
	row := gin.H{
		"id":                    ep.ID,
		"tenant_id":             ep.TenantID,
		"owner_user_id":         ep.OwnerUserID,
		"name":                  ep.Name,
		"description":           ep.Description,
		"enabled":               ep.Enabled,
		"token_hint":            ep.TokenHint,
		"knowledge_base_ids":    []string(ep.KnowledgeBaseIDs),
		"tools":                 []string(ep.Tools),
		"default_agent_id":      ep.DefaultAgentID,
		"rate_limit_per_minute": ep.RateLimitPerMinute,
		"path":                  MCPEndpointPath + ep.ID,
		"last_used_at":          ep.LastUsedAt,
		"created_at":            ep.CreatedAt,
		"updated_at":            ep.UpdatedAt,
	}
	if strings.TrimSpace(token) != "" {
		row["token"] = token
	}
	return row
}

func writeMCPEndpointMgmtError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrMCPEndpointNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "mcp endpoint not found"})
	case errors.Is(err, apperrors.ErrSessionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
	default:
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			switch appErr.Code {
			case apperrors.ErrNotFound:
				c.JSON(http.StatusNotFound, gin.H{"error": appErr.Message})
				return
			case apperrors.ErrBadRequest, apperrors.ErrValidation:
				c.JSON(http.StatusBadRequest, gin.H{"error": appErr.Message})
				return
			}
		}
		logger.Error(c.Request.Context(), "mcp endpoint management failed", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "operation failed"})
	}
}
