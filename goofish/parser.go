package goofish

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// ParseSearchResponse decodes raw response bytes/JSON into a slice of GoofishItem listings.
func ParseSearchResponse(responseMap map[string]interface{}) []GoofishItem {
	var parsedItems []GoofishItem

	dataNode := getMap(responseMap, "data")
	if dataNode == nil {
		log.Println("[Parser] Warning: No data node found in search response.")
		return parsedItems
	}

	resultList, ok := dataNode["resultList"].([]interface{})
	if !ok {
		log.Println("[Parser] Warning: resultList missing or invalid type.")
		return parsedItems
	}

	debugDumped := false
	for _, elem := range resultList {
		elemMap, ok := elem.(map[string]interface{})
		if !ok {
			continue
		}

		// Only process real item blocks (type == "DX" or similar)
		elemType := getString(elemMap, "type")
		if elemType != "DX" && elemType != "" {
			continue
		}

		elemData := getMap(elemMap, "data")
		if elemData == nil {
			continue
		}

		itemNode := getMap(elemData, "item")
		if itemNode == nil {
			continue
		}

		mainNode := getMap(itemNode, "main")
		// exContent moved from itemNode level into mainNode in recent API version
		exContent := getMap(mainNode, "exContent")
		if exContent == nil {
			exContent = getMap(itemNode, "exContent") // backward compat fallback
		}

		// DEBUG: dump the first item's raw structure to identify field paths
		if !debugDumped {
			debugDumped = true
			if rawItemJSON, err := json.Marshal(itemNode); err == nil {
				log.Printf("[Parser][DEBUG] First itemNode raw JSON: %s", string(rawItemJSON))
			}
			if rawExJSON, err := json.Marshal(exContent); err == nil {
				log.Printf("[Parser][DEBUG] First exContent raw JSON: %s", string(rawExJSON))
			}
			if rawMainJSON, err := json.Marshal(mainNode); err == nil {
				log.Printf("[Parser][DEBUG] First mainNode raw JSON: %s", string(rawMainJSON))
			}
		}

		// 1. Item ID
		itemID := getString(itemNode, "itemId")
		if itemID == "" {
			// Fallback to clickParam -> args -> id
			clickParam := getMap(mainNode, "clickParam")
			clickArgs := getMap(clickParam, "args")
			itemID = getString(clickArgs, "id")
		}
		if itemID == "" {
			continue
		}

		// 2. Title
		title := parseTitle(exContent)

		// 3. Price
		priceStr := parsePrice(exContent)

		// 4. Image URL
		picURL := fixURL(getString(exContent, "picUrl"))

		// 5. Seller Info
		clickParam := getMap(mainNode, "clickParam")
		userID := parseUserID(exContent, clickParam)
		userNickname := parseNickname(exContent)
		userAvatar := fixURL(getString(exContent, "userAvatarUrl"))

		// 6. Area
		area := getString(exContent, "area")

		// 7. Want count
		wantCount := getString(exContent, "want")

		// 8. Target URL
		targetURL := getString(mainNode, "targetUrl")

		// 9. Publish Time & Age Calculation
		publishTS := parsePublishTime(clickParam)
		var ageMinutes *int64
		if publishTS != nil {
			age := calcAgeMinutes(*publishTS)
			ageMinutes = &age
		}

		// 10. Fish Tags (is_verified, is_new_item, relative_time)
		isVerified, isNewItem, relativeTime := parseFishTags(exContent)

		item := GoofishItem{
			ItemID:       itemID,
			Title:        title,
			Price:        priceStr,
			PicURL:       picURL,
			UserID:       userID,
			UserNickname: userNickname,
			UserAvatar:   userAvatar,
			Area:         area,
			WantCount:    wantCount,
			TargetURL:    targetURL,
			PublishTS:    publishTS,
			RelativeTime: relativeTime,
			IsVerified:   isVerified,
			IsNewItem:    isNewItem,
			AgeMinutes:   ageMinutes,
		}

		parsedItems = append(parsedItems, item)
	}

	log.Printf("[Parser] Extracted %d product items from response successfully", len(parsedItems))
	return parsedItems
}

// ParseSearchResponseFromBytes decodes raw JSON response bytes.
func ParseSearchResponseFromBytes(data []byte) ([]GoofishItem, error) {
	var responseMap map[string]interface{}
	if err := json.Unmarshal(data, &responseMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return ParseSearchResponse(responseMap), nil
}

// --- Safe Navigation Map Helpers ---

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		if res, ok := v.(map[string]interface{}); ok {
			return res
		}
	}
	return nil
}

func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		// Fallback for numbers, bools, etc.
		if v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// --- Private Parsing Helpers ---

func fixURL(url string) string {
	if url == "" {
		return ""
	}
	if strings.HasPrefix(url, "//") {
		return "https:" + url
	}
	return url
}

func parsePublishTime(clickParam map[string]interface{}) *int64 {
	args := getMap(clickParam, "args")
	if args == nil {
		return nil
	}
	valRaw, ok := args["publishTime"]
	if !ok || valRaw == nil {
		return nil
	}

	var ts int64
	switch v := valRaw.(type) {
	case float64:
		ts = int64(v)
	case int64:
		ts = v
	case int:
		ts = int64(v)
	case string:
		var parsed int64
		_, err := fmt.Sscan(v, &parsed)
		if err == nil {
			ts = parsed
		} else {
			return nil
		}
	default:
		return nil
	}

	return &ts
}

func calcAgeMinutes(publishTSMs int64) int64 {
	nowMs := time.Now().UnixNano() / int64(time.Millisecond)
	deltaMs := nowMs - publishTSMs
	if deltaMs < 0 {
		return 0
	}
	return deltaMs / 60000
}

func parseFishTags(exContent map[string]interface{}) (bool, bool, string) {
	isVerified := false
	isNewItem := false
	relativeTime := ""

	fishTags := getMap(exContent, "fishTags")
	if fishTags == nil {
		return isVerified, isNewItem, relativeTime
	}

	var allTags []interface{}
	for _, key := range []string{"r1", "r2", "r3", "r4"} {
		group := getMap(fishTags, key)
		if group == nil {
			continue
		}
		tagList, ok := group["tagList"].([]interface{})
		if ok {
			allTags = append(allTags, tagList...)
		}
	}

	for _, tag := range allTags {
		tagMap, ok := tag.(map[string]interface{})
		if !ok {
			continue
		}

		content := getString(tagMap, "content")
		labelID := getString(tagMap, "labelId")

		// Platform verification badge
		if strings.Contains(content, "验货宝") || labelID == "5" || labelID == "6" {
			isVerified = true
		}

		// Brand new condition tag
		if strings.Contains(content, "全新") {
			isNewItem = true
		}

		// Relative publish time
		if labelID == "752" || strings.Contains(content, "发布") || strings.Contains(content, "小时前") || strings.Contains(content, "分钟前") || strings.Contains(content, "天前") {
			if content != "" && relativeTime == "" {
				relativeTime = content
			}
		}
	}

	return isVerified, isNewItem, relativeTime
}

func parsePrice(exContent map[string]interface{}) string {
	// 1. soldPrice from detailParams
	detailParams := getMap(exContent, "detailParams")
	if detailParams != nil {
		soldPrice := getString(detailParams, "soldPrice")
		if soldPrice == "" {
			soldPrice = getString(detailParams, "price")
		}
		if soldPrice != "" {
			return "¥" + soldPrice
		}
	}

	// 2. Price array components
	if priceComponents, ok := exContent["price"].([]interface{}); ok {
		var sb strings.Builder
		for _, p := range priceComponents {
			pMap, ok := p.(map[string]interface{})
			if ok {
				sb.WriteString(getString(pMap, "text"))
			}
		}
		joined := sb.String()
		if joined != "" {
			return joined
		}
	}

	// 3. Plain price string
	if priceStr := getString(exContent, "price"); priceStr != "" {
		return priceStr
	}

	return "¥?"
}

func parseTitle(exContent map[string]interface{}) string {
	detailParams := getMap(exContent, "detailParams")
	if detailParams != nil {
		title := getString(detailParams, "title")
		if title != "" {
			return strings.TrimSpace(title)
		}
	}

	title := getString(exContent, "title")
	if title != "" {
		return strings.TrimSpace(title)
	}

	return "Без названия"
}

func parseNickname(exContent map[string]interface{}) string {
	detailParams := getMap(exContent, "detailParams")
	if detailParams != nil {
		nick := getString(detailParams, "userNick")
		if nick != "" {
			return nick
		}
	}

	nick := getString(exContent, "userNickName")
	if nick != "" {
		return nick
	}

	return "Seller"
}

func parseUserID(exContent map[string]interface{}, clickParam map[string]interface{}) string {
	// 1. from detailParams
	detailParams := getMap(exContent, "detailParams")
	if detailParams != nil {
		uid := getString(detailParams, "userId")
		if uid != "" {
			return uid
		}
	}
	// 2. from clickParam args
	args := getMap(clickParam, "args")
	if args != nil {
		uid := getString(args, "userId")
		if uid == "" {
			uid = getString(args, "user_id")
		}
		if uid == "" {
			uid = getString(args, "sellerId")
		}
		if uid == "" {
			uid = getString(args, "seller_id")
		}
		if uid != "" {
			return uid
		}
	}
	// 3. direct from exContent
	uid := getString(exContent, "userId")
	if uid != "" {
		return uid
	}
	return ""
}

// GoofishSellerProfile represents standard Alipay/Zhima verification and background info for a seller.
type GoofishSellerProfile struct {
	UserID        string `json:"user_id"`
	Nickname      string `json:"nickname"`
	Avatar        string `json:"avatar"`
	ZhimaLevel    string `json:"zhima_level"`   // e.g. "芝麻极好", "芝麻优秀"
	RegisterDays  string `json:"register_days"` // e.g. "已来闲鱼500天"
	Authenticated bool   `json:"authenticated"` // 实名认证
	Gender        string `json:"gender"`        // 性别 (F/M/etc.)
}

// ParseSellerProfile extracts a unified profile object from the mtop.idle.web.user.page.head API response.
func ParseSellerProfile(responseMap map[string]interface{}) *GoofishSellerProfile {
	dataNode := getMap(responseMap, "data")
	if dataNode == nil {
		return nil
	}

	userCard := getMap(dataNode, "idleUserCard")
	if userCard == nil {
		// Try root keys
		userCard = dataNode
	}

	zhimaNode := getMap(userCard, "zhimaLevelInfo")
	zhimaTitle := ""
	if zhimaNode != nil {
		zhimaTitle = getString(zhimaNode, "title")
	}
	if zhimaTitle == "" {
		zhimaTitle = getString(userCard, "zhimaLevel")
	}
	if zhimaTitle == "" {
		zhimaTitle = getString(userCard, "zhimaTitle")
	}

	// Extract register days
	registerInfo := getString(userCard, "comeIdleDays")
	if registerInfo == "" {
		registerInfo = getString(userCard, "registerDaysInfo")
	}

	// Realname auth status
	isAuthed := getString(userCard, "realNameAuth") == "true" || getBool(userCard, "realNameAuth")

	profile := &GoofishSellerProfile{
		UserID:        getString(userCard, "userId"),
		Nickname:      getString(userCard, "nick"),
		Avatar:        fixURL(getString(userCard, "avatar")),
		ZhimaLevel:    zhimaTitle,
		RegisterDays:  registerInfo,
		Authenticated: isAuthed,
		Gender:        getString(userCard, "gender"),
	}

	return profile
}

func getBool(m map[string]interface{}, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
		if s, ok := v.(string); ok {
			return s == "true" || s == "1"
		}
	}
	return false
}
