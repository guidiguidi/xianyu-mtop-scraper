package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"goofish-backend/config"
	"goofish-backend/database"
	"goofish-backend/goofish"
	"goofish-backend/services"

	"github.com/gin-gonic/gin"
)

// APIHandler contains REST routes for inspecting search, cookies, and translator endpoints.
type APIHandler struct {
	db        *database.Database
	refresher *services.CookieRefresherService
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(db *database.Database, refresher *services.CookieRefresherService) *APIHandler {
	return &APIHandler{
		db:        db,
		refresher: refresher,
	}
}

// BindRoutes binds the endpoints to Gin.
func (h *APIHandler) BindRoutes(r *gin.Engine) {
	r.GET("/", h.Root)
	r.GET("/health", h.Health)
	r.GET("/search", h.Search)
	r.GET("/search/translated", h.SearchTranslated)
	r.GET("/cookies/status", h.CookiesStatus)
	r.GET("/translate", h.Translate)
	r.GET("/debug/raw-search", h.DebugRawSearch)
}

// Root renders a beautiful HTML status dashboard.
func (h *APIHandler) Root(c *gin.Context) {
	htmlContent := `
	<!DOCTYPE html>
	<html lang="ru">
	<head>
		<meta charset="UTF-8">
		<title>🐟 Xianyu Bot API (Go)</title>
		<style>
			body { font-family: -apple-system, sans-serif; background: #0f0f0f; color: #e0e0e0; padding: 40px; }
			h1 { color: #4fc3f7; }
			.endpoint { background: #1a1a2e; border-left: 3px solid #4fc3f7; padding: 12px 16px; margin: 10px 0; border-radius: 4px; }
			.method { background: #00897b; color: white; padding: 2px 8px; border-radius: 3px; font-size: 12px; font-weight: bold; }
			a { color: #4fc3f7; }
			code { background: #1e1e1e; padding: 2px 6px; border-radius: 3px; }
		</style>
	</head>
	<body>
		<h1>🐟 Xianyu Bot API (Go)</h1>
		<p>REST API for querying, parsing, and normalizing listings from Goofish.</p>
		<p>📖 Review endpoints below for system health and queries.</p>
		<h2>Endpoints</h2>
		<div class="endpoint"><span class="method">GET</span> <code>/health</code> — System health check and configuration status</div>
		<div class="endpoint"><span class="method">GET</span> <code>/search?query=rick+owens&page=1</code> — Search Goofish listings (raw normalized)</div>
		<div class="endpoint"><span class="method">GET</span> <code>/search/translated?query=maison+margiela</code> — Search with DeepL translation pipeline</div>
		<div class="endpoint"><span class="method">GET</span> <code>/cookies/status</code> — Active session cookies status</div>
		<div class="endpoint"><span class="method">GET</span> <code>/translate?text=马丁靴&target=EN</code> — Direct DeepL translation probe</div>
	</body>
	</html>
	`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(htmlContent))
}

// Health checks system states, configurations, and database metrics.
func (h *APIHandler) Health(c *gin.Context) {
	cookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	monitors, err := h.db.GetActiveMonitors()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"config": gin.H{
			"bot_token_set":   config.Settings.BotToken != "" && !strings.Contains(config.Settings.BotToken, "example"),
			"cookies_loaded":  cookies != "",
			"cookies_length":  len(cookies),
			"proxy_url":       config.Settings.ProxyURL,
			"asocks_key_set":  config.Settings.AsocksAPIKey != "",
			"deepl_key_set":   config.Settings.DeeplAPIKey != "",
			"deepl_endpoint":  services.GlobalTranslator.Endpoint,
		},
		"database": gin.H{
			"path":            config.Settings.DatabasePath,
			"active_monitors": len(monitors),
		},
	})
}

// Search queries raw items from Goofish search.
func (h *APIHandler) Search(c *gin.Context) {
	query := c.Query("query")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "query parameter is required"})
		return
	}

	pageVal := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageVal)
	if err != nil || page < 1 {
		page = 1
	}

	limitVal := c.DefaultQuery("limit", "5")
	limit, err := strconv.Atoi(limitVal)
	if err != nil || limit < 1 {
		limit = 5
	}

	var priceMin, priceMax *int
	if pMinStr := c.Query("price_min"); pMinStr != "" {
		if val, err := strconv.Atoi(pMinStr); err == nil {
			priceMin = &val
		}
	}
	if pMaxStr := c.Query("price_max"); pMaxStr != "" {
		if val, err := strconv.Atoi(pMaxStr); err == nil {
			priceMax = &val
		}
	}

	activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
	if err != nil || activeCookies == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "No active session cookies found. Please supply session cookies via /setcookies."})
		return
	}

	client := goofish.NewSearchClient(activeCookies, config.Settings.ProxyURL, func(s string) error {
		return h.db.SaveCookies(s)
	})
	client.ForceCookieRefreshFunc = func(curr string) (string, error) {
		return h.refresher.RefreshCookies(curr)
	}

	expandedQuery := services.ExpandQuery(query)
	rawResp, err := client.Search(expandedQuery, page, priceMin, priceMax, limit, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	items := goofish.ParseSearchResponse(rawResp)
	var serializedItems []map[string]interface{}
	for _, item := range items {
		serializedItems = append(serializedItems, item.ToMap())
	}

	c.JSON(http.StatusOK, gin.H{
		"query":       query,
		"page":        page,
		"total_found": len(items),
		"items":       serializedItems,
	})
}

// SearchTranslated returns search listings complete with parallel Russian DeepL translations.
func (h *APIHandler) SearchTranslated(c *gin.Context) {
	query := c.Query("query")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "query parameter is required"})
		return
	}

	pageVal := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageVal)
	if err != nil || page < 1 {
		page = 1
	}

	limitVal := c.DefaultQuery("limit", "3")
	limit, err := strconv.Atoi(limitVal)
	if err != nil || limit < 1 {
		limit = 3
	}

	targetLang := c.DefaultQuery("target_lang", "RU")

	var priceMin, priceMax *int
	if pMinStr := c.Query("price_min"); pMinStr != "" {
		if val, err := strconv.Atoi(pMinStr); err == nil {
			priceMin = &val
		}
	}
	if pMaxStr := c.Query("price_max"); pMaxStr != "" {
		if val, err := strconv.Atoi(pMaxStr); err == nil {
			priceMax = &val
		}
	}

	activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
	if err != nil || activeCookies == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "No active session cookies."})
		return
	}

	client := goofish.NewSearchClient(activeCookies, config.Settings.ProxyURL, func(s string) error {
		return h.db.SaveCookies(s)
	})
	client.ForceCookieRefreshFunc = func(curr string) (string, error) {
		return h.refresher.RefreshCookies(curr)
	}

	expandedQuery := services.ExpandQuery(query)
	rawResp, err := client.Search(expandedQuery, page, priceMin, priceMax, limit, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	items := goofish.ParseSearchResponse(rawResp)
	if len(items) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"query":       query,
			"page":        page,
			"total_found": 0,
			"items":       []interface{}{},
		})
		return
	}

	// Concurrently translate all titles and areas using DeepL
	var wg sync.WaitGroup
	type transResult struct {
		index      int
		titleTrans string
		areaTrans  string
	}
	transChan := make(chan transResult, len(items))

	for idx, item := range items {
		wg.Add(1)
		go func(i int, it goofish.GoofishItem) {
			defer wg.Done()
			tTitle := services.GlobalTranslator.Translate(it.Title, "ZH", targetLang)
			tArea := ""
			if it.Area != "" {
				tArea = services.GlobalTranslator.Translate(it.Area, "ZH", targetLang)
			}
			transChan <- transResult{index: i, titleTrans: tTitle, areaTrans: tArea}
		}(idx, item)
	}

	wg.Wait()
	close(transChan)

	transMap := make(map[int]transResult)
	for res := range transChan {
		transMap[res.index] = res
	}

	var results []map[string]interface{}
	for idx, item := range items {
		trans := transMap[idx]
		item.TitleTranslated = trans.titleTrans
		item.AreaTranslated = trans.areaTrans

		item.TelegramCaption = fmt.Sprintf(
			"🐟 [%d/%d] %s...\n\n💰 Price: %s\n📍 Region: %s\n👤 Seller: %s\n🔗 %s",
			idx+1, len(items), trans.titleTrans, item.Price, trans.areaTrans, item.UserNickname, item.ShareURL(),
		)

		results = append(results, item.ToMap())
	}

	c.JSON(http.StatusOK, gin.H{
		"query":        query,
		"page":         page,
		"target_lang":  targetLang,
		"deepl_active": config.Settings.DeeplAPIKey != "",
		"total_found":  len(results),
		"items":        results,
	})
}

// CookiesStatus verifies active cookie structures and parameters.
func (h *APIHandler) CookiesStatus(c *gin.Context) {
	cookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}

	if cookies == "" {
		c.JSON(http.StatusOK, gin.H{
			"status":  "empty",
			"message": "Cookies not set. Use /setcookies to configure session.",
		})
		return
	}

	cookiePairs := strings.Split(cookies, ";")
	var names []string
	hasH5Token := false
	hasTbToken := false

	for _, pair := range cookiePairs {
		pair = strings.TrimSpace(pair)
		if idx := strings.Index(pair, "="); idx != -1 {
			name := strings.TrimSpace(pair[:idx])
			names = append(names, name)
			if name == "_m_h5_tk" {
				hasH5Token = true
			}
			if name == "_tb_token_" {
				hasTbToken = true
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "ok",
		"cookie_count": len(names),
		"cookie_names": names,
		"has_h5_token": hasH5Token,
		"has_tb_token": hasTbToken,
	})
}

// Translate verifies DeepL translation status and translates text directly.
func (h *APIHandler) Translate(c *gin.Context) {
	text := c.Query("text")
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "text query parameter is required"})
		return
	}

	source := c.DefaultQuery("source", "ZH")
	target := c.DefaultQuery("target", "RU")

	translated := services.GlobalTranslator.Translate(text, source, target)
	c.JSON(http.StatusOK, gin.H{
		"deepl_active":  config.Settings.DeeplAPIKey != "",
		"free_fallback": true,
		"endpoint":      services.GlobalTranslator.Endpoint,
		"source_lang":   source,
		"target_lang":   target,
		"original":      text,
		"translated":    translated,
	})
}

// Helper to bypass JSON bool literal issues
func FalseVal() bool {
	return false
}

// DebugRawSearch returns the RAW JSON response from the Goofish API without any parsing.
func (h *APIHandler) DebugRawSearch(c *gin.Context) {
	query := c.DefaultQuery("query", "rick owens")

	activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
	if err != nil || activeCookies == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"detail": "No active cookies."})
		return
	}

	client := goofish.NewSearchClient(activeCookies, config.Settings.ProxyURL, func(s string) error {
		return h.db.SaveCookies(s)
	})
	client.ForceCookieRefreshFunc = func(curr string) (string, error) {
		return h.refresher.RefreshCookies(curr)
	}

	rawResp, err := client.Search(query, 1, nil, nil, 1, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, rawResp)
}
