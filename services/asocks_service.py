import aiohttp
import json
from loguru import logger
from typing import Dict, Any, Optional

class ASocksService:
    """
    Service to interact with the ASocks API.
    Retrieves active proxy credentials and geolocational mappings dynamically.
    """
    
    def __init__(self, api_key: str):
        self.api_key = api_key
        self.base_url = "https://api.asocks.com/v2"

    async def get_active_proxy(self) -> Optional[Dict[str, Any]]:
        """
        Fetches the first active residential proxy port from ASocks.
        If no active port exists, it creates one.
        """
        connector = aiohttp.TCPConnector(ssl=False) # Bypass local macOS cert verification
        
        async with aiohttp.ClientSession(connector=connector) as session:
            try:
                # 1. Fetch current ports list
                ports_url = f"{self.base_url}/proxy/ports"
                params = {"apiKey": self.api_key, "per_page": 50}
                
                logger.info("Fetching active ports list from ASocks...")
                async with session.get(ports_url, params=params, timeout=15) as response:
                    if response.status != 200:
                        logger.error(f"ASocks API error: HTTP {response.status}")
                        return None
                        
                    res_data = await response.json()
                    if not res_data.get("success"):
                        logger.error(f"ASocks response reported failure: {res_data.get('message')}")
                        return None
                        
                    proxies = res_data.get("message", {}).get("proxies", [])
                    active_proxies = [p for p in proxies if p.get("status") == 1]
                    
                    if active_proxies:
                        # Pick the first active proxy
                        target_proxy = active_proxies[0]
                        logger.info(f"Selected active ASocks proxy: {target_proxy.get('name')} ({target_proxy.get('cityName')})")
                        return self._format_proxy_response(target_proxy)
                    
                    # 2. If no active proxies, let's create a new residential proxy port (Default: GB - Great Britain)
                    logger.info("No active ports found. Creating a new ASocks residential port...")
                    create_url = f"{self.base_url}/proxy/create-port"
                    create_params = {"apiKey": self.api_key}
                    create_payload = {
                        "count": 1,
                        "proxy_type_id": 1, # Residential proxy type
                        "country": "GB"
                    }
                    
                    async with session.post(create_url, params=create_params, json=create_payload, timeout=20) as create_resp:
                        if create_resp.status != 200:
                            body = await create_resp.text()
                            logger.error(f"Failed to create ASocks port: HTTP {create_resp.status} - {body}")
                            return None
                            
                        create_data = await create_resp.json()
                        if not create_data.get("success"):
                            logger.error(f"ASocks create port failed: {create_data}")
                            return None
                            
                        # Query ports list again to get the full credentials of the new port
                        return await self.get_active_proxy()
                        
            except Exception as e:
                logger.exception(f"Error querying ASocks API: {e}")
                return None

    def _format_proxy_response(self, proxy_data: dict) -> Dict[str, Any]:
        """Formats the raw ASocks proxy node into structured coordinates and credentials."""
        return {
            "proxy_url": proxy_data.get("template"),
            "country_code": proxy_data.get("countryCode", "GB").upper(),
            "city": proxy_data.get("cityName", "London"),
            "state": proxy_data.get("stateName", "England")
        }
