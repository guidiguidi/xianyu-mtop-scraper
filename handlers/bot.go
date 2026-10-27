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
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Access denied.")
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
			reply := tgbotapi.NewMessage(msg.Chat.ID, "❓ Unknown command. Use /help for available commands.")
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
	reply := tgbotapi.NewMessage(msg.Chat.ID, "💡 Use /search <query> to search or /help for instructions.")
	h.bot.Send(reply)
}

// --- Commands Handlers ---

func (h *BotHandler) cmdStart(msg *tgbotapi.Message) {
	welcome := "🐟 <b>Welcome to Xianyu / Goofish Search & Monitor Bot!</b>\n\n" +
		"Automated assistant for searching Goofish / Xianyu marketplace and receiving real-time alerts.\n\n" +
		"📖 <b>Core Commands:</b>\n" +
		"• `/search <query>` - Interactive product search\n" +
		"• `/monitor <query> [<min_price> <max_price>]` - Track new listings\n" +
		"• `/monitors` - List active monitor tasks\n" +
		"• `/unmonitor <ID>` - Remove monitor task\n" +
		"• `/setcookies` - Update session cookies\n" +
		"• `/status` - Check proxy and session token status\n\n" +
		"ℹ️ Use /help for instructions"

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

func (h *BotHandler) cmdHelp(msg *tgbotapi.Message) {
	help := "📖 <b>Help & Usage Guide</b>\n\n" +
		"🔍 <b>How to Search?</b>\n" +
		"Use `/search <query>`. The bot returns structured item cards with images and action buttons.\n\n" +
		"🔔 <b>How Does Monitoring Work?</b>\n" +
		"Subscribe to keywords. Background worker polls Goofish and notifies on new items.\n\n" +
		"🍪 <b>Session Authentication:</b>\n" +
		"Goofish H5 API requires MTOP signatures. Use /setcookies to renew session tokens.\n\n"

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

		reply := tgbotapi.NewMessage(msg.Chat.ID, "🔍 Enter search query (e.g. <code>/search rick owens</code>)")
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
	h.states.Store(msg.From.ID, "waiting_for_query")
}

func (h *BotHandler) processSearchQuery(msg *tgbotapi.Message) {
	h.states.Delete(msg.From.ID)
	query := strings.TrimSpace(msg.Text)
	if query == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Empty query. Please provide search keywords.")
		h.bot.Send(reply)
		return
	}
	h.executeSearch(msg.Chat.ID, query, 1, nil, nil)
}

func (h *BotHandler) cmdAddMonitor(msg *tgbotapi.Message) {
	args := strings.TrimSpace(msg.CommandArguments())
	if args == "" {
		help := "⚠️ Invalid syntax.\n\n" +
			"Examples:\n" +
			"• `/monitor rickowens` - track all new listings\n" +
			"• `/monitor rickowens 5000 15000` - track in price range ¥5000-¥15000"
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
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Error creating monitor task: "+err.Error())
		h.bot.Send(reply)
		return
	}

	priceFilter := "None"
	if priceMin != nil || priceMax != nil {
		priceFilter = fmt.Sprintf("¥%d-¥%d", *priceMin, *priceMax)
	}

	response := fmt.Sprintf(
		"✅ <b>Monitoring task started!</b>\n\n"+
			"🆔 Task ID: <code>%d</code>\n"+
			"🔍 Keyword: <b>%s</b>\n"+
			"💰 Price Filter: %s\n"+
			"⏱ Polling Interval: <code>5 minutes</code>",
		monitorID, keyword, priceFilter,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, response)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

func (h *BotHandler) cmdListMonitors(msg *tgbotapi.Message) {
	monitors, err := h.db.GetUserMonitors(msg.From.ID)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to read monitor tasks: "+err.Error())
		h.bot.Send(reply)
		return
	}

	if len(monitors) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "🔍 No active monitor tasks. Create one using /monitor.")
		h.bot.Send(reply)
		return
	}

	var sb strings.Builder
	sb.WriteString("📋 <b>Active Monitor Tasks:</b>\n\n")

	for _, mon := range monitors {
		priceStr := "Any"
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
				"   💰 Price: <code>%s</code> | ⏱ Interval: <code>5 min</code> | Status: <code>Active</code>\n"+
				"   ❌ Remove: `/unmonitor %d`\n\n",
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
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Specify task ID. Example: `/unmonitor 1`")
		h.bot.Send(reply)
		return
	}

	monitorID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Task ID must be an integer.")
		h.bot.Send(reply)
		return
	}

	success, err := h.db.DeleteMonitor(monitorID, msg.From.ID)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Error removing monitor: "+err.Error())
		h.bot.Send(reply)
		return
	}

	if success {
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Monitor task <b>ID %d</b> successfully removed.", monitorID))
		reply.ParseMode = "HTML"
		h.bot.Send(reply)
	} else {
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Task with <b>ID %d</b> not found.", monitorID))
		reply.ParseMode = "HTML"
		h.bot.Send(reply)
	}
}

func (h *BotHandler) cmdSetCookiesPrompt(msg *tgbotapi.Message) {
	welcome := "🍪 <b>Update Goofish Session Cookies</b>\n\n" +
		"Send raw session Cookie header (from anti-detect browser or DevTools).\n" +
		"Must include <code>cookie2</code> and <code>_m_h5_tk</code>.\n\n" +
		"Send /cancel to abort."

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcome)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
	h.states.Store(msg.From.ID, "waiting_for_cookies")
}

func (h *BotHandler) cmdCancel(msg *tgbotapi.Message) {
	h.states.Delete(msg.From.ID)
	reply := tgbotapi.NewMessage(msg.Chat.ID, "🔄 Action cancelled.")
	h.bot.Send(reply)
}

func (h *BotHandler) processCookiesInput(msg *tgbotapi.Message) {
	h.states.Delete(msg.From.ID)
	cookieStr := strings.TrimSpace(msg.Text)

	if cookieStr == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Empty input. Please provide cookies string.")
		h.bot.Send(reply)
		return
	}

	if !strings.Contains(cookieStr, "_m_h5_tk") {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Missing required parameters `_m_h5_tk` or `cookie2`.")
		reply.ParseMode = "HTML"
		h.bot.Send(reply)
		return
	}

	// Save to DB
	if err := h.db.SaveCookies(cookieStr); err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Error saving cookies: "+err.Error())
		h.bot.Send(reply)
		return
	}

	statusMsg := tgbotapi.NewMessage(msg.Chat.ID, "💾 Cookies saved to SQLite. Validating connection...")
	statusSent, _ := h.bot.Send(statusMsg)

	// Validate cookies by running a test search
	go func() {
		client := goofish.NewSearchClient(cookieStr, config.Settings.ProxyURL, func(s string) error {
			return h.db.SaveCookies(s)
		})

		testWord := "test"
		rawRes, err := client.Search(testWord, 1, nil, nil, 1, false)
		if err != nil {
			edit := tgbotapi.NewEditMessageText(msg.Chat.ID, statusSent.MessageID, "⚠️ <b>Cookies saved, but verification failed:</b> <code>"+html.EscapeString(err.Error())+"</code>.\nEnsure proxy connection is operational.")
			edit.ParseMode = "HTML"
			h.bot.Send(edit)
			return
		}

		retMsg := "Unknown"
		if retList, ok := rawRes["ret"].([]interface{}); ok && len(retList) > 0 {
			if s, ok := retList[0].(string); ok {
				retMsg = s
			}
		}

		if strings.Contains(retMsg, "SUCCESS") {
			edit := tgbotapi.NewEditMessageText(msg.Chat.ID, statusSent.MessageID, "✅ <b>Cookies verified successfully!</b> API session is active and functional.")
			edit.ParseMode = "HTML"
			h.bot.Send(edit)
		} else {
			edit := tgbotapi.NewEditMessageText(msg.Chat.ID, statusSent.MessageID, "⚠️ <b>Cookies saved, but API test returned error:</b> <code>"+html.EscapeString(retMsg)+"</code>. Cookies may have expired or verification is required.")
			edit.ParseMode = "HTML"
			h.bot.Send(edit)
		}
	}()
}

func (h *BotHandler) cmdStatus(msg *tgbotapi.Message) {
	cookies, _ := h.db.GetCookies(config.Settings.XianyuCookies)
	client := goofish.NewSearchClient(cookies, config.Settings.ProxyURL, nil)

	hasToken := "❌ No"
	tokenVal := "None"
	if tok := client.GetToken(); tok != "" {
		hasToken = "✅ Yes"
		tokenVal = tok
	}

	proxyStatus := "❌ Disabled"
	if config.Settings.ProxyURL != "" {
		proxyStatus = fmt.Sprintf("<code>%s</code>", html.EscapeString(config.Settings.ProxyURL))
	}

	response := fmt.Sprintf(
		"🖥 <b>Xianyu Bot System Status</b>\n\n"+
			"📡 Proxy: %s\n"+
			"🍪 H5 Token Status: %s\n"+
			"🔑 Token: <code>%s</code>\n\n"+
			"To update cookies, send: /setcookies",
		proxyStatus, hasToken, tokenVal,
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, response)
	reply.ParseMode = "HTML"
	h.bot.Send(reply)
}

// --- Search Action Engine ---

func (h *BotHandler) executeSearch(chatID int64, query string, page int, priceMin, priceMax *int) {
	statusMsg := tgbotapi.NewMessage(chatID, "⏳ Searching Goofish...")
	statusSent, _ := h.bot.Send(statusMsg)

	go func() {
		activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
		if err != nil {
			h.editStatusMessage(chatID, statusSent.MessageID, "❌ Database error: "+err.Error())
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
			h.editStatusMessage(chatID, statusSent.MessageID, "❌ Search error: "+html.EscapeString(err.Error()))
			return
		}

		items := goofish.ParseSearchResponse(rawResp)
		if len(items) == 0 {
		h.editStatusMessage(chatID, statusSent.MessageID, "🔍 No listings found. Verify query or refresh cookies via /setcookies.")
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
			escapedArea := "Unspecified"
			if trans.areaTrans != "" {
				escapedArea = html.EscapeString(trans.areaTrans)
			}
			escapedNickname := "Unspecified"
			if item.UserNickname != "" {
				escapedNickname = html.EscapeString(item.UserNickname)
			}
			escapedWant := html.EscapeString(item.WantCount)

			// Collect badges
			var badges []string
			if item.IsVerified {
				badges = append(badges, "✅ Verified Seller")
			}
			if item.IsNewItem {
				badges = append(badges, "🆕 Brand New")
			}
			if fLabel := item.FreshnessLabel(); fLabel != "" {
				badges = append(badges, fLabel)
			}
			badgeLine := strings.Join(badges, "  ")

			caption := fmt.Sprintf(
				"🐟 <b>[%d/%d] %s</b>\n\n"+
					"💰 Price: <code>%s</code>\n"+
					"📍 Region: %s\n"+
					"👤 Seller: %s\n",
				idx+1, len(items), escapedTitle, escapedPrice, escapedArea, escapedNickname,
			)

			if escapedWant != "" {
				caption += fmt.Sprintf("📦 %s\n", escapedWant)
			}
			if badgeLine != "" {
				caption += fmt.Sprintf("%s\n", badgeLine)
			}
			caption += fmt.Sprintf("\n<a href=\"%s\">🔗 Open on Xianyu</a>", item.ShareURL())

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
						tgbotapi.NewInlineKeyboardButtonData("👤 Seller", fmt.Sprintf("seller:%s", item.UserID)),
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
							tgbotapi.NewInlineKeyboardButtonData("👤 Seller", fmt.Sprintf("seller:%s", item.UserID)),
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

		ctrlMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("👇 Manage results for query <b>'%s'</b> (page %d):", escapedQuery, page))
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
		tgbotapi.NewInlineKeyboardButtonData("◀️ Prev", fmt.Sprintf("page:%d:%s:%s:%s", page-1, query, priceMin, priceMax)),
		tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("📄 Page %d", page), "noop"),
		tgbotapi.NewInlineKeyboardButtonData("Next ▶️", fmt.Sprintf("page:%d:%s:%s:%s", page+1, query, priceMin, priceMax)),
	}

	row2 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💰 Any Price", fmt.Sprintf("price:%s::", query)),
		tgbotapi.NewInlineKeyboardButtonData("💰 < ¥1000", fmt.Sprintf("price:%s::1000", query)),
		tgbotapi.NewInlineKeyboardButtonData("💰 ¥1000-5000", fmt.Sprintf("price:%s:1000:5000", query)),
	}

	row3 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("💰 ¥5000-15000", fmt.Sprintf("price:%s:5000:15000", query)),
		tgbotapi.NewInlineKeyboardButtonData("💰 ¥15000+", fmt.Sprintf("price:%s:15000:", query)),
	}

	row4 := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonData("🔔 Track in Monitor", fmt.Sprintf("addmon:%s:%s:%s", query, priceMin, priceMax)),
	}

	return tgbotapi.NewInlineKeyboardMarkup(row1, row2, row3, row4)
}

func (h *BotHandler) handleCallbackQuery(cb *tgbotapi.CallbackQuery) {
	userID := cb.From.ID

	// 1. Verify authorization
	if !config.Settings.IsUserAllowed(userID) {
		h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "❌ Access denied."))
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
			h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "⚠️ Already on first page."))
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

		priceMsg := "Any"
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
		h.bot.Send(tgbotapi.NewCallback(cb.ID, fmt.Sprintf("Applied price filter: %s", priceMsg)))

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
			h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "❌ Error creating monitor: "+err.Error()))
			return
		}

		h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, fmt.Sprintf("🔔 Task added to monitoring! ID: %d", monitorID)))

	case "seller":
		// Syntax: seller:userID
		if len(parts) < 2 {
			h.bot.Send(tgbotapi.NewCallbackWithAlert(cb.ID, "⚠️ Invalid seller ID."))
			return
		}
		sellerID := parts[1]

		h.bot.Send(tgbotapi.NewCallback(cb.ID, "⏳ Loading seller profile..."))

		go func() {
			activeCookies, err := h.db.GetCookies(config.Settings.XianyuCookies)
			if err != nil {
				reply := tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Database error: "+err.Error())
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
				reply := tgbotapi.NewMessage(cb.Message.Chat.ID, "❌ Failed to fetch seller profile: "+err.Error())
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
