package goofish

import (
	"fmt"
)

// GoofishItem represents a clean, parsed product listing from the Goofish marketplace.
type GoofishItem struct {
	ItemID           string  `json:"item_id"`
	Title            string  `json:"title"`
	TitleTranslated  string  `json:"title_translated,omitempty"`
	Price            string  `json:"price"`
	PicURL           string  `json:"pic_url"`
	UserID           string  `json:"user_id"`
	UserNickname     string  `json:"user_nickname"`
	UserAvatar       string  `json:"user_avatar"`
	Area             string  `json:"area"`
	AreaTranslated   string  `json:"area_translated,omitempty"`
	WantCount        string  `json:"want_count"`
	TargetURL        string  `json:"target_url"`
	PublishTS        *int64  `json:"publish_ts"` // Unix timestamp in ms
	RelativeTime     string  `json:"relative_time"`
	IsVerified       bool    `json:"is_verified"`
	IsNewItem        bool    `json:"is_new_item"`
	AgeMinutes       *int64  `json:"age_minutes"`
	TelegramCaption  string  `json:"telegram_caption,omitempty"`
}

// ShareURL returns the standard Web URL for the product page.
func (item *GoofishItem) ShareURL() string {
	return fmt.Sprintf("https://www.goofish.com/item?id=%s", item.ItemID)
}

// FreshnessLabel calculates a human-readable Russian label based on publish age.
func (item *GoofishItem) FreshnessLabel() string {
	if item.AgeMinutes == nil {
		return ""
	}
	age := *item.AgeMinutes
	if age < 10 {
		return "🔥 Just now"
	}
	if age < 60 {
		return fmt.Sprintf("🟢 %d min ago", age)
	}
	if age < 1440 {
		hours := age / 60
		return fmt.Sprintf("🟡 %d hr ago", hours)
	}
	days := age / 1440
	return fmt.Sprintf("⚪️ %d d ago", days)
}

// ToMap converts GoofishItem to a standard map suitable for JSON outputs.
func (item *GoofishItem) ToMap() map[string]interface{} {
	m := map[string]interface{}{
		"item_id":          item.ItemID,
		"title":            item.Title,
		"price":            item.Price,
		"pic_url":          item.PicURL,
		"user_id":          item.UserID,
		"user_nickname":    item.UserNickname,
		"user_avatar":      item.UserAvatar,
		"area":             item.Area,
		"want_count":       item.WantCount,
		"target_url":       item.TargetURL,
		"share_url":        item.ShareURL(),
		"publish_ts":       item.PublishTS,
		"relative_time":    item.RelativeTime,
		"is_verified":      item.IsVerified,
		"is_new_item":      item.IsNewItem,
		"age_minutes":      item.AgeMinutes,
		"freshness_label":  item.FreshnessLabel(),
	}

	if item.TitleTranslated != "" {
		m["title_translated"] = item.TitleTranslated
	}
	if item.AreaTranslated != "" {
		m["area_translated"] = item.AreaTranslated
	}
	if item.TelegramCaption != "" {
		m["telegram_caption"] = item.TelegramCaption
	}

	return m
}
