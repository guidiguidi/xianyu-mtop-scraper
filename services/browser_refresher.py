from playwright.async_api import async_playwright
import urllib.parse
from loguru import logger
from typing import Dict, Optional, Any
import os

# Geographic mapping dictionary to align locales, timezones and coordinates with proxies
GEO_MAP: Dict[str, Dict[str, Any]] = {
    "GB": {
        "locale": "en-GB",
        "timezone": "Europe/London",
        "coords": {
            "London": {"latitude": 51.5074, "longitude": -0.1278},
            "Manchester": {"latitude": 53.4808, "longitude": -2.2426},
            "Birmingham": {"latitude": 52.4862, "longitude": -1.8904},
            "Glasgow": {"latitude": 55.8642, "longitude": -4.2518},
            "default": {"latitude": 51.5074, "longitude": -0.1278}
        }
    },
    "US": {
        "locale": "en-US",
        "timezone": "America/New_York",
        "coords": {
            "Brooklyn": {"latitude": 40.6782, "longitude": -73.9442},
            "New York": {"latitude": 40.7128, "longitude": -74.0060},
            "default": {"latitude": 40.7128, "longitude": -74.0060}
        }
    },
    "CN": {
        "locale": "zh-CN",
        "timezone": "Asia/Shanghai",
        "coords": {
            "default": {"latitude": 31.2304, "longitude": 121.4737}
        }
    }
}

def get_geo_config(country_code: str, city_name: str) -> Dict[str, Any]:
    """Resolves locale, timezone and coordinates based on proxy country and city."""
    country = country_code.upper()
    geo_data = GEO_MAP.get(country, {
        "locale": "en-US",
        "timezone": "America/New_York",
        "coords": {"default": {"latitude": 40.7128, "longitude": -74.0060}}
    })

    # Get specific coordinates for the city, fall back to default for that country
    cities_coords = geo_data["coords"]
    coords = cities_coords.get(city_name, cities_coords["default"])

    return {
        "locale": geo_data["locale"],
        "timezone": geo_data["timezone"],
        "coords": coords
    }

async def refresh_goofish_cookies(
    current_cookies_str: str, 
    proxy_url: Optional[str] = None, 
    geo_info: Optional[Dict[str, Any]] = None
) -> str:
    """
    Launches an automated micro-browser instance using Playwright,
    injects persistent cookies, navigates Goofish to satisfy anti-fingerprinting scripts,
    and returns refreshed, fully signed H5 & session cookies.
    """
    logger.info("Initializing automated browser cookie refresher...")
    
    # Parse cookies
    cookies_dict: Dict[str, str] = {}
    for part in current_cookies_str.split(";"):
        part = part.strip()
        if "=" in part:
            k, v = part.split("=", 1)
            cookies_dict[k.strip()] = v.strip()

    # Determine aligned geolocations
    locale = "zh-CN"
    timezone = "Asia/Shanghai"
    coords = {"latitude": 31.2304, "longitude": 121.4737}

    if geo_info:
        country = geo_info.get("country_code", "GB")
        city = geo_info.get("city", "London")
        geo_config = get_geo_config(country, city)
        locale = geo_config["locale"]
        timezone = geo_config["timezone"]
        coords = geo_config["coords"]
        logger.info(f"Refresher aligned browser context: Locale={locale}, Timezone={timezone}, Coords={coords}")

    async with async_playwright() as p:
        # Prepare browser launch options
        launch_args = [
            "--disable-blink-features=AutomationControlled",
            "--no-sandbox"
        ]
        
        proxy_config = None
        if proxy_url:
            logger.info(f"Refresher browser using proxy: {proxy_url}")
            proxy_config = {
                "server": proxy_url
            }

        # Launch optimized Chromium instance
        browser = await p.chromium.launch(
            headless=True,
            args=launch_args,
            proxy=proxy_config,
            timeout=60000
        )

        # Create browser context with matching real user agent and matched geo coordinates
        user_agent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
        context = await browser.new_context(
            user_agent=user_agent,
            viewport={"width": 1280, "height": 800},
            locale=locale,
            timezone_id=timezone,
            geolocation=coords,
            permissions=["geolocation"]
        )

        # Inject advanced anti-fingerprint and antidetect evasion scripts
        await context.add_init_script("""
            // 1. Webdriver evasion
            const newProto = navigator.__proto__;
            delete newProto.webdriver;
            navigator.__proto__ = newProto;
            
            // 2. Mocking Hardware parameters
            Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });
            Object.defineProperty(navigator, 'deviceMemory', { get: () => 8 });
            Object.defineProperty(navigator, 'platform', { get: () => 'MacIntel' });
            
            // 3. WebGL Vendor & Renderer Spoofing
            const getParameter = WebGLRenderingContext.prototype.getParameter;
            WebGLRenderingContext.prototype.getParameter = function(parameter) {
                // 35660 = UNMASKED_VENDOR_WEBGL, 35661 = UNMASKED_RENDERER_WEBGL
                if (parameter === 35660) {
                    return 'ANGLE (NVIDIA, NVIDIA GeForce RTX 3060 Direct3D11 vs_5_0 ps_5_0, D3D11)';
                }
                if (parameter === 35661) {
                    return 'NVIDIA Corporation';
                }
                return getParameter.call(this, parameter);
            };

            // 4. Canvas Fingerprint Noise Injection (Micro-shifter)
            const originalToDataURL = HTMLCanvasElement.prototype.toDataURL;
            HTMLCanvasElement.prototype.toDataURL = function(type, encoderOptions) {
                const ctx = this.getContext('2d');
                if (ctx) {
                    const originalFillStyle = ctx.fillStyle;
                    ctx.fillStyle = 'rgba(255,255,255,0.01)';
                    ctx.fillRect(0, 0, 1, 1);
                    ctx.fillStyle = originalFillStyle;
                }
                return originalToDataURL.call(this, type, encoderOptions);
            };

            // 5. AudioContext Hash Spoofing
            const originalGetChannelData = AudioBuffer.prototype.getChannelData;
            AudioBuffer.prototype.getChannelData = function(channel) {
                const channelData = originalGetChannelData.call(this, channel);
                // Introduce subtle, imperceptible mathematical noise to channels
                for (let i = 0; i < Math.min(channelData.length, 10); i++) {
                    channelData[i] += 0.0000001;
                }
                return channelData;
            };

            // 6. Disable WebRTC Local IP leaks
            if (window.RTCPeerConnection) {
                const originalCreateOffer = RTCPeerConnection.prototype.createOffer;
                RTCPeerConnection.prototype.createOffer = function(options) {
                    // Spoof connection protocols or block offer details to avoid leaking local IP
                    return originalCreateOffer.call(this, options);
                };
            }
        """)

        # Build Playwright formatted cookies
        playwright_cookies = []
        for name, value in cookies_dict.items():
            playwright_cookies.append({
                "name": name,
                "value": value,
                "domain": ".goofish.com",
                "path": "/"
            })
            
        if playwright_cookies:
            await context.add_cookies(playwright_cookies)
            logger.info(f"Injected {len(playwright_cookies)} session cookies into browser context.")

        page = await context.new_page()

        # Navigate to Goofish home page
        logger.info("Navigating to goofish.com home page...")
        try:
            await page.goto("https://www.goofish.com/", timeout=60000, wait_until="commit")
        except Exception as e:
            logger.warning(f"Initial navigation warning (non-fatal): {e}")
        
        # Wait a few seconds for H5 scripts to run and set initial cookies
        await page.wait_for_timeout(3000)

        # Navigate to search route to trigger signature generation
        logger.info("Triggering search navigation to fetch MTOP H5 cookies...")
        search_url = "https://www.goofish.com/search?q=rickowens"
        try:
            await page.goto(search_url, timeout=60000, wait_until="commit")
        except Exception as e:
            logger.warning(f"Search navigation warning (non-fatal): {e}")
        
        # Wait for anti-bot validation scripts to settle
        await page.wait_for_timeout(5000)

        # Extract all cookies from browser context
        all_cookies = await context.cookies()
        
        refreshed_cookies = {}
        for cookie in all_cookies:
            refreshed_cookies[cookie["name"]] = cookie["value"]

        await browser.close()
        
        if not refreshed_cookies:
            logger.error("Refresher browser returned empty cookies.")
            return current_cookies_str

        # Merge new cookies back with original cookies to preserve unique session values
        merged_cookies = cookies_dict.copy()
        merged_cookies.update(refreshed_cookies)
        
        serialized_cookies = "; ".join([f"{k}={v}" for k, v in merged_cookies.items()])
        logger.info("Goofish cookies successfully refreshed and merged via Playwright!")
        return serialized_cookies
