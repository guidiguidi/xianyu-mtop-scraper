package services

import (
	"log"
	"regexp"
	"strings"
)

var cyrillicRe = regexp.MustCompile(`[\x{0400}-\x{04FF}]`)

// ExpandQuery takes a user's search query (Russian/English/etc.) and transforms it into optimized Chinese terms for Xianyu.
func ExpandQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	// 1. Lowercase for synonym matching
	lower := strings.ToLower(query)

	// Custom Russian/English slang and brand synonym mappings for Xianyu
	brandSynonyms := map[string]string{
		"баленсиага":   "balenciaga 巴黎世家",
		"хром хартс":   "chrome hearts 克罗心",
		"хромы":        "chrome hearts 克罗心",
		"маржела":      "maison margiela 马吉拉",
		"арктерикс":    "始祖鸟 arcteryx",
		"найк":         "nike 耐克",
		"адидас":       "adidas 阿迪дас",
		"ветмо":        "vetements vtm",
		"рикроуэнс":    "ro rick owens",
		"рик овенс":    "ro rick owens",
	}

	for k, v := range brandSynonyms {
		if strings.Contains(lower, k) {
			lower = strings.ReplaceAll(lower, k, v)
		}
	}

	// Term mappings
	termMappings := map[string]string{
		"штаны":     "裤子",
		"брюки":     "裤子",
		"куртка":    "外套",
		"ветровка":  "外套",
		"пуховик":   "羽绒服",
		"худи":      "卫衣",
		"зипка":     "拉链卫衣",
		"зип":       "拉链",
		"свитшот":   "卫衣",
		"футболка":  "T恤",
		"майка":     "背心",
		"кроссовки": "鞋",
		"кеды":      "鞋",
		"обувь":     "鞋",
		"сумка":     "包",
		"рюкзак":    "双肩包",
		"кольцо":    "戒指",
		"ремень":    "腰带",
		"кепка":     "帽子",
		"шапка":     "帽子",
		"новые":     "全新",
		"новый":     "全新",
		"оригинал":  "正品 保真",
	}

	for k, v := range termMappings {
		if strings.Contains(lower, k) {
			lower = strings.ReplaceAll(lower, k, v)
		}
	}

	// 2. If the query still contains Cyrillic characters, use Google Translate to translate them to Chinese
	if cyrillicRe.MatchString(lower) {
		log.Printf("[QueryExpander] Translating query to Chinese: '%s'", lower)
		if GlobalTranslator != nil {
			translated := GlobalTranslator.Translate(lower, "auto", "ZH")
			if translated != "" && translated != lower {
				lower = translated
			}
		}
	}

	// Ensure common brands are represented well
	lower = strings.ReplaceAll(lower, "rick owens", "ro rick owens")
	lower = strings.ReplaceAll(lower, "chrome hearts", "chrome hearts 克罗心")

	// Clean up whitespaces
	words := strings.Fields(lower)
	// Remove duplicate words in query
	seen := make(map[string]bool)
	var uniqueWords []string
	for _, w := range words {
		wLower := strings.ToLower(w)
		if !seen[wLower] {
			seen[wLower] = true
			uniqueWords = append(uniqueWords, w)
		}
	}

	expanded := strings.Join(uniqueWords, " ")
	log.Printf("[QueryExpander] Expanded query: '%s' ➔ '%s'", query, expanded)
	return expanded
}
