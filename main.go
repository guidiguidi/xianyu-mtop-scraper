package main

import (
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"goofish-backend/config"
	"goofish-backend/database"
	"goofish-backend/handlers"
	"goofish-backend/services"

	"github.com/gin-gonic/gin"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	// 1. Setup multi-output logging (Stdout + xianyu_bot.log file)
	logFile, err := os.OpenFile("xianyu_bot.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Warning: Failed to create log file: %v. Logging to stdout only.", err)
		log.SetOutput(os.Stdout)
	} else {
		defer logFile.Close()
		multiWriter := io.MultiWriter(os.Stdout, logFile)
		log.SetOutput(multiWriter)
	}

	log.Println("[Main] Initializing Xianyu/Goofish Telegram Bot and API Service (Go Version)...")

	// 2. Validate configuration
	if config.Settings.BotToken == "" || strings.Contains(config.Settings.BotToken, "example_token") || len(config.Settings.BotToken) < 15 {
		log.Println("[Main] Fatal Error: Invalid Telegram BOT_TOKEN. Configure it in your .env file!")
		os.Exit(1)
	}

	// 3. Initialize translation service
	services.InitializeTranslator(config.Settings.DeeplAPIKey)

	// 4. Initialize database
	db := database.NewDatabase(config.Settings.DatabasePath)
	if err := db.Initialize(); err != nil {
		log.Fatalf("[Main] Fatal Error: SQLite database failed to initialize: %v", err)
	}
	defer func() {
		log.Println("[Main] Closing database connection...")
		db.Close()
	}()

	// 5. Initialize cookies refresher
	refresher := services.NewCookieRefresherService(config.Settings.ProxyURL, config.Settings.AsocksAPIKey)

	// 6. Initialize Telegram Bot client
	bot, err := tgbotapi.NewBotAPI(config.Settings.BotToken)
	if err != nil {
		log.Fatalf("[Main] Fatal Error: Failed to authorize Telegram Bot: %v", err)
	}
	log.Printf("[Main] Telegram Bot authorized successfully on account: @%s", bot.Self.UserName)

	// 7. Start Background monitoring cycle
	monitor := services.NewMonitorService(bot, db, refresher)
	monitor.Start()
	defer func() {
		log.Println("[Main] Terminating background monitor goroutine...")
		monitor.Stop()
	}()

	// 8. Start REST API Server (Gin)
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Simple logger middleware to pipe Gin outputs to our global multi-writer
	r.Use(func(c *gin.Context) {
		t := time.Now()
		c.Next()
		log.Printf("[API] HTTP %d | %s | %s | %s", c.Writer.Status(), c.Request.Method, c.Request.URL.Path, time.Since(t))
	})

	apiHandler := handlers.NewAPIHandler(db, refresher)
	apiHandler.BindRoutes(r)

	go func() {
		log.Println("[API] Gin server listening on http://0.0.0.0:8080")
		if err := r.Run("0.0.0.0:8080"); err != nil {
			log.Printf("[API] Error: HTTP server exited: %v", err)
		}
	}()

	// 9. Handle Telegram updates via long polling
	botHandler := handlers.NewBotHandler(bot, db, refresher)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Listen for system interrupt or terminate signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Println("[Main] Bot is now starting long polling updates. Welcome!")
		for update := range updates {
			botHandler.HandleUpdate(update)
		}
	}()

	// Await signal trigger
	sig := <-sigChan
	log.Printf("[Main] Received system signal: %v. Initiating graceful shutdown sequence...", sig)
	log.Println("[Main] Shutdown complete. Bye!")
}
