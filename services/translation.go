package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib-x/deepl"
)

// DeepLTranslationService handles requests to DeepL or Google Translate.
type DeepLTranslationService struct {
	APIKey   string
	Endpoint string
	cache    sync.Map // thread-safe cache
}

// GlobalTranslator holds the centralized translation service instance.
var GlobalTranslator *DeepLTranslationService

// Regex to check for CJK (Chinese, Japanese, Korean) characters.
// This includes Hiragana, Katakana, Kanji, and Chinese characters.
var cjkCharsRe = regexp.MustCompile(`[\x{3000}-\x{303f}\x{3040}-\x{309f}\x{30a0}-\x{30ff}\x{4e00}-\x{9faf}\x{ff00}-\x{ff9f}\x{ac00}-\x{d7af}]`)

var deeplLimiter = struct {
	sync.Mutex
	lastRequest   time.Time
	cooldownUntil time.Time
	lastLog       time.Time
}{
	lastRequest: time.Now().Add(-time.Hour),
}

const deeplMinInterval = time.Second
const deeplCooldown = 5 * time.Minute

// InitializeTranslator sets up the global translator.
func InitializeTranslator(apiKey string) {
	apiKey = strings.TrimSpace(apiKey)
	var endpoint string

	if apiKey != "" {
		if strings.HasSuffix(apiKey, ":fx") {
			endpoint = "https://api-free.deepl.com/v2/translate"
			log.Println("[Translator] DeepL Translation Service initialized using FREE API endpoint.")
		} else {
			endpoint = "https://api.deepl.com/v2/translate"
			log.Println("[Translator] DeepL Translation Service initialized using PRO API endpoint.")
		}
	} else {
		log.Println("[Translator] DeepL API Key is missing. Translation service will operate using free/fallback engines.")
	}

	GlobalTranslator = &DeepLTranslationService{
		APIKey:   apiKey,
		Endpoint: endpoint,
	}
}

// hasCJK checks if the text contains any Chinese or Japanese characters.
func hasCJK(text string) bool {
	return cjkCharsRe.MatchString(text)
}

// Translate translates text from source to target language with in-memory caching.
func (t *DeepLTranslationService) Translate(text, sourceLang, targetLang string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// 1. Check if the text contains Chinese/Japanese characters.
	// If there are none, return the original text instantly to prevent unnecessary API calls.
	if !hasCJK(text) {
		return text
	}

	// 2. Check thread-safe cache
	cacheKey := fmt.Sprintf("%s:%s:%s", strings.ToUpper(sourceLang), strings.ToUpper(targetLang), text)
	if cachedVal, ok := t.cache.Load(cacheKey); ok {
		if s, ok := cachedVal.(string); ok {
			return s
		}
	}

	var result string

	// 3. If a premium/free official API key is provided, use the official DeepL API.
	if t.APIKey != "" && t.Endpoint != "" {
		result = t.translateOfficial(text, sourceLang, targetLang)
	}

	// 4. Fallback to DeepL Free scrapeless library if official key is absent or failed.
	if result == "" {
		result = t.translateDeepLFree(text, sourceLang, targetLang)
	}

	// 5. Fallback to Google Translate if both official and free DeepL failed.
	if result == "" {
		result = t.translateGoogle(text, sourceLang, targetLang)
	}

	// 6. Cache the result if found, otherwise return the original text.
	if result != "" && result != text {
		t.cache.Store(cacheKey, result)
		return result
	}

	return text
}

// translateOfficial handles translation requests using official DeepL developer credentials.
func (t *DeepLTranslationService) translateOfficial(text, sourceLang, targetLang string) string {
	payload := map[string]interface{}{
		"text":        []string{text},
		"target_lang": strings.ToUpper(targetLang),
		"source_lang": strings.ToUpper(sourceLang),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[Translator] Warning: Failed to marshal payload for official translation: %v", err)
		return ""
	}

	req, err := http.NewRequest("POST", t.Endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		log.Printf("[Translator] Warning: Failed to build official HTTP request: %v", err)
		return ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("DeepL-Auth-Key %s", t.APIKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Translator] Warning: Official DeepL API call failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Translator] Warning: Failed to read official response body: %v", err)
		return ""
	}

	if resp.StatusCode != 200 {
		log.Printf("[Translator] Official DeepL API returned error status HTTP %d: %s", resp.StatusCode, string(bodyBytes))
		return ""
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &resMap); err != nil {
		return ""
	}

	translations, ok := resMap["translations"].([]interface{})
	if !ok || len(translations) == 0 {
		return ""
	}

	transNode, ok := translations[0].(map[string]interface{})
	if !ok {
		return ""
	}

	translatedText, _ := transNode["text"].(string)
	return strings.TrimSpace(translatedText)
}

// translateDeepLFree translates via the free API (without key) using rate limits and cooldown protection.
func (t *DeepLTranslationService) translateDeepLFree(text, sourceLang, targetLang string) string {
	deeplLimiter.Lock()
	now := time.Now()
	if now.Before(deeplLimiter.cooldownUntil) {
		deeplLimiter.Unlock()
		return ""
	}
	if wait := deeplMinInterval - now.Sub(deeplLimiter.lastRequest); wait > 0 {
		deeplLimiter.Unlock()
		time.Sleep(wait)
		deeplLimiter.Lock()
	}
	deeplLimiter.lastRequest = time.Now()
	deeplLimiter.Unlock()

	resp, err := deepl.Translate(strings.ToUpper(sourceLang), strings.ToUpper(targetLang), text)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too many requests") {
			deeplLimiter.Lock()
			deeplLimiter.cooldownUntil = time.Now().Add(deeplCooldown)
			shouldLog := time.Since(deeplLimiter.lastLog) > time.Minute
			deeplLimiter.lastLog = time.Now()
			deeplLimiter.Unlock()
			if shouldLog {
				log.Printf("[Translator] DeepL free hit rate limit; cooling down for %v and using Google fallback", deeplCooldown)
			}
			return ""
		}
		log.Printf("[Translator] DeepL free translation failed: %v", err)
		return ""
	}
	if resp == nil || resp.Result.Texts == nil || len(resp.Result.Texts) == 0 {
		return ""
	}
	return resp.Result.Texts[0].Text.Text
}

// translateGoogle is a fallback translating mechanism using translate.googleapis.com.
func (t *DeepLTranslationService) translateGoogle(text, sourceLang, targetLang string) string {
	encoded := url.QueryEscape(text)
	sl := strings.ToLower(sourceLang)
	tl := strings.ToLower(targetLang)
	apiURL := fmt.Sprintf("https://translate.googleapis.com/translate_a/single?client=gtx&sl=%s&tl=%s&dt=t&q=%s", sl, tl, encoded)

	resp, err := http.Get(apiURL)
	if err != nil {
		log.Printf("[Translator] Google Translate request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var data []interface{}
	if err := json.Unmarshal(body, &data); err != nil || len(data) == 0 {
		return ""
	}
	sentences, ok := data[0].([]interface{})
	if !ok {
		return ""
	}

	var parts []string
	for _, s := range sentences {
		arr, ok := s.([]interface{})
		if !ok || len(arr) == 0 {
			continue
		}
		if translated, ok := arr[0].(string); ok {
			parts = append(parts, translated)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "")
}
