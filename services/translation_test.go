package services

import (
	"strings"
	"sync"
	"testing"
)

func TestHasCJK(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		{"Hello World", false},
		{"12345!@#$", false},
		{"Hello, how are you?", false},
		{"iPhone 15 Pro Max", false},
		{"全新闲鱼宝贝", true},       // Simplified Chinese
		{"閒魚寶貝", true},         // Traditional Chinese
		{"こんにちは", true},        // Japanese Hiragana
		{"アイフォン", true},        // Japanese Katakana
		{"안녕하세요", true},        // Korean Hangul
		{"Xbox Series X 全新", true}, // Mixed alphanumeric and Chinese
	}

	for _, tc := range tests {
		result := hasCJK(tc.text)
		if result != tc.expected {
			t.Errorf("hasCJK(%q) = %v; expected %v", tc.text, result, tc.expected)
		}
	}
}

func TestTranslate_NoCJKReturnsInstantly(t *testing.T) {
	// Initialize a dummy translator
	trans := &DeepLTranslationService{
		APIKey:   "",
		Endpoint: "",
		cache:    sync.Map{},
	}

	input := "iPhone 15 Pro Max 128GB"
	output := trans.Translate(input, "ZH", "EN")

	// Because there is no CJK character, it should return the input text immediately
	if output != input {
		t.Errorf("Translate(%q) = %q; expected %q", input, output, input)
	}
}

func TestTranslate_Caching(t *testing.T) {
	trans := &DeepLTranslationService{
		APIKey:   "",
		Endpoint: "",
		cache:    sync.Map{},
	}

	// Prime the cache with a mock translation
	text := "全新"
	translatedText := "Brand New"
	cacheKey := "ZH:EN:" + text
	trans.cache.Store(cacheKey, translatedText)

	// Since it is cached, it should return "Brand New" immediately without attempting to call external APIs
	output := trans.Translate(text, "ZH", "EN")
	if output != translatedText {
		t.Errorf("Expected cached translation %q, but got %q", translatedText, output)
	}
}

func TestTranslate_GoogleFallback(t *testing.T) {
	// We can test if the Google Translate fallback is functional.
	// (Note: This is an integration test as it makes an outbound HTTP request to Google Translate API,
	// but it is public, free, and generally extremely reliable).
	trans := &DeepLTranslationService{
		APIKey:   "",
		Endpoint: "",
		cache:    sync.Map{},
	}

	text := "全新" // "Brand New" in Chinese
	output := trans.Translate(text, "zh", "en")

	// Expecting something similar to "brand new", "completely new", "new"
	lowerOutput := tcNormalize(output)
	if !containsAny(lowerOutput, []string{"new", "brand new", "fresh"}) {
		t.Errorf("Google fallback translation for %q was %q; expected a translation containing 'new' or 'brand new'", text, output)
	}
}

func tcNormalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func containsAny(s string, sub []string) bool {
	for _, sb := range sub {
		if strings.Contains(s, sb) {
			return true
		}
	}
	return false
}
