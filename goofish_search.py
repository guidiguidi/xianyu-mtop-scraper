import requests
import time
import json
import hashlib
import urllib.parse
from typing import Dict, Any, Optional

class GoofishSearchClient:
    """
    A robust client for interacting with the Goofish/Xianyu H5 search API.
    Handles automatic signature calculation and session cookie management.
    """
    
    def __init__(self, cookies: Optional[Dict[str, str]] = None):
        self.session = requests.Session()
        self.app_key = "34839810"
        
        # Default modern headers resembling standard desktop browser access
        self.session.headers.update({
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
        })
        
        if cookies:
            self.session.cookies.update(cookies)
            
    def _get_token(self) -> str:
        """
        Extracts the MTOP token from the '_m_h5_tk' cookie.
        """
        token_cookie = self.session.cookies.get('_m_h5_tk')
        if not token_cookie:
            raise ValueError("Required cookie '_m_h5_tk' is missing from the session. Initialize with valid cookies first.")
        # Token is the part before the underscore
        return token_cookie.split('_')[0]

    def calculate_signature(self, token: str, timestamp: str, data_str: str) -> str:
        """
        Generates the MD5 signature required by the Taobao H5 API.
        Format: md5(token & timestamp & appKey & data)
        """
        sign_string = f"{token}&{timestamp}&{self.app_key}&{data_str}"
        return hashlib.md5(sign_string.encode('utf-8')).hexdigest()

    def search(self, keyword: str, page: int = 1, price_min: Optional[int] = None, price_max: Optional[int] = None, rows_per_page: int = 30) -> Dict[str, Any]:
        """
        Executes a Xianyu search request with dynamic parameters and automatic signature calculation.
        """
        timestamp = str(int(time.time() * 1000))
        
        # Build propValueStr based on price range parameters
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
        
        # Serialize payload exactly as required for signature calculation (no whitespace separators)
        data_str = json.dumps(data_payload, separators=(',', ':'))
        
        token = self._get_token()
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
        
        # Request body must be URL-encoded data payload
        payload = f"data={urllib.parse.quote(data_str)}"
        
        response = self.session.post(url, params=params, data=payload)
        response.raise_for_status()
        return response.json()

# Demonstration usage block
if __name__ == '__main__':
    # Initial cookies from user request
    initial_cookies = {
        'cookie2': '127a60941c28d0db7c71433aec79c9d6',
        'mtop_partitioned_detect': '1',
        '_m_h5_tk': 'ea5147d0d9a2253bb28abdc8e1353353_1779127314126',
        '_m_h5_tk_enc': '15a2e4ba01bb7159199809ac0c970f8c',
        'cna': 'gx2SIgm22UUBASoQ1Yb3GDbh',
        'xlly_s': '1',
        '_samesite_flag_': 'true',
        't': '2d62177314ff3aa91c3fae899457710c',
        '_tb_token_': '7eb7de6e3e3e8',
        'tfstk': 'gP3tnkgOyBCO1fYBeNxnmpOJSrRnWHcZjAl5o-2GcvHKNbWgcV4iHiHigxqXQP0xpjMJ_SvZo1oEEb_iShzckPz4lLvkrUqZ_rzXq277YOPQG7L0G-Nb7ysSIfLJrUcZ11V6ZXTomdzQwSybllZfOJwUM16jho_IOJP4GONf5BhQLJN1l-s_RWN4NGNshrOKOJP_1rZshyOLgJwblrMYPKeC1-bxYbYe0AtA8aboJ5ETHGVNkkiPrPe5X8bAkRHLXGc_pZ_jJmr071yJJ9wnGbobvqpVnrln2xnjFpjgC0F-e7DMRNUjAv38VXJR7RnIQqUg4FBz6DG8RR4WW1ySSShbwztfO5gix0qtvd_YKV2Uc2Uv21PnWXgzDVJORRHEO04ZPp7xFXco7kuve9wqj7zQMqR5D-Ef4q3oyx9PE8FcfBdd0ir_T4Invhk5Ov2g98A9kiS4YBPLEBQ50ir_TWektrIV0kRF.'
    }
    
    client = GoofishSearchClient(cookies=initial_cookies)
    
    print("Initiating Goofish API Search request...")
    try:
        results = client.search(keyword="rickowens", price_min=5000, price_max=15000)
        print("Success! Response API status:")
        print(json.dumps(results.get("ret", []), indent=2))
    except Exception as e:
        print("Error executing request:", e)
