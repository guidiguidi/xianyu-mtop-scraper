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
	zhimaBadge := "⚪️ <i>Рейтинг Zhima не определен или скрыт</i>"
	if prof.ZhimaLevel != "" {
		switch {
		case strings.Contains(prof.ZhimaLevel, "极好") || strings.Contains(prof.ZhimaLevel, "Outstanding"):
			zhimaBadge = "🟢 <b>Zhima Credit: Превосходно (极好)</b> — 🛡️ Максимальное доверие!"
		case strings.Contains(prof.ZhimaLevel, "优秀") || strings.Contains(prof.ZhimaLevel, "Excellent"):
			zhimaBadge = "🔵 <b>Zhima Credit: Отлично (优秀)</b> — ✅ Высокое доверие."
		case strings.Contains(prof.ZhimaLevel, "良好") || strings.Contains(prof.ZhimaLevel, "Good"):
			zhimaBadge = "🟡 <b>Zhima Credit: Хорошо (良好)</b> — ⚠️ Среднее доверие."
		case strings.Contains(prof.ZhimaLevel, "中等") || strings.Contains(prof.ZhimaLevel, "Medium"):
			zhimaBadge = "🟠 <b>Zhima Credit: Средне (中等)</b> — ⚠️ Покупайте с осторожностью."
		default:
			zhimaBadge = fmt.Sprintf("🔵 <b>Zhima Credit: %s</b>", prof.ZhimaLevel)
		}
	}

	authBadge := "❌ Не подтвержден паспортом"
	if prof.Authenticated {
		authBadge = "✅ Личность подтверждена (实名认证)"
	}

	genderBadge := "👤 Продавец"
	if prof.Gender == "F" || prof.Gender == "female" || prof.Gender == "0" {
		genderBadge = "👩 Продавец (Женский пол)"
	} else if prof.Gender == "M" || prof.Gender == "male" || prof.Gender == "1" {
		genderBadge = "👨 Продавец (Мужской пол)"
	}

	registerDaysTrans := "Новый аккаунт"
	if prof.RegisterDays != "" {
		if GlobalTranslator != nil {
			registerDaysTrans = GlobalTranslator.Translate(prof.RegisterDays, "ZH", "RU")
		} else {
			registerDaysTrans = prof.RegisterDays
		}
	}

	card := fmt.Sprintf(
		"👤 <b>Профиль Продавца Xianyu</b>\n\n"+
			"🏷 Никнейм: <b>%s</b>\n"+
			"🆔 ID: <code>%s</code>\n\n"+
			"🛡 <b>Статус Доверия:</b>\n"+
			"%s\n"+
			"💳 Верификация: <i>%s</i>\n\n"+
			"📅 Срок на платформе: <b>%s</b>\n"+
			"⚧ Пол: <i>%s</i>\n\n"+
			"🔗 <a href=\"https://www.goofish.com/user?id=%s\">Открыть профиль на Xianyu</a>",
		prof.Nickname, prof.UserID, zhimaBadge, authBadge, registerDaysTrans, genderBadge, prof.UserID,
	)

	return card, nil
}
