package services

import (
	"context"
	"fmt"
	"html"
	"log"
	"time"

	"goofish-backend/config"
	"goofish-backend/database"
	"goofish-backend/goofish"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// MonitorService coordinates periodic background scraping task alerts.
type MonitorService struct {
	bot       *tgbotapi.BotAPI
	db        *database.Database
	refresher *CookieRefresherService
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewMonitorService creates a new MonitorService.
func NewMonitorService(bot *tgbotapi.BotAPI, db *database.Database, refresher *CookieRefresherService) *MonitorService {
	return &MonitorService{
		bot:       bot,
		db:        db,
		refresher: refresher,
	}
}

// Start spawns the perpetual monitor ticking routine.
func (s *MonitorService) Start() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	log.Println("[MonitorService] Starting Goofish background monitor ticker...")

	go func() {
		for {
			select {
			case <-s.ctx.Done():
				log.Println("[MonitorService] Monitor loop context cancelled. Exiting.")
				return
			default:
				s.runCycle()
				// Sleep 60 seconds before executing next check cycle
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(60 * time.Second):
				}
			}
		}
	}()
}

// Stop stops the monitor goroutine safely.
func (s *MonitorService) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	log.Println("[MonitorService] Background monitor service stopped.")
}

func (s *MonitorService) runCycle() {
	log.Println("[MonitorService] Running background scraping verification cycle...")

	// 1. Fetch active monitors
	monitors, err := s.db.GetActiveMonitors()
	if err != nil {
		log.Printf("[MonitorService] Error fetching active monitors: %v", err)
		return
	}

	if len(monitors) == 0 {
		log.Println("[MonitorService] No active monitors found. Idling for next tick...")
		return
	}

	log.Printf("[MonitorService] Checking %d active search tasks...", len(monitors))

	// 2. Fetch current cookie string from SQLite
	cookiesStr, err := s.db.GetCookies(config.Settings.XianyuCookies)
	if err != nil {
		log.Printf("[MonitorService] Error querying cookies from DB: %v", err)
		return
	}

	// 3. Process each task sequentially to avoid overloading IP / proxy endpoints
	for _, mon := range monitors {
		select {
		case <-s.ctx.Done():
			return
		default:
			s.checkMonitorTask(mon, cookiesStr)
			// Cool down 5 seconds between monitor checks to bypass target anti-bot triggers
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (s *MonitorService) checkMonitorTask(mon database.Monitor, cookiesStr string) {
	log.Printf("[MonitorService] Checking Task ID %d for keyword: '%s'", mon.ID, mon.Keyword)

	// Callback to save cookie changes directly to sqlite
	onCookiesUpdated := func(newCookies string) error {
		return s.db.SaveCookies(newCookies)
	}

	// Setup client
	client := goofish.NewSearchClient(cookiesStr, config.Settings.ProxyURL, onCookiesUpdated)
	client.ForceCookieRefreshFunc = func(curr string) (string, error) {
		return s.refresher.RefreshCookies(curr)
	}

	// Search items (pulling top 15 matches to inspect)
	expandedKeyword := ExpandQuery(mon.Keyword)
	rawResp, err := client.Search(expandedKeyword, 1, mon.PriceMin, mon.PriceMax, 15, true)
	if err != nil {
		log.Printf("[MonitorService] Search API failed for Monitor ID %d: %v", mon.ID, err)
		return
	}

	items := goofish.ParseSearchResponse(rawResp)
	if len(items) == 0 {
		log.Printf("[MonitorService] No results returned for Task ID %d.", mon.ID)
		return
	}

	newAlertsCount := 0

	// Loop items in REVERSE order (oldest to newest) to post alerts chronologically
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		seen, err := s.db.IsItemSeen(item.ItemID, mon.ID)
		if err != nil {
			log.Printf("[MonitorService] Error checking seen status: %v", err)
			continue
		}

		if !seen {
			// Apply anti-spam / duplicate filter
			if IsSpamDuplicate(mon.ID, item) {
				// Mark as processed in database so we don't inspect it again next cycle,
				// but do NOT trigger alert/notification.
				_ = s.db.AddSeenItem(item.ItemID, mon.ID)
				continue
			}

			// Save in sqlite to mark as processed
			if err := s.db.AddSeenItem(item.ItemID, mon.ID); err != nil {
				log.Printf("[MonitorService] Error saving seen item: %v", err)
				continue
			}

			newAlertsCount++

			// Trigger notification dispatch
			s.sendNotification(mon.UserID, mon.Keyword, item)

			// Small 1-second delay between Telegram API sends to prevent bot rate-limiting
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}

	if newAlertsCount > 0 {
		log.Printf("[MonitorService] Task ID %d ('%s'): Dispatched %d new item alert notifications.", mon.ID, mon.Keyword, newAlertsCount)
	}
}

func (s *MonitorService) sendNotification(userID int64, keyword string, item goofish.GoofishItem) {
	// Translate details to Russian concurrently via our DeepL client
	tTitle := GlobalTranslator.Translate(item.Title, "ZH", "RU")
	tArea := ""
	if item.Area != "" {
		tArea = GlobalTranslator.Translate(item.Area, "ZH", "RU")
	}

	// Escape inputs to ensure HTML parser compliance
	escapedKeyword := html.EscapeString(keyword)
	escapedTitle := html.EscapeString(tTitle)
	if len(escapedTitle) > 75 {
		escapedTitle = escapedTitle[:75] + "..."
	}
	escapedPrice := html.EscapeString(item.Price)

	escapedArea := "Unspecified"
	if tArea != "" {
		escapedArea = html.EscapeString(tArea)
	}

	escapedNickname := "Unspecified"
	if item.UserNickname != "" {
		escapedNickname = html.EscapeString(item.UserNickname)
	}

	escapedWant := "New listing"
	if item.WantCount != "" {
		escapedWant = html.EscapeString(item.WantCount)
	}

	caption := fmt.Sprintf(
		"🔔 <b>New Listing Alert! (Monitor: %s)</b>\n\n"+
			"🐟 <b>%s</b>\n\n"+
			"💰 Price: <code>%s</code>\n"+
			"📍 Region: %s\n"+
			"👤 Seller: %s\n"+
			"📦 %s\n\n"+
			"<a href=\"%s\">🔗 View on Xianyu</a>",
		escapedKeyword,
		escapedTitle,
		escapedPrice,
		escapedArea,
		escapedNickname,
		escapedWant,
		item.ShareURL(),
	)

	picURL := item.PicURL
	if picURL == "" {
		picURL = "https://img.alicdn.com/tps/i4/TB1OKd7JXXXXXb1XFXXc8s9FXXX-400-400.png"
	}

	// Send rich photo message
	photoMsg := tgbotapi.NewPhoto(userID, tgbotapi.FileURL(picURL))
	photoMsg.Caption = caption
	photoMsg.ParseMode = "HTML"

	if item.UserID != "" {
		photoMsg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("👤 Seller", fmt.Sprintf("seller:%s", item.UserID)),
			),
		)
	}

	_, err := s.bot.Send(photoMsg)
	if err != nil {
		log.Printf("[MonitorService] Warning: Failed to send photo message to User %d: %v. Retrying with plain text fallback...", userID, err)

		// Fallback to text message
		txtMsg := tgbotapi.NewMessage(userID, caption)
		txtMsg.ParseMode = "HTML"
		txtMsg.DisableWebPagePreview = false

		if _, fallbackErr := s.bot.Send(txtMsg); fallbackErr != nil {
			log.Printf("[MonitorService] Error: Telegram plain-text fallback also failed for User %d: %v", userID, fallbackErr)
		}
	}
}
