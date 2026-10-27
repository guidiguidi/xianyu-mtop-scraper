"""
Xianyu Bot REST API Test Server
Allows testing search, parsing, and data retrieval without Telegram client.
Run: uvicorn api_server:app --host 0.0.0.0 --port 8080 --reload
"""

import asyncio
import sys
import os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from fastapi import FastAPI, Query, HTTPException
from fastapi.responses import JSONResponse, HTMLResponse
from typing import Optional
from loguru import logger

from config import settings
from database import Database
from goofish_client import GoofishSearchClient
from services.parser import parse_search_response
from services.translation_service import translator

app = FastAPI(
    title="🐟 Xianyu Bot API",
    description="REST API for querying and parsing listings from Goofish / Xianyu",
    version="1.0.0"
)


def get_db() -> Database:
    return Database(settings.DATABASE_PATH)


# ─── Health Check ─────────────────────────────────────────────────────────────

@app.get("/", response_class=HTMLResponse, tags=["Info"])
async def root():
    """HTML dashboard for API health status."""
    return """
    <!DOCTYPE html>
    <html lang="ru">
    <head>
        <meta charset="UTF-8">
        <title>🐟 Xianyu Bot API</title>
        <style>
            body { font-family: -apple-system, sans-serif; background: #0f0f0f; color: #e0e0e0; padding: 40px; }
            h1 { color: #4fc3f7; }
            .endpoint { background: #1a1a2e; border-left: 3px solid #4fc3f7; padding: 12px 16px; margin: 10px 0; border-radius: 4px; }
            .method { background: #00897b; color: white; padding: 2px 8px; border-radius: 3px; font-size: 12px; font-weight: bold; }
            a { color: #4fc3f7; }
            code { background: #1e1e1e; padding: 2px 6px; border-radius: 3px; }
        </style>
    </head>
    <body>
        <h1>🐟 Xianyu Bot API</h1>
        <p>REST API for searching, parsing, and normalizing listings from Goofish / Xianyu.</p>
        <p>📖 <a href="/docs">Swagger UI Documentation</a> | <a href="/redoc">ReDoc</a></p>
        <h2>Endpoints</h2>
        <div class="endpoint"><span class="method">GET</span> <code>/health</code> — System health check and configuration status</div>
        <div class="endpoint"><span class="method">GET</span> <code>/search?query=rick+owens&page=1</code> — Search Goofish listings (raw normalized)</div>
        <div class="endpoint"><span class="method">GET</span> <code>/search/translated?query=maison+margiela</code> — Search with DeepL translation pipeline</div>
        <div class="endpoint"><span class="method">GET</span> <code>/cookies/status</code> — Active session cookies status</div>
        <div class="endpoint"><span class="method">GET</span> <code>/translate?text=马丁靴&target=EN</code> — Direct DeepL translation probe</div>
    </body>
    </html>
    """


@app.get("/health", tags=["Info"])
async def health():
    """System health, configuration, and credentials verification."""
    db = get_db()
    cookies = await db.get_cookies(settings.XIANYU_COOKIES)
    monitors = await db.get_active_monitors()

    return {
        "status": "ok",
        "config": {
            "bot_token_set": bool(settings.BOT_TOKEN and "example" not in settings.BOT_TOKEN),
            "cookies_loaded": bool(cookies),
            "cookies_length": len(cookies),
            "proxy_url": settings.PROXY_URL or "not configured",
            "asocks_key_set": bool(settings.ASOCKS_API_KEY),
            "deepl_key_set": bool(settings.DEEPL_API_KEY),
            "deepl_endpoint": translator.endpoint or "bypass (key missing)",
        },
        "database": {
            "path": settings.DATABASE_PATH,
            "active_monitors": len(monitors)
        }
    }


# ─── Search ───────────────────────────────────────────────────────────────────

@app.get("/search", tags=["Search"])
async def search_raw(
    query: str = Query(..., description="Search query keyword (e.g., rick owens)"),
    page: int = Query(1, ge=1, description="Page index"),
    price_min: Optional[int] = Query(None, description="Minimum price in CNY"),
    price_max: Optional[int] = Query(None, description="Maximum price in CNY"),
    limit: int = Query(5, ge=1, le=20, description="Listing limit per page")
):
    """
    Queries Goofish items. Returns normalized listing objects.
    """
    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)

    if not active_cookies:
        raise HTTPException(status_code=401, detail="No active cookies found. Please supply session cookies via /setcookies.")

    async def dummy_refresh(new_cookies: str):
        await db.save_cookies(new_cookies)

    client = GoofishSearchClient(
        cookies_str=active_cookies,
        proxy_url=settings.PROXY_URL,
        on_cookies_refreshed=dummy_refresh
    )

    try:
        raw_response = await client.search(
            keyword=query,
            page=page,
            price_min=price_min,
            price_max=price_max,
            rows_per_page=limit
        )
        items = parse_search_response(raw_response)

        return {
            "query": query,
            "page": page,
            "total_found": len(items),
            "items": [item.to_dict() for item in items]
        }

    except Exception as e:
        logger.exception(f"Search API error for query '{query}'")
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/search/translated", tags=["Search"])
async def search_translated(
    query: str = Query(..., description="Search keyword"),
    page: int = Query(1, ge=1),
    price_min: Optional[int] = Query(None),
    price_max: Optional[int] = Query(None),
    limit: int = Query(3, ge=1, le=10),
    target_lang: str = Query("EN", description="Target translation language (EN, DE, ...)")
):
    """
    Search endpoint with automated metadata translation.
    Matches the payload formatted for the Telegram notification pipeline.
    """
    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)

    if not active_cookies:
        raise HTTPException(status_code=401, detail="No active cookies found. Please supply session cookies.")

    async def dummy_refresh(new_cookies: str):
        await db.save_cookies(new_cookies)

    client = GoofishSearchClient(
        cookies_str=active_cookies,
        proxy_url=settings.PROXY_URL,
        on_cookies_refreshed=dummy_refresh
    )

    try:
        raw_response = await client.search(
            keyword=query,
            page=page,
            price_min=price_min,
            price_max=price_max,
            rows_per_page=limit
        )
        items = parse_search_response(raw_response)

        if not items:
            return {"query": query, "page": page, "total_found": 0, "items": []}

        # Concurrent translation processing
        async def translate_item(item):
            t_title = await translator.translate(item.title, source_lang="ZH", target_lang=target_lang)
            t_area = await translator.translate(item.area, source_lang="ZH", target_lang=target_lang) if item.area else item.area
            return t_title, t_area

        translations = await asyncio.gather(*(translate_item(item) for item in items))

        result = []
        for i, item in enumerate(items):
            t_title, t_area = translations[i]
            d = item.to_dict()
            d["title_translated"] = t_title
            d["area_translated"] = t_area
            d["telegram_caption"] = (
                f"🐟 [{i+1}/{len(items)}] {t_title[:80]}...\n\n"
                f"💰 Price: {item.price}\n"
                f"📍 Region: {t_area or 'Unspecified'}\n"
                f"👤 Seller: {item.user_nickname}\n"
                f"🔗 {item.share_url}"
            )
            result.append(d)

        return {
            "query": query,
            "page": page,
            "target_lang": target_lang,
            "deepl_active": bool(settings.DEEPL_API_KEY),
            "total_found": len(result),
            "items": result
        }

    except Exception as e:
        logger.exception(f"Translated search API error for query '{query}'")
        raise HTTPException(status_code=500, detail=str(e))


# ─── Cookies ──────────────────────────────────────────────────────────────────

@app.get("/cookies/status", tags=["Cookies"])
async def cookies_status():
    """Verifies format and presence of active session cookies."""
    db = get_db()
    cookies = await db.get_cookies(settings.XIANYU_COOKIES)

    if not cookies:
        return {"status": "empty", "message": "Cookies not configured. Use /setcookies."}

    # Parse cookie count
    cookie_pairs = [c.strip() for c in cookies.split(";") if "=" in c]
    names = [p.split("=")[0].strip() for p in cookie_pairs]

    return {
        "status": "ok",
        "cookie_count": len(cookie_pairs),
        "cookie_names": names,
        "has_h5_token": "_m_h5_tk" in cookies,
        "has_tb_token": "_tb_token_" in cookies
    }


# ─── Translation ──────────────────────────────────────────────────────────────

@app.get("/translate", tags=["Translation"])
async def test_translate(
    text: str = Query(..., description="Text to translate (Chinese source)"),
    source: str = Query("ZH", description="Source language"),
    target: str = Query("EN", description="Target language")
):
    """Direct translation test endpoint."""
    if not settings.DEEPL_API_KEY:
        return {
            "deepl_active": False,
            "message": "DeepL API key not provided. Set DEEPL_API_KEY in .env.",
            "original": text,
            "translated": text
        }

    translated = await translator.translate(text, source_lang=source, target_lang=target)
    return {
        "deepl_active": True,
        "endpoint": translator.endpoint,
        "source_lang": source,
        "target_lang": target,
        "original": text,
        "translated": translated,
        "cached": (f"{source}:{target}:{text}") in translator.cache
    }


# ─── Entrypoint ───────────────────────────────────────────────────────────────

if __name__ == "__main__":
    import uvicorn
    uvicorn.run("api_server:app", host="0.0.0.0", port=8080, reload=True)
