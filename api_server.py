"""
Xianyu Bot REST API Test Server
Позволяет тестировать поиск, парсинг и перевод без Telegram.
Запуск: uvicorn api_server:app --host 0.0.0.0 --port 8080 --reload
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
    description="REST API для тестирования поиска и парсинга товаров с Goofish/Xianyu",
    version="1.0.0"
)


def get_db() -> Database:
    return Database(settings.DATABASE_PATH)


# ─── Health Check ─────────────────────────────────────────────────────────────

@app.get("/", response_class=HTMLResponse, tags=["Info"])
async def root():
    """Красивая HTML-страница статуса API."""
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
        <p>REST API для тестирования поиска, парсинга и перевода товаров с Goofish.</p>
        <p>📖 <a href="/docs">Swagger UI документация</a> | <a href="/redoc">ReDoc</a></p>
        <h2>Эндпоинты</h2>
        <div class="endpoint"><span class="method">GET</span> <code>/health</code> — Проверка статуса системы и конфигурации</div>
        <div class="endpoint"><span class="method">GET</span> <code>/search?query=rick+owens&page=1</code> — Поиск товаров на Goofish</div>
        <div class="endpoint"><span class="method">GET</span> <code>/search/translated?query=maison+margiela</code> — Поиск с переводом DeepL</div>
        <div class="endpoint"><span class="method">GET</span> <code>/cookies/status</code> — Статус активных куки сессии</div>
        <div class="endpoint"><span class="method">GET</span> <code>/translate?text=马丁靴&target=RU</code> — Тест перевода через DeepL</div>
    </body>
    </html>
    """


@app.get("/health", tags=["Info"])
async def health():
    """Проверка статуса системы, конфигурации и наличия ключей."""
    db = get_db()
    cookies = await db.get_cookies(settings.XIANYU_COOKIES)
    monitors = await db.get_active_monitors()

    return {
        "status": "ok",
        "config": {
            "bot_token_set": bool(settings.BOT_TOKEN and "example" not in settings.BOT_TOKEN),
            "cookies_loaded": bool(cookies),
            "cookies_length": len(cookies),
            "proxy_url": settings.PROXY_URL or "не задан",
            "asocks_key_set": bool(settings.ASOCKS_API_KEY),
            "deepl_key_set": bool(settings.DEEPL_API_KEY),
            "deepl_endpoint": translator.endpoint or "bypass (нет ключа)",
        },
        "database": {
            "path": settings.DATABASE_PATH,
            "active_monitors": len(monitors)
        }
    }


# ─── Search ───────────────────────────────────────────────────────────────────

@app.get("/search", tags=["Search"])
async def search_raw(
    query: str = Query(..., description="Поисковый запрос (например: rick owens)"),
    page: int = Query(1, ge=1, description="Номер страницы"),
    price_min: Optional[int] = Query(None, description="Минимальная цена в юанях"),
    price_max: Optional[int] = Query(None, description="Максимальная цена в юанях"),
    limit: int = Query(5, ge=1, le=20, description="Количество товаров")
):
    """
    Поиск товаров на Goofish. Возвращает сырые распарсенные данные без перевода.
    """
    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)

    if not active_cookies:
        raise HTTPException(status_code=401, detail="Нет активных куки. Сначала установи cookies через /setcookies в боте.")

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
    query: str = Query(..., description="Поисковый запрос"),
    page: int = Query(1, ge=1),
    price_min: Optional[int] = Query(None),
    price_max: Optional[int] = Query(None),
    limit: int = Query(3, ge=1, le=10),
    target_lang: str = Query("RU", description="Язык перевода (RU, EN, DE, ...)")
):
    """
    Поиск с автоматическим переводом названий и регионов через DeepL.
    Именно это видит пользователь в Telegram-боте.
    """
    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)

    if not active_cookies:
        raise HTTPException(status_code=401, detail="Нет активных куки. Установи cookies через /setcookies в боте.")

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

        # Параллельный перевод всех товаров
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
                f"💰 Цена: {item.price}\n"
                f"📍 Регион: {t_area or 'Не указан'}\n"
                f"👤 Продавец: {item.user_nickname}\n"
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
    """Проверяет наличие и базовый формат активных куки сессии."""
    db = get_db()
    cookies = await db.get_cookies(settings.XIANYU_COOKIES)

    if not cookies:
        return {"status": "empty", "message": "Куки не установлены. Используй /setcookies в боте."}

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
    text: str = Query(..., description="Текст для перевода (на китайском)"),
    source: str = Query("ZH", description="Исходный язык"),
    target: str = Query("RU", description="Целевой язык")
):
    """Тестирует DeepL перевод напрямую."""
    if not settings.DEEPL_API_KEY:
        return {
            "deepl_active": False,
            "message": "DeepL API ключ не задан. Добавь DEEPL_API_KEY в .env и перезапусти контейнер.",
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
