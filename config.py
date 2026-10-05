import os
from typing import Set, List
from pydantic_settings import BaseSettings, SettingsConfigDict
from pydantic import field_validator

class Settings(BaseSettings):
    BOT_TOKEN: str
    ALLOWED_USER_IDS_RAW: str = "0"
    XIANYU_COOKIES: str = ""
    PROXY_URL: str = ""
    ASOCKS_API_KEY: str = ""
    DEEPL_API_KEY: str = ""
    DATABASE_PATH: str = "xianyu_bot.db"

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore"
    )

    @property
    def ALLOWED_USER_IDS(self) -> Set[int]:
        try:
            # Support both semicolon and comma separators
            raw = self.ALLOWED_USER_IDS_RAW.replace(";", ",")
            return {int(x.strip()) for x in raw.split(",") if x.strip()}
        except Exception:
            return set()

# Initialize settings
settings = Settings(_env_file=os.path.join(os.path.dirname(__file__), ".env"))
