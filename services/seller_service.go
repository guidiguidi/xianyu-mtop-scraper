package services

import (
	"fmt"
	"strings"

	"goofish-backend/goofish"
)

// GetSellerProfileCard fetches the seller profile and builds a premium-designed Russian Telegram HTML card.
func GetSellerProfileCard(client *goofish.SearchClient, userID string) (string, error) {
	rawProfile, err := client.GetUserProfile(userID)
	if err != nil {
		return "", err
	}

	prof := goofish.ParseSellerProfile(rawProfile)
	if prof == nil {
		return "", fmt.Errorf("failed to parse seller profile response")
	}

	// Map Zhima level to premium Russian badges
	zhimaBadge := "⚪️ <i>Zhima Credit not available or hidden</i>"
	if prof.ZhimaLevel != "" {
		switch {
		case strings.Contains(prof.ZhimaLevel, "极好") || strings.Contains(prof.ZhimaLevel, "Outstanding"):
			zhimaBadge = "🟢 <b>Zhima Credit: Outstanding (极好)</b> — 🛡️ Maximum Trust!"
		case strings.Contains(prof.ZhimaLevel, "优秀") || strings.Contains(prof.ZhimaLevel, "Excellent"):
			zhimaBadge = "🔵 <b>Zhima Credit: Excellent (优秀)</b> — ✅ High Trust."
		case strings.Contains(prof.ZhimaLevel, "良好") || strings.Contains(prof.ZhimaLevel, "Good"):
			zhimaBadge = "🟡 <b>Zhima Credit: Good (良好)</b> — ⚠️ Moderate Trust."
		case strings.Contains(prof.ZhimaLevel, "中等") || strings.Contains(prof.ZhimaLevel, "Medium"):
			zhimaBadge = "🟠 <b>Zhima Credit: Medium (中等)</b> — ⚠️ Exercise caution."
		default:
			zhimaBadge = fmt.Sprintf("🔵 <b>Zhima Credit: %s</b>", prof.ZhimaLevel)
		}
	}

	authBadge := "❌ Real-name identity unverified"
	if prof.Authenticated {
		authBadge = "✅ Real-name verified (实名认证)"
	}

	genderBadge := "👤 Seller"
	if prof.Gender == "F" || prof.Gender == "female" || prof.Gender == "0" {
		genderBadge = "👩 Seller (Female)"
	} else if prof.Gender == "M" || prof.Gender == "male" || prof.Gender == "1" {
		genderBadge = "👨 Seller (Male)"
	}

	registerDaysTrans := "New account"
	if prof.RegisterDays != "" {
		if GlobalTranslator != nil {
			registerDaysTrans = GlobalTranslator.Translate(prof.RegisterDays, "ZH", "RU")
		} else {
			registerDaysTrans = prof.RegisterDays
		}
	}

	card := fmt.Sprintf(
		"👤 <b>Xianyu Seller Profile</b>\n\n"+
			"🏷 Handle: <b>%s</b>\n"+
			"🆔 ID: <code>%s</code>\n\n"+
			"🛡 <b>Trust Status:</b>\n"+
			"%s\n"+
			"💳 Verification: <i>%s</i>\n\n"+
			"📅 Account Age: <b>%s</b>\n"+
			"⚧ Gender: <i>%s</i>\n\n"+
			"🔗 <a href=\"https://www.goofish.com/user?id=%s\">Open Profile on Xianyu</a>",
		prof.Nickname, prof.UserID, zhimaBadge, authBadge, registerDaysTrans, genderBadge, prof.UserID,
	)

	return card, nil
}
