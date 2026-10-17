package goofish

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SearchClient is a high-performance Goofish API client.
type SearchClient struct {
	AppKey                 string
	ProxyURL               string
	Cookies                map[string]string
	Headers                map[string]string
	mu                     sync.RWMutex
	OnCookiesRefreshed     func(string) error
	ForceCookieRefreshFunc func(string) (string, error)
}

// NewSearchClient creates a new SearchClient instance with default headers.
func NewSearchClient(cookiesStr, proxyURL string, onCookiesRefreshed func(string) error) *SearchClient {
	c := &SearchClient{
		AppKey:             "34839810",
		ProxyURL:           strings.TrimSpace(proxyURL),
		Cookies:            make(map[string]string),
		OnCookiesRefreshed: onCookiesRefreshed,
		Headers: map[string]string{
			"accept":          "application/json",
			"accept-language": "en-GB,en-US;q=0.9,en;q=0.8",
			"content-type":    "application/x-www-form-urlencoded",
			"origin":          "https://www.goofish.com",
			"referer":         "https://www.goofish.com/",
			"sec-ch-ua":       `"Chromium";v="148", "Google Chrome";v="148", "Not/A)Brand";v="99"`,
			"sec-ch-ua-mobile": "?0",
			"sec-ch-ua-platform": `"macOS"`,
			"sec-fetch-dest":   "empty",
			"sec-fetch-mode":   "cors",
			"sec-fetch-site":   "same-site",
			"user-agent":      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		},
	}

	if cookiesStr != "" {
		c.ParseCookiesString(cookiesStr)
	}

	return c
}

// ParseCookiesString parses a raw cookie string into the client's cookie map.
func (c *SearchClient) ParseCookiesString(cookieStr string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	parsedCount := 0
	parts := strings.Split(cookieStr, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, "="); idx != -1 {
			k := strings.TrimSpace(part[:idx])
			v := strings.TrimSpace(part[idx+1:])
			c.Cookies[k] = v
			parsedCount++
		}
	}
	log.Printf("[SearchClient] Loaded %d cookies into context.", parsedCount)
}

// GetCookiesString serializes the active cookie map back into a semicolon-separated string.
func (c *SearchClient) GetCookiesString() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var pairs []string
	for k, v := range c.Cookies {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, "; ")
}

// GetToken extracts the active H5 token signature from the cookies.
func (c *SearchClient) GetToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tokenCookie, exists := c.Cookies["_m_h5_tk"]
	if !exists {
		return ""
	}
	parts := strings.Split(tokenCookie, "_")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// CalculateSignature hashes the signature using MD5 according to MTOP standards.
func (c *SearchClient) CalculateSignature(token, timestamp, dataStr string) string {
	signString := fmt.Sprintf("%s&%s&%s&%s", token, timestamp, c.AppKey, dataStr)
	hasher := md5.New()
	hasher.Write([]byte(signString))
	return hex.EncodeToString(hasher.Sum(nil))
}

// ForceCookieRefresh triggers the external Playwright browser cookie refresher.
func (c *SearchClient) ForceCookieRefresh() error {
	if c.ForceCookieRefreshFunc == nil {
		return fmt.Errorf("no cookie refresh function configured on client")
	}

	currentCookies := c.GetCookiesString()
	log.Println("[SearchClient] Triggering forced automated browser cookie renewal...")
	
	refreshed, err := c.ForceCookieRefreshFunc(currentCookies)
	if err != nil {
		return fmt.Errorf("automated cookie refresh failed: %w", err)
	}

	if refreshed != "" && refreshed != currentCookies {
		c.ParseCookiesString(refreshed)
		if c.OnCookiesRefreshed != nil {
			if err := c.OnCookiesRefreshed(refreshed); err != nil {
				log.Printf("[SearchClient] Warning: Failed to persist refreshed cookies: %v", err)
			} else {
				log.Println("[SearchClient] Auto-refreshed cookies persisted to DB successfully.")
			}
		}
	}
	return nil
}

// Search executes the HTTP POST request to the Goofish H5 Search API.
func (c *SearchClient) Search(keyword string, page int, priceMin *int, priceMax *int, rowsPerPage int, retryOnExpired bool) (map[string]interface{}, error) {
	timestamp := strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10)

	// Build price range string
	priceRange := ""
	if priceMin != nil || priceMax != nil {
		minStr := ""
		maxStr := ""
		if priceMin != nil {
			minStr = strconv.Itoa(*priceMin)
		}
		if priceMax != nil {
			maxStr = strconv.Itoa(*priceMax)
		}
		priceRange = fmt.Sprintf("priceRange:%s,%s;", minStr, maxStr)
	}

	// Build exact payload
	dataPayload := map[string]interface{}{
		"pageNumber":        page,
		"keyword":           keyword,
		"fromFilter":        true,
		"rowsPerPage":       rowsPerPage,
		"sortValue":         "",
		"sortField":         "",
		"customDistance":    "",
		"gps":               "",
		"propValueStr": map[string]interface{}{
			"searchFilter": priceRange,
		},
		"customGps":          "",
		"searchReqFromPage":  "pcSearch",
		"extraFilterValue":   "{}",
		"userPositionJson":   "{}",
	}

	payloadBytes, err := json.Marshal(dataPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search data payload: %w", err)
	}
	dataStr := string(payloadBytes)

	token := c.GetToken()
	if token == "" {
		if retryOnExpired {
			log.Println("[SearchClient] No H5 token found. Attempting automatic cookie refresh...")
			if err := c.ForceCookieRefresh(); err == nil {
				return c.Search(keyword, page, priceMin, priceMax, rowsPerPage, false)
			}
		}
		return nil, fmt.Errorf("no valid H5 token found in cookies")
	}

	signature := c.CalculateSignature(token, timestamp, dataStr)

	params := url.Values{}
	params.Set("jsv", "2.7.2")
	params.Set("appKey", c.AppKey)
	params.Set("t", timestamp)
	params.Set("sign", signature)
	params.Set("v", "1.0")
	params.Set("type", "originaljson")
	params.Set("accountSite", "xianyu")
	params.Set("dataType", "json")
	params.Set("timeout", "20000")
	params.Set("api", "mtop.taobao.idlemtopsearch.pc.search")
	params.Set("sessionOption", "AutoLoginOnly")
	params.Set("spm_cnt", "a21ybx.search.0.0")
	params.Set("spm_pre", "a21ybx.home.searchInput.0")

	apiURL := "https://h5api.m.goofish.com/h5/mtop.taobao.idlemtopsearch.pc.search/1.0/"
	reqURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	formData := url.Values{}
	formData.Set("data", dataStr)
	reqBody := formData.Encode()

	// Prepare standard library HTTP client with Proxy and SSL-bypassing configuration
	var httpClient *http.Client
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	if c.ProxyURL != "" {
		parsedProxyURL, err := url.Parse(c.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %s: %w", c.ProxyURL, err)
		}
		transport.Proxy = http.ProxyURL(parsedProxyURL)
		log.Printf("[SearchClient] Routing request through proxy: %s", c.ProxyURL)
	}

	httpClient = &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
	}

	req, err := http.NewRequest("POST", reqURL, strings.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Copy headers
	c.mu.RLock()
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("cookie", c.GetCookiesString())
	c.mu.RUnlock()

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.Status != "200 OK" && resp.StatusCode != 200 {
		return nil, fmt.Errorf("Goofish response error status: %s, body: %s", resp.Status, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response JSON: %w, raw body: %s", err, string(bodyBytes))
	}

	// Inspect for H5 token expiration errors in the "ret" array
	tokenExpired := false
	if retList, ok := result["ret"].([]interface{}); ok {
		for _, retElem := range retList {
			if retStr, ok := retElem.(string); ok {
				for _, errCode := range []string{"FAIL_SYS_TOKEN", "FAIL_SYS_ILLEGAL_ACCESS", "TOKEN_EXPIRED", "ERR_SID_INVALID"} {
					if strings.Contains(retStr, errCode) {
						tokenExpired = true
						log.Printf("[SearchClient] Detected token error matching %s: %s", errCode, retStr)
						break
					}
				}
			}
			if tokenExpired {
				break
			}
		}
	}

	if tokenExpired && retryOnExpired {
		log.Println("[SearchClient] Taobao H5 token expired or rejected. Initiating auto-refresh...")
		if err := c.ForceCookieRefresh(); err == nil {
			// Retry search with fresh cookie context
			return c.Search(keyword, page, priceMin, priceMax, rowsPerPage, false)
		}
	}

	// Dynamically capture updated H5 tokens from Set-Cookie headers
	newCookies := make(map[string]string)
	for _, cookieHeader := range resp.Header["Set-Cookie"] {
		parts := strings.Split(cookieHeader, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.Contains(part, "=") {
				idx := strings.Index(part, "=")
				k := strings.TrimSpace(part[:idx])
				v := strings.TrimSpace(part[idx+1:])
				if k == "_m_h5_tk" || k == "_m_h5_tk_enc" {
					newCookies[k] = v
				}
			}
		}
	}

	if len(newCookies) > 0 {
		c.mu.Lock()
		for k, v := range newCookies {
			c.Cookies[k] = v
		}
		serialized := c.GetCookiesString()
		c.mu.Unlock()

		log.Printf("[SearchClient] Automatically updated dynamic H5 tokens in session: %v", newCookies)
		if c.OnCookiesRefreshed != nil {
			if err := c.OnCookiesRefreshed(serialized); err != nil {
				log.Printf("[SearchClient] Warning: Failed to persist set-cookies update: %v", err)
			}
		}
	}

	return result, nil
}

// GetUserProfile fetches the seller's profile, including their Zhima Credit status and rating.
func (c *SearchClient) GetUserProfile(userID string) (map[string]interface{}, error) {
	timestamp := strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10)

	dataPayload := map[string]interface{}{
		"self":   false,
		"userId": userID,
	}

	payloadBytes, err := json.Marshal(dataPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile data payload: %w", err)
	}
	dataStr := string(payloadBytes)

	token := c.GetToken()
	if token == "" {
		return nil, fmt.Errorf("no valid H5 token found in cookies for profile request")
	}

	signature := c.CalculateSignature(token, timestamp, dataStr)

	params := url.Values{}
	params.Set("jsv", "2.7.2")
	params.Set("appKey", c.AppKey)
	params.Set("t", timestamp)
	params.Set("sign", signature)
	params.Set("v", "1.0")
	params.Set("type", "originaljson")
	params.Set("accountSite", "xianyu")
	params.Set("dataType", "json")
	params.Set("timeout", "20000")
	params.Set("api", "mtop.idle.web.user.page.head")
	params.Set("sessionOption", "AutoLoginOnly")
	params.Set("spm_cnt", "a21ybx.personal.0.0")

	apiURL := "https://h5api.m.goofish.com/h5/mtop.idle.web.user.page.head/1.0/"
	reqURL := fmt.Sprintf("%s?%s", apiURL, params.Encode())

	formData := url.Values{}
	formData.Set("data", dataStr)
	reqBody := formData.Encode()

	var httpClient *http.Client
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	if c.ProxyURL != "" {
		parsedProxyURL, err := url.Parse(c.ProxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}

	httpClient = &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
	}

	req, err := http.NewRequest("POST", reqURL, strings.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create profile request: %w", err)
	}

	c.mu.RLock()
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("cookie", c.GetCookiesString())
	c.mu.RUnlock()

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("profile request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile response body: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode profile response JSON: %w, raw body: %s", err, string(bodyBytes))
	}

	return result, nil
}

