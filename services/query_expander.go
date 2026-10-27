package services

import (
	"log"
	"strings"
)

// ExpandQuery takes a user search query and expands synonyms and tags for Xianyu / Goofish.
func ExpandQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	lower := strings.ToLower(query)

	// Brand and slang mappings for Xianyu
	brandSynonyms := map[string]string{
		"rick owens":    "ro rick owens",
		"chrome hearts": "chrome hearts 克罗心",
		"margiela":      "maison margiela 马吉拉",
		"arcteryx":      "始祖鸟 arcteryx",
		"balenciaga":    "balenciaga 巴黎世家",
		"vetements":     "vetements vtm",
	}

	for k, v := range brandSynonyms {
		if strings.Contains(lower, k) {
			lower = strings.ReplaceAll(lower, k, v)
		}
	}

	// English category mappings to Chinese marketplace tags
	termMappings := map[string]string{
		"pants":     "裤子",
		"trousers":  "裤子",
		"jacket":    "外套",
		"windbreaker": "外套",
		"down jacket": "羽绒服",
		"puffer":    "羽绒服",
		"hoodie":    "卫衣",
		"zip hoodie": "拉链卫衣",
		"sweatshirt": "卫衣",
		"t-shirt":   "T恤",
		"tee":       "T恤",
		"tank top":  "背心",
		"shoes":     "鞋",
		"sneakers":  "鞋",
		"boots":     "靴子",
		"bag":       "包",
		"backpack":  "双肩包",
		"ring":      "戒指",
		"belt":      "腰带",
		"cap":       "帽子",
		"hat":       "帽子",
		"brand new": "全新",
		"new":       "全新",
		"authentic": "正品 保真",
	}

	for k, v := range termMappings {
		if strings.Contains(lower, k) {
			lower = strings.ReplaceAll(lower, k, v)
		}
	}

	// Remove duplicate words in query
	words := strings.Fields(lower)
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
