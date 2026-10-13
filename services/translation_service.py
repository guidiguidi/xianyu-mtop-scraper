import aiohttp
from loguru import logger
from config import settings

class DeepLTranslationService:
    def __init__(self, api_key: str = ""):
        self.api_key = api_key or settings.DEEPL_API_KEY
        self.cache = {}  # In-memory translation cache to save credits/characters

        # Determine endpoint based on API key structure (standard DeepL practice)
        if self.api_key:
            if self.api_key.endswith(":fx"):
                self.endpoint = "https://api-free.deepl.com/v2/translate"
                logger.info("DeepL Translation Service initialized using FREE API endpoint.")
            else:
                self.endpoint = "https://api.deepl.com/v2/translate"
                logger.info("DeepL Translation Service initialized using PRO API endpoint.")
        else:
            self.endpoint = None
            logger.warning("DeepL API Key is missing. Translation service will operate in bypass mode (returning original text).")

    async def translate(self, text: str, source_lang: str = "ZH", target_lang: str = "RU") -> str:
        """Translates text asynchronously from source to target language using DeepL API."""
        if not text or not text.strip():
            return text

        # Clean string
        text = text.strip()

        # 1. Check in-memory cache
        cache_key = f"{source_lang}:{target_lang}:{text}"
        if cache_key in self.cache:
            return self.cache[cache_key]

        # 2. If no API key configured, bypass translation
        if not self.api_key or not self.endpoint:
            return text

        try:
            headers = {
                "Authorization": f"DeepL-Auth-Key {self.api_key}",
                "Content-Type": "application/json"
            }
            payload = {
                "text": [text],
                "target_lang": target_lang.upper(),
                "source_lang": source_lang.upper()
            }

            # Execute async HTTP POST call with 5-second timeout
            async with aiohttp.ClientSession() as session:
                async with session.post(self.endpoint, headers=headers, json=payload, timeout=5.0) as response:
                    if response.status == 200:
                        data = await response.json()
                        translated_text = data["translations"][0]["text"]
                        
                        # Cache the successful translation
                        self.cache[cache_key] = translated_text
                        logger.debug(f"Translated successfully via DeepL: '{text}' -> '{translated_text}'")
                        return translated_text
                    else:
                        error_body = await response.text()
                        logger.error(f"DeepL API returned error status {response.status}: {error_body}")
                        return text

        except Exception as e:
            logger.warning(f"Failed to translate text '{text}' via DeepL: {str(e)}. Falling back to original.")
            return text

# Centralized global translator instance
translator = DeepLTranslationService()
