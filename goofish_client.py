import time
import json
import hashlib
import urllib.parse
import aiohttp
from typing import Dict, Any, Optional, Callable, Awaitable
from loguru import logger
from services.browser_refresher import refresh_goofish_cookies
from services.asocks_service import ASocksService
from config import settings

class GoofishSearchClient:
    """
    Async client for Goofish/Xianyu search API using aiohttp.
    Supports dynamic ASocks proxying, robust cookie handling, signature generation,
    and automatic browser cookie renewal on token expiration errors.
    """
    
    def __init__(
        self, 
        cookies_str: str = "", 
        proxy_url: str = "",
        on_cookies_refreshed: Optional[Callable[[str], Awaitable[None]]] = None
    ):
        self.app_key = "34839810"
        self.proxy_url = proxy_url if proxy_url.strip() else None
        self.cookies: Dict[str, str] = {}
        self.on_cookies_refreshed = on_cookies_refreshed
        
        # Load ASocks configurations
        self.asocks_key = settings.ASOCKS_API_KEY if settings.ASOCKS_API_KEY.strip() else None
        
        if cookies_str:
            self.parse_cookies_string(cookies_str)
            
        self.headers = {
            'accept': 'application/json',
            'accept-language': 'en-GB,en-US;q=0.9,en;q=0.8',
            'content-type': 'application/x-www-form-urlencoded',
            'origin': 'https://www.goofish.com',
            'referer': 'https://www.goofish.com/',
            'sec-ch-ua': '"Chromium";v="148", "Google Chrome";v="148", "Not/A)Brand";v="99"',
            'sec-ch-ua-mobile': '?0',
            'sec-ch-ua-platform': '"macOS"',
            'sec-fetch-dest': 'empty',
            'sec-fetch-mode': 'cors',
            'sec-fetch-site': 'same-site',
            'user-agent': 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36'
        }

    def parse_cookies_string(self, cookie_str: str):
        """
        Parses a standard semicolon-separated cookie string into a dictionary.
        """
        parsed: Dict[str, str] = {}
        for part in cookie_str.split(';'):
            part = part.strip()
            if '=' in part:
                k, v = part.split('=', 1)
                parsed[k.strip()] = v.strip()
        self.cookies.update(parsed)
        logger.info(f"Loaded {len(parsed)} cookies successfully.")

    def get_cookies_string(self) -> str:
        """
        Reconstructs the cookie dictionary back into a header string.
        """
        return "; ".join([f"{k}={v}" for k, v in self.cookies.items()])

    def _get_token(self) -> str:
        """
        Extracts the H5 signature token from the cookies dictionary.
        """
        token_cookie = self.cookies.get('_m_h5_tk')
        if not token_cookie:
            logger.warning("Cookie '_m_h5_tk' is missing from current session.")
            return ""
        return token_cookie.split('_')[0]

    def calculate_signature(self, token: str, timestamp: str, data_str: str) -> str:
        """
        Calculates the MTOP signature hash (MD5).
        """
        sign_string = f"{token}&{timestamp}&{self.app_key}&{data_str}"
        return hashlib.md5(sign_string.encode('utf-8')).hexdigest()

    async def force_cookie_refresh(self):
        """
        Triggers the Playwright engine to fetch fresh cookies.
        Dynamically integrates ASocks residential proxy geolocations if available.
        """
        try:
            current_cookies = self.get_cookies_string()
            geo_info = None

            # 1. Fetch dynamic proxy and geo settings from ASocks if API key is configured
            if self.asocks_key:
                logger.info("Connecting to ASocks API to fetch residential proxy geo info...")
                asocks = ASocksService(self.asocks_key)
                active_proxy = await asocks.get_active_proxy()
                
                if active_proxy:
                    self.proxy_url = active_proxy["proxy_url"]
                    geo_info = active_proxy
                    logger.info(f"Dynamically loaded ASocks proxy: {self.proxy_url} located in {geo_info.get('city')}, {geo_info.get('country_code')}")
            
            # 2. Run Playwright headless Chromium refresher
            refreshed = await refresh_goofish_cookies(
                current_cookies_str=current_cookies,
                proxy_url=self.proxy_url,
                geo_info=geo_info
            )
            
            if refreshed and refreshed != current_cookies:
                self.parse_cookies_string(refreshed)
                if self.on_cookies_refreshed:
                    await self.on_cookies_refreshed(refreshed)
                    logger.info("Auto-refreshed cookies persisted to database successfully.")
        except Exception as e:
            logger.error(f"Failed to auto-refresh cookies: {e}")

    async def search(self, keyword: str, page: int = 1, price_min: Optional[int] = None, price_max: Optional[int] = None, rows_per_page: int = 30, retry_on_expired: bool = True) -> Dict[str, Any]:
        """
        Performs an asynchronous search request to Goofish/Xianyu API.
        Automatically handles signature expired/illegal token retries via Playwright and ASocks.
        """
        timestamp = str(int(time.time() * 1000))
        
        # Build price range string
        price_range = ""
        if price_min is not None or price_max is not None:
            min_val = price_min if price_min is not None else ""
            max_val = price_max if price_max is not None else ""
            price_range = f"priceRange:{min_val},{max_val};"
            
        data_payload = {
            "pageNumber": page,
            "keyword": keyword,
            "fromFilter": True,
            "rowsPerPage": rows_per_page,
            "sortValue": "",
            "sortField": "",
            "customDistance": "",
            "gps": "",
            "propValueStr": {
                "searchFilter": price_range
            },
            "customGps": "",
            "searchReqFromPage": "pcSearch",
            "extraFilterValue": "{}",
            "userPositionJson": "{}"
        }
        
        data_str = json.dumps(data_payload, separators=(',', ':'))
        token = self._get_token()
        
        if not token:
            if retry_on_expired:
                logger.warning("No H5 token found. Attempting automatic cookie refresh...")
                await self.force_cookie_refresh()
                return await self.search(keyword, page, price_min, price_max, rows_per_page, retry_on_expired=False)
            else:
                raise ValueError("No valid H5 token found. Update your cookies.")
            
        signature = self.calculate_signature(token, timestamp, data_str)
        
        params = {
            'jsv': '2.7.2',
            'appKey': self.app_key,
            't': timestamp,
            'sign': signature,
            'v': '1.0',
            'type': 'originaljson',
            'accountSite': 'xianyu',
            'dataType': 'json',
            'timeout': '20000',
            'api': 'mtop.taobao.idlemtopsearch.pc.search',
            'sessionOption': 'AutoLoginOnly',
            'spm_cnt': 'a21ybx.search.0.0',
            'spm_pre': 'a21ybx.home.searchInput.0'
        }
        
        url = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemtopsearch.pc.search/1.0/"
        payload = f"data={urllib.parse.quote(data_str)}"
        
        headers = self.headers.copy()
        headers['cookie'] = self.get_cookies_string()
        
        logger.debug(f"Sending async post to {url} with proxy: {self.proxy_url}")
        
        # Bypass local cert verification if required
        connector = aiohttp.TCPConnector(ssl=False)
        
        async with aiohttp.ClientSession(connector=connector) as session:
            try:
                async with session.post(
                    url, 
                    params=params, 
                    data=payload, 
                    headers=headers, 
                    proxy=self.proxy_url,
                    timeout=20
                ) as response:
                    if response.status != 200:
                        body = await response.text()
                        logger.error(f"Goofish response error {response.status}: {body}")
                        raise RuntimeError(f"HTTP Error {response.status}")
                        
                    result = await response.json()
                    
                    # Inspect for H5 token expiration errors
                    ret_list = result.get("ret", [])
                    token_expired = False
                    for ret in ret_list:
                        if any(err in ret for err in ["FAIL_SYS_TOKEN", "FAIL_SYS_ILLEGAL_ACCESS", "TOKEN_EXPIRED", "ERR_SID_INVALID"]):
                            token_expired = True
                            break
                            
                    if token_expired and retry_on_expired:
                        logger.warning(f"Taobao H5 token expired or rejected ({ret_list}). Initiating auto-refresh...")
                        await self.force_cookie_refresh()
                        # Retry search with fresh cookie context
                        return await self.search(keyword, page, price_min, price_max, rows_per_page, retry_on_expired=False)
                    
                    # Update cookies from set-cookie headers if provided to keep session fresh
                    new_cookies = {}
                    for cookie_header in response.headers.getall('set-cookie', []):
                        for part in cookie_header.split(';'):
                            part = part.strip()
                            if '=' in part:
                                k, v = part.split('=', 1)
                                if k.strip() in ['_m_h5_tk', '_m_h5_tk_enc']:
                                    new_cookies[k.strip()] = v.strip()
                    
                    if new_cookies:
                        self.cookies.update(new_cookies)
                        logger.info(f"Updated H5 tokens dynamically: {new_cookies}")
                        if self.on_cookies_refreshed:
                            await self.on_cookies_refreshed(self.get_cookies_string())
                        
                    return result
            except Exception as e:
                logger.error(f"Search request failed: {e}")
                raise e
