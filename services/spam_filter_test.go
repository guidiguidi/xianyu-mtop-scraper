package services

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"goofish-backend/goofish"
)

func TestIsSpamDuplicate(t *testing.T) {
	// Reset the cache for isolated testing
	sentItemsCache = sync.Map{}

	monitorID := int64(1001)

	item1 := goofish.GoofishItem{
		ItemID: "item_abc_1",
		Title:  "Beautiful Antique Vase",
		Price:  "¥500",
		PicURL: "http://example.com/vase.jpg",
	}

	// 1. First search: should NOT be a duplicate
	if IsSpamDuplicate(monitorID, item1) {
		t.Errorf("Expected first item to not be detected as spam duplicate")
	}

	// 2. Second search with same item: should BE detected as duplicate
	if !IsSpamDuplicate(monitorID, item1) {
		t.Errorf("Expected identical item to be detected as spam duplicate")
	}

	// 3. Item with different ID but same Title and Price: should BE a duplicate
	item2 := goofish.GoofishItem{
		ItemID: "item_abc_2",
		Title:  "Beautiful Antique Vase",
		Price:  "¥500",
		PicURL: "http://example.com/other_vase.jpg",
	}
	if !IsSpamDuplicate(monitorID, item2) {
		t.Errorf("Expected item with same title and price to be detected as spam duplicate")
	}

	// 4. Item with different ID and Title, but same PicURL: should BE a duplicate
	item3 := goofish.GoofishItem{
		ItemID: "item_abc_3",
		Title:  "Some completely different title",
		Price:  "¥600",
		PicURL: "http://example.com/vase.jpg",
	}
	if !IsSpamDuplicate(monitorID, item3) {
		t.Errorf("Expected item with same picture URL to be detected as spam duplicate")
	}

	// 5. Item with different Title, Price, and PicURL: should NOT be a duplicate
	item4 := goofish.GoofishItem{
		ItemID: "item_abc_4",
		Title:  "Completely different thing",
		Price:  "¥999",
		PicURL: "http://example.com/different.jpg",
	}
	if IsSpamDuplicate(monitorID, item4) {
		t.Errorf("Expected different item to not be detected as spam duplicate")
	}
}

func TestIsSpamDuplicate_Expiration(t *testing.T) {
	sentItemsCache = sync.Map{}
	monitorID := int64(2002)

	item := goofish.GoofishItem{
		ItemID: "expired_item",
		Title:  "Retro Console",
		Price:  "¥300",
		PicURL: "http://example.com/retro.jpg",
	}

	// Add item as already sent, but simulate it was sent 25 hours ago
	pastRecord := SentItemRecord{
		ItemID:   item.ItemID,
		Title:    item.Title,
		Price:    item.Price,
		PicURL:   item.PicURL,
		SentTime: time.Now().Add(-25 * time.Hour),
	}
	sentItemsCache.Store(monitorID, []SentItemRecord{pastRecord})

	// Since it's older than 24 hours, checking it again should NOT mark it as a duplicate, and it should purge the old record
	if IsSpamDuplicate(monitorID, item) {
		t.Errorf("Expected item from 25 hours ago to not be considered a duplicate")
	}

	// The old record should be cleaned up, and a new one added. Check that a subsequent check now detects duplicate
	if !IsSpamDuplicate(monitorID, item) {
		t.Errorf("Expected immediate subsequent check to detect duplicate")
	}
}

func TestIsSpamDuplicate_Concurrency(t *testing.T) {
	sentItemsCache = sync.Map{}
	monitorID := int64(3003)

	var wg sync.WaitGroup
	workers := 10
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			item := goofish.GoofishItem{
				ItemID: "concurrent_item_" + strconv.Itoa(id),
				Title:  "Vase " + strconv.Itoa(id),
				Price:  "¥500",
				PicURL: "http://example.com/vase_" + strconv.Itoa(id) + ".jpg",
			}
			// Concurrently call IsSpamDuplicate to ensure thread safety
			IsSpamDuplicate(monitorID, item)
		}(i)
	}

	wg.Wait()
}
