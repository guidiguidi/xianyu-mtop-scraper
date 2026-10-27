# 🐟 Xianyu / Goofish MTOP Reverse-Engineered Scraper & Monitor

High-performance, automated Telegram monitoring agent and REST API pipeline for **Alibaba Xianyu (Goofish)**. Features client-side MTOP MD5 signature generation, Playwright-driven self-healing session management, anti-detect browser fingerprint masking, and dynamic **ASocks** residential proxy synchronization.

---

## 🌟 Key Architecture & Capabilities

Xianyu scraping requires overcoming strict platform constraints: **rapid H5 token expiration (`_m_h5_tk`) and aggressive browser fingerprinting WAFs**.

### 1. 🔍 Real-Time Item Discovery (`/search` & REST API)
* Executes concurrent listing lookups via reverse-engineered MTOP endpoints.
* Returns structured item cards with primary image media, CNY (¥) pricing, seller handle, origin location, and user interest metrics ("want count").
* Interactive navigation via Telegram inline keyboards:
  * **◀️ Prev / Next ▶️**: Cursor and page-based result traversal.
  * **Price Filters**: Instant category filtering (e.g. `< ¥1000`, `¥1000 - ¥5000`).
  * **🔔 Track in Monitor**: Register active background monitor task directly from search results.

### 2. 🔔 Resilient Polling & Notification Engine (`/monitor`)
* Periodic background scanning worker polling saved query parameters against SQLite state cache.
* Prevents duplicate alerts by maintaining unique item ID sets and price hash indexing.
* Dispatches formatted rich cards with direct buy links to target chat channels.
* Task lifecycle management via `/monitors` (list) and `/unmonitor <ID>` (delete).

### 3. 🍪 Self-Healing Session Engine (Headless Playwright)
* Eliminates manual token refresh cycles.
* On upstream `401 / TOKEN_EXPIRED` detection, automatically spawns headless Chromium.
* Injects long-lived session credentials (`cookie2`) and simulates human navigation to clear security challenges.
* Extracts renewed `_m_h5_tk` tokens and dynamic `tfstk` device signatures, persisting them back to SQLite and resuming ingestion uninterrupted.

### 4. 📡 Residential Proxy & Geo Synchronization (ASocks API)
* Native integration with ASocks residential proxy pool.
* Automatically selects or provisions active residential ports with country-level targeting.
* **Synchronizes anti-detect profile**: aligns Playwright browser environment with upstream proxy geolocation:
  * **Locale**: `en-US` / target proxy locale
  * **Timezone**: Aligned with proxy exit node (e.g., `Europe/London`)
  * **Geolocation**: Exact latitude/longitude matching proxy IP location.
* Matches IP, timezone, system language, and geolocation coordinates to maintain 100% session legitimacy.

---

## 🛡 Anti-Detect Fingerprinting Countermeasures

Module `services/browser_refresher.py` embeds robust fingerprint masking:

1. **🎭 Webdriver Evasion**: Complete removal of `navigator.webdriver` across prototypes with native property emulation.
2. **🖥 Chrome Runtime & Plugin Shim**: Injects synthetic `window.chrome` objects, plugin arrays, and standard MIME types.
3. **🎨 Canvas & WebGL Defense**: Injects subtle mathematical noise into 2D canvas exports (`toDataURL`, `getImageData`) and WebGL renderer parameters to prevent tracking while preserving visual rendering.
4. **🎵 AudioContext Hash Spoofing**: Introduces micro-jitter into `AudioBuffer.prototype.getChannelData` to defeat acoustic waveform profiling.
5. **🔒 WebRTC Leakage Prevention**: Enforces strict routing policy to block local network IP leaks through STUN/TURN handshakes.

---

## 📐 Project Structure

```
xianyu-mtop-scraper/
│
├── bot.py                      # Main entrypoint: runs Telegram polling and background monitor worker
├── api_server.py               # REST API service exposing search and scraping endpoints
├── config.py                   # Pydantic configuration loader validating .env settings
├── database.py                 # Async SQLite manager for tasks, cache, and token persistence
├── goofish_client.py           # Core client: computes Alibaba MTOP MD5 signatures and handles requests
│
├── services/
│   ├── asocks_service.py       # ASocks residential proxy manager and geo resolver
│   ├── browser_refresher.py    # Playwright browser automation for headless cookie renewal
│   ├── parser.py               # Multi-tier price parser and item metadata normalizer
│   └── query_expander.go       # Multi-language search query expansion utilities
│
├── handlers/
│   ├── search.py               # /search command handler and pagination callbacks
│   ├── monitor.py              # /monitor task registration and management handlers
│   └── settings.py             # Session cookie injection and diagnostic handlers
│
├── Dockerfile                  # Container definition with Chromium dependencies
├── docker-compose.yml          # Production deployment compose file with persistent volumes
└── requirements.txt            # Python dependencies (aiogram, aiohttp, playwright, aiosqlite)
```

---

## 🚀 Quick Start

### 1. Environment Configuration
Create or configure `.env` in the project root:
```ini
# Telegram Bot Token from @BotFather
BOT_TOKEN=your_telegram_bot_token_here

# Authorized Admin Telegram User IDs
ALLOWED_USER_IDS=123456789

# ASocks API Key for dynamic proxy allocation
ASOCKS_API_KEY=your_asocks_api_key_here
```

### 2. Run with Docker (Recommended)
```bash
docker compose up -d --build
```
Persistent SQLite storage and logs are mapped to the host directory.

### 3. Local Execution
```bash
pip3 install -r requirements.txt
python3 -m playwright install chromium
python3 bot.py
```

### 4. Initial Session Setup
1. Open [goofish.com](https://www.goofish.com) in your browser and log in.
2. Copy your session cookie header (containing `cookie2` and `_m_h5_tk`).
3. Send `/setcookies` to your Telegram bot and provide the cookie string.
4. The bot validates the session, synchronizes proxy settings, and activates automated monitoring.
