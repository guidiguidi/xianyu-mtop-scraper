package handlers

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"sync"

	"goofish-backend/config"
	"goofish-backend/database"
	"goofish-backend/goofish"
	"goofish-backend/services"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// BotHandler manages all command routers, FSM conversational states, and inline callback queries.
type BotHandler struct {
	bot       *tgbotapi.BotAPI
	db        *database.Database
	refresher *services.CookieRefresherService
	states    sync.Map // Map[int64]string for FSM state
}

// NewBotHandler creates a new BotHandler instance.
func NewBotHandler(bot *tgbotapi.BotAPI, db *database.Database, refresher *services.CookieRefresherService) *BotHandler {
	return &BotHandler{
		bot:       bot,
		db:        db,
		refresher: refresher,
	}
}

// HandleUpdate routes a incoming Telegram update to the correct handler.
func (h *BotHandler) HandleUpdate(update tgbotapi.Update) {
	if update.Message != nil {
		h.handleMessage(update.Message)
		return
	}

	if update.CallbackQuery != nil {
		h.handleCallbackQuery(update.CallbackQuery)
		return
	}
}

func (h *BotHandler) handleMessage(msg *tgbotapi.Message) {
	userID := msg.From.ID

	// 1. Verify authorization
	if !config.Settings.IsUserAllowed(userID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ У вас нет доступа к этому боту.")
		h.bot.Send(reply)
		return
	}

	// 2. Handle standard command messages
	if msg.IsCommand() {
		h.states.Delete(userID) // Reset state on command trigger
		switch msg.Command() {
		case "start":
			h.cmdStart(msg)
		case "help":
			h.cmdHelp(msg)
		case "search":
			h.cmdSearchPrompt(msg)
		case "monitor":
			h.cmdAddMonitor(msg)
		case "monitors":
			h.cmdListMonitors(msg)
		case "unmonitor":
			h.cmdRemoveMonitor(msg)
		case "setcookies":
			h.cmdSetCookiesPrompt(msg)
		case "cancel":
			h.cmdCancel(msg)
		case "status":
			h.cmdStatus(msg)
		default:
			reply := tgbotapi.NewMessage(msg.Chat.ID, "❓ Неизвестная команда. Введите /help для списка доступных команд.")
			h.bot.Send(reply)
		}
		return
	}

	// 3. Handle active FSM state messages
	if stateVal, ok := h.states.Load(userID); ok {
		state := stateVal.(string)
		switch state {
		case "waiting_for_query":
			h.processSearchQuery(msg)
		case "waiting_for_cookies":
			h.processCookiesInput(msg)
		}
		return
	}

	// Default fallback response
	reply := tgbotapi.NewMessage(msg.Chat.ID, "💡 Введите /search <запрос> для поиска товаров или /help для справки.")
	h.bot.Send(reply)
}

// --- Commands Handlers ---

func (h *BotHandler) cmdStart(msg *tgbotapi.Message) {
	welcome := "🐟 <b>Привет! Добро пожаловать в Xianyu/Goofish Search & Monitor Bot!</b>\n\n" +
		"Я помогу вам искать товары на китайской барахолке Goofish и получать моментальные оповещения о новых предложениях.\n\n" +
		"📖 <b>Основные Команды:</b>\n" +
		"• `/search <запрос>` - Быстрый интерактивный поиск товаров\n" +
		"• `/monitor <запрос> [<мин_цена> <макс_цена>]` - Начать следить за новыми лотами\n" +
		"• `/monitors` - Показать активные задачи мониторинга\n" +
		"• `/unmonitor <ID>` - Удалить задачу мониторинга\n" +
		"• `/setcookies` - Обновить H5 куки из Antidetect браузера\n" +
		"• `/status` - Проверить статус токенов и прокси\n\n" +
		"ℹ️ Для вызова справки используйте: /help"

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

func (h *BotHandler) cmdHelp(msg *tgbotapi.Message) {
	help := "📖 <b>Справка по использованию бота</b>\n\n" +
		"🔍 <b>Как искать товары?</b>\n" +
		"Используйте команду `/search rickowens`. Бот вернет карточки товаров с фотографиями, ценами в юанях и прямой ссылкой на покупку. Кнопки позволят перелистывать страницы или мгновенно отфильтровать товары по цене.\n\n" +
		"🔔 <b>Как работает мониторинг?</b>\n" +
		"Вы можете подписаться на отслеживание новых товаров. Бот опрашивает Goofish раз в 5 минут, сравнивает результаты и присылает фото-уведомление, если на рынке появился новый лот. Пример: `/monitor rickowens 5000 15000` (следить за Rick Owens от ¥5000 до ¥15000).\n\n" +
		"🍪 <b>Что делать, если запросы не работают?</b>\n" +
		"Goofish H5 API защищен подписью MTOP. Если сессия истекает, получите новые cookies в Antidetect браузере на сайте goofish.com и обновите их в боте с помощью `/setcookies`."

	reply := tgbotapi.NewMessage(msg.Chat.ID, help)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

func (h *BotHandler) cmdSearchPrompt(msg *tgbotapi.Message) {
	args := strings.TrimSpace(msg.CommandArguments())
	if args != "" {
		h.executeSearch(msg.Chat.ID, args, 1, nil, nil)
		return
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, "🔍 Введите поисковый запрос (например: <b>rick owens</b>):")
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
	h.states.Store(msg.From.ID, "waiting_for_query")
}

func (h *BotHandler) processSearchQuery(msg *tgbotapi.Message) {
	h.states.Delete(msg.From.ID)
	query := strings.TrimSpace(msg.Text)
	if query == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Пустой запрос. Попробуйте еще раз с /search.")
		h.bot.Send(reply)
		return
	}
	h.executeSearch(msg.Chat.ID, query, 1, nil, nil)
}

func (h *BotHandler) cmdAddMonitor(msg *tgbotapi.Message) {
	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		help := "⚠️ Некорректный синтаксис.\n\n" +
			"Примеры:\n" +
			"• `/monitor rickowens` - следить за всеми новыми товарами\n" +
			"• `/monitor rickowens 5000 15000` - следить в диапазоне цен от ¥5000 до ¥15000"
		reply := tgbotapi.NewMessage(msg.Chat.ID, help)
		reply.ParseMode = "Markdown"
		h.bot.Send(reply)
		return
	}

	var priceMin, priceMax *int
	var keyword string

	// Extract trailing integer pairs (prices) if present
	parts := strings.Fields(args)
	if len(parts) >= 3 {
		pMinVal, err1 := strconv.Atoi(parts[len(parts)-2])
		pMaxVal, err2 := strconv.Atoi(parts[len(parts)-1])
		if err1 == nil && err2 == nil {
			priceMin = &pMinVal
			priceMax = &pMaxVal
			keyword = strings.Join(parts[:len(parts)-2], " ")
		} else {
			keyword = args
		}
	} else {
		keyword = args
	}

	keyword = strings.TrimSpace(keyword)

	monitorID, err := h.db.AddMonitor(msg.From.ID, keyword, priceMin, priceMax, 300)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при создании мониторинга: "+err.Error())
		h.bot.Send(reply)
		return
	}

	priceFilter := "отсутствует"
	if priceMin != nil || priceMax != nil {
		priceFilter = fmt.Sprintf("¥%d-¥%d", *priceMin, *priceMax)
	}

	response := fmt.Sprintf(
		"✅ <b>Мониторинг успешно запущен!</b>\n\n"+
			"🆔 ID Задачи: <code>%d</code>\n"+
			"🔍 Ключевое слово: <b>%s</b>\n"+
			"💰 Фильтр цен: %s\n"+
			"⏱ Интервал проверки: <code>5 минут</code>",
		monitorID, keyword, priceFilter,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, response)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

func (h *BotHandler) cmdListMonitors(msg *tgbotapi.Message) {
	monitors, err := h.db.GetUserMonitors(msg.From.ID)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Не удалось прочитать список мониторов: "+err.Error())
		h.bot.Send(reply)
		return
	}

	if len(monitors) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "🔍 У вас нет активных задач мониторинга. Запустите одну с помощью /monitor")
		h.bot.Send(reply)
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 <b>Список ваших активных задач мониторинга:</b>\n\n")

	for _, mon := range monitors {
		priceStr := "Любая"
		if mon.PriceMin != nil || mon.PriceMax != nil {
			pMin := 0
			pMax := 0
			if mon.PriceMin != nil {
				pMin = *mon.PriceMin
			}
			if mon.PriceMax != nil {
				pMax = *mon.PriceMax
			}
			priceStr = fmt.Sprintf("¥%d-¥%d", pMin, pMax)
		}

		sb.WriteString(fmt.Sprintf(
			"🔸 <b>ID %d</b>: <b>%s</b>\n"+
				"   💰 Цена: <code>%s</code> | ⏱ Интервал: <code>5 мин</code> | Статус: <code>Активен</code>\n"+
				"   ❌ Удалить: `/unmonitor %d`\n\n",
			mon.ID, html.EscapeString(mon.Keyword), priceStr, mon.ID,
		))
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, sb.String())
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

func (h *BotHandler) cmdRemoveMonitor(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 1 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Укажите ID задачи. Пример: `/unmonitor 1`")
		h.bot.Send(reply)
		return
	}

	monitorID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ ID должен быть числом.")
		h.bot.Send(reply)
		return
	}

	success, err := h.db.DeleteMonitor(monitorID, msg.From.ID)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при удалении монитора: "+err.Error())
		h.bot.Send(reply)
		return
	}

	if success {
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Задача мониторинга <b>ID %d</b> успешно удалена.", monitorID))
		reply.ParseMode = "HTML"
		h.bot.Send(reply)
	} else {
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Задача с <b>ID %d</b> не найдена или принадлежит не вам.", monitorID))
		reply.ParseMode = "HTML"
		h.bot.Send(reply)
	}
}

func (h *BotHandler) cmdSetCookiesPrompt(msg *tgbotapi.Message) {
	welcome := "🍪 <b>Обновление Cookies Goofish</b>\n\n" +
		"Отправьте мне всю строку Cookies (из Antidetect браузера или DevTools).\n" +
		"Обязательно должны присутствовать <code>cookie2</code> и <code>_m_h5_tk</code>.\n\n" +
		"Отправьте /cancel для отмены."

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
	h.states.Store(msg.From.ID, "waiting_for_cookies")
}

func (h *BotHandler) cmdCancel(msg *tgbotapi.Message) {
	h.states.Delete(msg.From.ID)
	reply := tgbotapi.NewMessage(msg.Chat.ID, "🔄 Действие отменено.")
	h.bot.Send(reply)
}

func (h *BotHandler) processCookiesInput(msg *tgbotapi.Message) {
	h.states.Delete(msg.From.ID)
	cookieStr := strings.TrimSpace(msg.Text)

	if cookieStr == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Пустая строка. Попробуйте еще раз с /setcookies.")
		h.bot.Send(reply)
		return
	}

	if !strings.Contains(cookieStr, "_m_h5_tk") {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ В строке Cookies отсутствует обязательный параметр <code>_m_h5_tk</code>. Проверьте копирование и попробуйте еще раз.")
		reply.ParseMode = "HTML"
		h.bot.Send(reply)
		return
	}

	// Save to DB
	if err := h.db.SaveCookies(cookieStr); err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка сохранения Cookies: "+err.Error())
		h.bot.Send(reply)
		return
	}

	statusMsg := tgbotapi.NewMessage(msg.Chat.ID, "💾 Cookies сохранены в базу данных. Проверяю соединение с API...")
	statusSent, _ := h.bot.Send(statusMsg)

	// Validate cookies by running a test search
	go func() {
		client := goofish.NewSearchClient(cookieStr, config.Settings.ProxyURL, func(s string) error {
			return h.db.SaveCookies(s)
		})

		testWord := "test"
		rawRes, err := client.Search(testWord, 1, nil, nil, 1, false)
		if err != nil {
			edit := tgbotapi.NewEditMessageText(msg.Chat.ID, statusSent.MessageID, "⚠️ <b>Cookies сохранены, но при проверке произошла ошибка:</b> <code>"+html.EscapeString(err.Error())+"</code>.\nУбедитесь, что прокси работает корректно.")
			edit.ParseMode = "HTML"
			h.bot.Send(edit)
			return
		}

		retMsg := "Неизвестно"
		if retList, ok := rawRes["ret"].([]interface{}); ok && len(retList) > 0 {
			if s, ok := retList[0].(string); ok {
				retMsg = s
			}
		}

		if strings.Contains(retMsg, "SUCCESS") {
			edit := tgbotapi.NewEditMessageText(msg.Chat.ID, statusSent.MessageID, "✅ <b>Cookies успешно сохранены и проверены!</b> Соединение с Goofish API работает.")
			edit.ParseMode = "HTML"
			h.bot.Send(edit)
		} else {
			edit := tgbotapi.NewEditMessageText(msg.Chat.ID, statusSent.MessageID, "⚠️ <b>Cookies сохранены, но тест API вернул ошибку:</b> <code>"+html.EscapeString(retMsg)+"</code>. Возможно, куки истекли или требуется проверка.")
			edit.ParseMode = "HTML"
			h.bot.Send(edit)
		}
	}()
}

func (h *BotHandler) cmdStatus(msg *tgbotapi.Message) {
	cookies, _ := h.db.GetCookies(config.Settings.XianyuCookies)
	client := goofish.NewSearchClient(cookies, config.Settings.ProxyURL, nil)

	hasToken := "❌ Нет"
	tokenVal := "отсутствует"
	if tok := client.GetToken(); tok != "" {
		hasToken = "✅ Да"
		tokenVal = tok
	}

	proxyStatus := "❌ Отключен"
	if config.Settings.ProxyURL != "" {
		proxyStatus = fmt.Sprintf("<code>%s</code>", html.EscapeString(config.Settings.ProxyURL))
	}

	response := fmt.Sprintf(
		"🖥 <b>Статус Системы Xianyu Bot</b>\n\n"+
			"📡 Прокси: %s\n"+
			"🍪 Наличие H5 токена: %s\n"+
			"🔑 Токен: <code>%s</code>\n\n"+
			"Для обновления Cookies отправьте: /setcookies",
		proxyStatus, hasToken, tokenVal,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, response)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

// --- Search Action Engine ---

func (h *BotHandler) executeSearch(chatID int64, query string, page int, priceMin, priceMax *int) {
	statusMsg := tgbotapi.NewMessage(chatID, "⏳ Выполняю поиск на Goofish...")
	statusSent, _ := h.bot.Send(statusMsg)

	go func() {
		activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
		if err != nil {
			h.editStatusMessage(chatID, statusSent.MessageID, "❌ Ошибка базы данных: "+err.Error())
			return
		}

		client := goofish.NewSearchClient(activeCookies, config.Settings.ProxyURL, func(s string) error {
			return h.db.SaveCookies(s)
		})
		client.ForceCookieRefreshFunc = func(curr string) (string, error) {
			return h.refresher.RefreshCookies(curr)
		}

		expandedQuery := services.ExpandQuery(query)
		rawResp, err := client.Search(expandedQuery, page, priceMin, priceMax, 3, true)
		if err != nil {
			h.editStatusMessage(chatID, statusSent.MessageID, "❌ Ошибка поиска: "+html.EscapeString(err.Error()))
			return
		}

		items := goofish.ParseSearchResponse(rawResp)
		if len(items) == 0 {
			h.editStatusMessage(chatID, statusSent.MessageID, "🔍 Ничего не найдено. Проверьте запрос, ценовой фильтр или обновите cookies через /setcookies.")
			return
		}

		// Concurrent translation utilizing DeepL service
		var wg sync.WaitGroup
		type transResult struct {
			index      int
			titleTrans string
			areaTrans  string
		}
		transChan := make(chan transResult, len(items))

		for idx, item := range items {
			wg.Add(1)
			go func(i int, it goofish.GoofishItem) {
				defer wg.Done()
				tTitle := services.GlobalTranslator.Translate(it.Title, "ZH", "RU")
				tArea := ""
				if it.Area != "" {
					tArea = services.GlobalTranslator.Translate(it.Area, "ZH", "RU")
				}
				transChan <- transResult{index: i, titleTrans: tTitle, areaTrans: tArea}
			}(idx, item)
		}

		wg.Wait()
		close(transChan)

		// Map translations by item index
		transMap := make(map[int]transResult)
		for res := range transChan {
			transMap[res.index] = res
		}

		// Delete loading status message
		deleteMsg := tgbotapi.NewDeleteMessage(chatID, statusSent.MessageID)
		h.bot.Send(deleteMsg)

		// Send item cards individually
		for idx, item := range items {
			trans := transMap[idx]

			escapedTitle := html.EscapeString(trans.titleTrans)
			if len(escapedTitle) > 80 {
				escapedTitle = escapedTitle[:80] + "..."
			}

			escapedPrice := html.EscapeString(item.Price)
			escapedArea := "Не указан"
			if trans.areaTrans != "" {
				escapedArea = html.EscapeString(trans.areaTrans)
			}
			escapedNickname := "Не указан"
			if item.UserNickname != "" {
				escapedNickname = html.EscapeString(item.UserNickname)
			}
			escapedWant := html.EscapeString(item.WantCount)

			// Collect badges
			var badges []string
			if item.IsVerified {
				badges = append(badges, "✅ Проверен платформой")
			}
			if item.IsNewItem {
				badges = append(badges, "🆕 Новый")
			}
			if fLabel := item.FreshnessLabel(); fLabel != "" {
				badges = append(badges, fLabel)
			}
			badgeLine := strings.Join(badges, "  ")

			caption := fmt.Sprintf(
				"🐟 <b>[%d/%d] %s</b>\n\n"+
					"💰 Цена: <code>%s</code>\n"+
					"📍 Регион: %s\n"+
					"👤 Продавец: %s\n",
				idx+1, len(items), escapedTitle, escapedPrice, escapedArea, escapedNickname,
			)

			if escapedWant != "" {
				caption += fmt.Sprintf("📦 %s\n", escapedWant)
			}
			if badgeLine != "" {
				caption += fmt.Sprintf("%s\n", badgeLine)
			}
			caption += fmt.Sprintf("\n<a href=\"%s\">🔗 Открыть на Xianyu</a>", item.ShareURL())

			picURL := item.PicURL
			if picURL == "" {
				picURL = "https://img.alicdn.com/tps/i4/TB1OKd7JXXXXXb1XFXXc8s9FXXX-400-400.png"
			}

			photoCard := tgbotapi.NewPhoto(chatID, tgbotapi.FileURL(picURL))
			photoCard.Caption = caption
			photoCard.ParseMode = "HTML"

			if item.UserID != "" {
				photoCard.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("👤 Продавец", fmt.Sprintf("seller:%s", item.UserID)),
					),
				)
			}

			if _, sendErr := h.bot.Send(photoCard); sendErr != nil {
				// Text fallback
				txt := tgbotapi.NewMessage(chatID, caption)
				txt.ParseMode = "HTML"
				if item.UserID != "" {
					txt.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
						tgbotapi.NewInlineKeyboardRow(
							tgbotapi.NewInlineKeyboardButtonData("👤 Продавец", fmt.Sprintf("seller:%s", item.UserID)),
						),
					)
				}
				h.bot.Send(txt)
			}
		}

		// Final message with inline keyboard controls
		pMinStr := ""
		pMaxStr := ""
		if priceMin != nil {
			pMinStr = strconv.Itoa(*priceMin)
		}
		if priceMax != nil {
			pMaxStr = strconv.Itoa(*priceMax)
		}

		keyboard := h.buildSearchKeyboard(query, page, pMinStr, pMaxStr)
		escapedQuery := html.EscapeString(query)

		ctrlMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("👇 Управление результатами по запросу <b>'%s'</b> (стр. %d):", escapedQuery, page))
		ctrlMsg.ReplyMarkup = keyboard
		ctrlMsg.ParseMode = "HTML"
		h.bot.Send(ctrlMsg)
	}()
}

func (h *BotHandler) editStatusMessage(chatID int64, messageID int, text string) {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "HTML"
	h.bot.Send(edit)
}

// --- Inline Keyboards builder & Callback queries handlers ---

func (h *BotHandler) buildSearchKeyboard(query string, page int, priceMin, priceMax string) tgbotapi.InlineKeyboardMarkup {
	row1 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("◀️ Назад", fmt.Sprintf("page:%d:%s:%s:%s", page-1, query, priceMin, priceMax)),
		tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📄 Стр. %d", page), "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Вперед ▶️", fmt.Sprintf("page:%d:%s:%s:%s", page+1, query, priceMin, priceMax)),
	}

	row2 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💰 Любая цена", fmt.Sprintf("price:%s::", query)),
		tgbotapi.NewInlineKeyboardButtonData("💰 до ¥1000", fmt.Sprintf("price:%s::1000", query)),
		tgbotapi.NewInlineKeyboardButtonData("💰 ¥1000-5000", fmt.Sprintf("price:%s:1000:5000", query)),
	}

	row3 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💰 ¥5000-15000", fmt.Sprintf("price:%s:5000:15000", query)),
		tgbotapi.NewInlineKeyboardButtonData("💰 ¥15000+", fmt.Sprintf("price:%s:15000:", query)),
	}

	row4 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔔 Добавить в мониторинг", fmt.Sprintf("addmon:%s:%s:%s", query, priceMin, priceMax)),
	}

	return tgbotapi.NewInlineKeyboardMarkup(row1, row2, row3, row4)
}

func (h *BotHandler) handleCallbackQuery(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID

	// 1. Verify authorization
	if !config.Settings.IsUserAllowed(userID) {
		h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "❌ У вас нет доступа."))
		return
	}

	data := cb.Data

	if data == "noop" {
		h.bot.Send(tgbotapi.NewCallback(cb.ID, ""))
		return
	}

	parts := strings.Split(data, ":")
	action := parts[0]

	switch action {
	case "page":
		// Syntax: page:pageNumber:query:priceMin:priceMax
		pageNum, _ := strconv.Atoi(parts[1])
		query := parts[2]
		var priceMin, priceMax *int

		if len(parts) > 3 && parts[3] != "" {
			v, _ := strconv.Atoi(parts[3])
			priceMin = &v
		}
		if len(parts) > 4 && parts[4] != "" {
			v, _ := strconv.Atoi(parts[4])
			priceMax = &v
		}

		if pageNum < 1 {
			h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "⚠️ Вы уже на первой странице."))
			return
		}

		// Delete control message
		del := tgbotapi.NewDeleteMessage(cb.Message.Chat.ID, cb.Message.MessageID)
		h.bot.Send(del)

		h.executeSearch(cb.Message.Chat.ID, query, pageNum, priceMin, priceMax)
		h.bot.Send(tgbotapi.NewCallback(cb.ID, ""))

	case "price":
		// Syntax: price:query:priceMin:priceMax
		query := parts[1]
		var priceMin, priceMax *int

		if len(parts) > 2 && parts[2] != "" {
			v, _ := strconv.Atoi(parts[2])
			priceMin = &v
		}
		if len(parts) > 3 && parts[3] != "" {
			v, _ := strconv.Atoi(parts[3])
			priceMax = &v
		}

		// Delete control message
		del := tgbotapi.NewDeleteMessage(cb.Message.Chat.ID, cb.Message.MessageID)
		h.bot.Send(del)

		h.executeSearch(cb.Message.Chat.ID, query, 1, priceMin, priceMax)

		priceMsg := "Любая"
		if priceMin != nil || priceMax != nil {
			minVal := 0
			maxVal := 0
			if priceMin != nil {
				minVal = *priceMin
			}
			if priceMax != nil {
				maxVal = *priceMax
			}
			priceMsg = fmt.Sprintf("%d-%d", minVal, maxVal)
		}
		h.bot.Send(tgbotapi.NewCallback(cb.ID, fmt.Sprintf("Применен фильтр цен: %s", priceMsg)))

	case "addmon":
		// Syntax: addmon:query:priceMin:priceMax
		query := parts[1]
		var priceMin, priceMax *int

		if len(parts) > 2 && parts[2] != "" {
			v, _ := strconv.Atoi(parts[2])
			priceMin = &v
		}
		if len(parts) > 3 && parts[3] != "" {
			v, _ := strconv.Atoi(parts[3])
			priceMax = &v
		}

		monitorID, err := h.db.AddMonitor(userID, query, priceMin, priceMax, 300)
		if err != nil {
			h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "❌ Ошибка создания мониторинга: "+err.Error()))
			return
		}

		h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, fmt.Sprintf("🔔 Задача добавлена в мониторинг! ID: %d", monitorID)))

	case "seller":
		// Syntax: seller:userID
		if len(parts) < 2 {
			h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "⚠️ Неверный ID продавца."))
			return
		}
		sellerID := parts[1]

		h.bot.Send(tgbotapi.NewCallback(cb.ID, "⏳ Загружаю профиль продавца..."))

		go func() {
			activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
			if err != nil {
				reply := tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Ошибка базы данных: "+err.Error())
				h.bot.Send(reply)
				return
			}

			client := goofish.NewSearchClient(activeCookies, config.Settings.ProxyURL, func(s string) error {
				return h.db.SaveCookies(s)
			})
			client.ForceCookieRefreshFunc = func(curr string) (string, error) {
				return h.refresher.RefreshCookies(curr)
			}

			card, err := services.GetSellerProfileCard(client, sellerID)
			if err != nil {
				reply := tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Не удалось получить профиль продавца: "+err.Error())
				h.bot.Send(reply)
				return
			}

			reply := tgbotapi.NewMessage(cb.Message.Chat.ID, card)
			reply.ParseMode = "HTML"
			reply.DisableWebPagePreview = true
			h.bot.Send(reply)
		}()
	}
}
