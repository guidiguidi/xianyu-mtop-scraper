package services

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProxyInfo represents a structured proxy response with geographic metadata.
type ProxyInfo struct {
	ProxyURL    string `json:"proxy_url"`
	CountryCode string `json:"country_code"`
	City        string `json:"city"`
	State       string `json:"state"`
}

// ASocksService interacts with the ASocks API to manage residential proxies.
type ASocksService struct {
	APIKey  string
	BaseURL string
}

// NewASocksService creates a new ASocksService.
func NewASocksService(apiKey string) *ASocksService {
	return &ASocksService{
		APIKey:  apiKey,
		BaseURL: "https://api.asocks.com/v2",
	}
}

// GetActiveProxy fetches the first active residential proxy or creates a new one if none are available.
func (s *ASocksService) GetActiveProxy() (*ProxyInfo, error) {
	// Configure SSL-bypassing client to align with local macOS certificates
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   15 * time.Second,
	}

	// 1. Fetch current ports list
	portsURL := fmt.Sprintf("%s/proxy/ports?apiKey=%s&per_page=50", s.BaseURL, s.APIKey)
	log.Println("[ASocks] Querying active residential proxy ports list from ASocks API...")

	resp, err := client.Get(portsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ports from ASocks: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ASocks response body: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ASocks API returned error status HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var resData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &resData); err != nil {
		return nil, fmt.Errorf("failed to parse ASocks response JSON: %w", err)
	}

	success, _ := resData["success"].(bool)
	if !success {
		msg, _ := resData["message"].(string)
		return nil, fmt.Errorf("ASocks API reported failure: %s", msg)
	}

	messageNode, _ := resData["message"].(map[string]interface{})
	proxies, _ := messageNode["proxies"].([]interface{})

	var activeProxy map[string]interface{}
	for _, pElem := range proxies {
		pMap, ok := pElem.(map[string]interface{})
		if !ok {
			continue
		}
		// status == 1 represents an active proxy port
		var status float64
		switch st := pMap["status"].(type) {
		case float64:
			status = st
		case string:
			if parsed, err := strconv.ParseFloat(st, 64); err == nil {
				status = parsed
			}
		}

		if status == 1 {
			activeProxy = pMap
			break
		}
	}

	if activeProxy != nil {
		log.Printf("[ASocks] Selected active proxy: %v (%v)", activeProxy["name"], activeProxy["cityName"])
		return s.formatProxyResponse(activeProxy), nil
	}

	// 2. If no active proxies exist, create a new residential proxy port in the GB region
	log.Println("[ASocks] No active ports found. Dynamically creating a new ASocks residential port (default: GB)...")
	createURL := fmt.Sprintf("%s/proxy/create-port?apiKey=%s", s.BaseURL, s.APIKey)

	payload := map[string]interface{}{
		"count":         1,
		"proxy_type_id": 1, // Residential proxy type
		"country":       "GB",
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create port payload: %w", err)
	}

	createResp, err := client.Post(createURL, "application/json", strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create new ASocks port: %w", err)
	}
	defer createResp.Body.Close()

	createBody, err := io.ReadAll(createResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read create response: %w", err)
	}

	if createResp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to create port: HTTP %d - %s", createResp.StatusCode, string(createBody))
	}

	var createData map[string]interface{}
	if err := json.Unmarshal(createBody, &createData); err != nil {
		return nil, fmt.Errorf("failed to decode create port response: %w", err)
	}

	createSuccess, _ := createData["success"].(bool)
	if !createSuccess {
		return nil, fmt.Errorf("ASocks API failed to create port: %v", createData)
	}

	// Query again to get full credentials of newly provisioned port
	time.Sleep(1 * time.Second) // small delay to let ASocks provision the port
	return s.GetActiveProxy()
}

func (s *ASocksService) formatProxyResponse(proxyData map[string]interface{}) *ProxyInfo {
	country, _ := proxyData["countryCode"].(string)
	if country == "" {
		country = "GB"
	}

	city, _ := proxyData["cityName"].(string)
	if city == "" {
		city = "London"
	}

	state, _ := proxyData["stateName"].(string)
	if state == "" {
		state = "England"
	}

	template, _ := proxyData["template"].(string)

	return &ProxyInfo{
		ProxyURL:    template,
		CountryCode: strings.ToUpper(country),
		City:        city,
		State:       state,
	}
}
