import asyncio
import sys
import argparse
import json
from typing import Dict, Optional, Any

# Route Loguru to stderr so it doesn't pollute stdout (which our Go app reads)
from loguru import logger
logger.remove()
logger.add(sys.stderr, format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - <level>{message}</level>", level="INFO")

from services.browser_refresher import refresh_goofish_cookies
from services.asocks_service import ASocksService

async def main():
    parser = argparse.ArgumentParser(description="Goofish Cookie Refresher CLI")
    parser.add_argument("--cookies", type=str, default="", help="Current cookie string")
    parser.add_argument("--proxy-url", type=str, default="", help="Optional connection proxy URL")
    parser.add_argument("--asocks-key", type=str, default="", help="Optional ASocks API key for dynamic residential geo-targeting")
    
    args = parser.parse_args()
    
    cookies_str = args.cookies.strip()
    
    # 2. Invoke browser cookie refresher WITHOUT proxy (direct connection is faster and more reliable)
    try:
        new_cookies = await refresh_goofish_cookies(
            current_cookies_str=cookies_str,
            proxy_url=None,
            geo_info=None
        )
        
        # Print ONLY the final cookies string to stdout for the Go app to capture
        sys.stdout.write(new_cookies.strip())
        sys.stdout.flush()
        logger.info("Playwright refresher finished successfully. Printed output to stdout.")
        sys.exit(0)
    except Exception as e:
        logger.error(f"Playwright refresher failed with error: {e}")
        # Print the original cookies as fallback to prevent empty database corruption
        sys.stdout.write(cookies_str)
        sys.stdout.flush()
        sys.exit(1)

if __name__ == "__main__":
    asyncio.run(main())
