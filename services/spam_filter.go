package services

import (
	"log"
	"strings"
	"sync"
	"time"

	"goofish-backend/goofish"
)

// SentItemRecord preserves details of an alert sent within the past 24 hours to match duplicates.
type SentItemRecord struct {
	ItemID   string
	Title    string
	Price    string
	PicURL   string
	SentTime time.Time
}

var (
	sentItemsCache sync.Map
	cacheMutex     sync.Mutex
)

// IsSpamDuplicate checks if a highly similar item has already been dispatched for this monitor in the last 24 hours.
func IsSpamDuplicate(monitorID int64, item goofish.GoofishItem) bool {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	now := time.Now()
	val, ok := sentItemsCache.Load(monitorID)

	var records []SentItemRecord
	if ok {
		records = val.([]SentItemRecord)
	}

	// 1. Clean up records older than 24 hours to prevent memory leaks
	var freshRecords []SentItemRecord
	isDuplicate := false

	for _, rec := range records {
		if now.Sub(rec.SentTime) < 24*time.Hour {
			freshRecords = append(freshRecords, rec)

			// 2. Check for duplicate criteria
			// A: Exact same image URL (unique image identifier)
			if rec.PicURL != "" && item.PicURL != "" && rec.PicURL == item.PicURL {
				isDuplicate = true
			}
			// B: Same Title and Price
			if strings.TrimSpace(rec.Title) == strings.TrimSpace(item.Title) && rec.Price == item.Price {
				isDuplicate = true
			}
		}
	}

	if isDuplicate {
		log.Printf("[SpamFilter] Filtered duplicate item ID %s ('%s', %s) for Monitor %d", item.ItemID, item.Title, item.Price, monitorID)
		return true
	}

	// Add the current item to records
	freshRecords = append(freshRecords, SentItemRecord{
		ItemID:   item.ItemID,
		Title:    item.Title,
		Price:    item.Price,
		PicURL:   item.PicURL,
		SentTime: now,
	})

	sentItemsCache.Store(monitorID, freshRecords)
	return false
}
