from aiogram import Router, F
from aiogram.types import Message, CallbackQuery, InputMediaPhoto, InlineKeyboardMarkup, InlineKeyboardButton
from aiogram.filters import Command
from aiogram.fsm.context import FSMContext
from aiogram.fsm.state import State, StatesGroup
from loguru import logger
import re
from config import settings
from database import Database
from goofish_client import GoofishSearchClient
from services.parser import parse_search_response

router = Router()

import html

class SearchState(StatesGroup):
    waiting_for_query = State()

def get_db() -> Database:
    return Database(settings.DATABASE_PATH)

def is_authorized(user_id: int) -> bool:
    allowed = settings.ALLOWED_USER_IDS
    # If allowed contains 0, allow all, otherwise strict match
    return (0 in allowed) or (user_id in allowed)

def build_search_keyboard(query: str, page: int, price_min: str = "", price_max: str = "") -> InlineKeyboardMarkup:
    """Builds interactive inline buttons for search control."""
    buttons = [
        [
            InlineKeyboardButton(text="◀️ Назад", callback_data=f"page:{page-1}:{query}:{price_min}:{price_max}"),
            InlineKeyboardButton(text=f"📄 Стр. {page}", callback_data="noop"),
            InlineKeyboardButton(text="Вперед ▶️", callback_data=f"page:{page+1}:{query}:{price_min}:{price_max}")
        ],
        [
            InlineKeyboardButton(text="💰 Любая цена", callback_data=f"price:{query}::"),
            InlineKeyboardButton(text="💰 до ¥1000", callback_data=f"price:{query}::1000"),
            InlineKeyboardButton(text="💰 ¥1000-5000", callback_data=f"price:{query}:1000:5000")
        ],
        [
            InlineKeyboardButton(text="💰 ¥5000-15000", callback_data=f"price:{query}:5000:15000"),
            InlineKeyboardButton(text="💰 ¥15000+", callback_data=f"price:{query}:15000:")
        ],
        [
            InlineKeyboardButton(text="🔔 Добавить в мониторинг", callback_data=f"addmon:{query}:{price_min}:{price_max}")
        ]
    ]
    return InlineKeyboardMarkup(inline_keyboard=buttons)

@router.message(Command("search"))
async def cmd_search_prompt(message: Message, state: FSMContext):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    # Extract eventual inline text argument
    args = message.text.split(maxsplit=1)
    if len(args) > 1:
        query = args[1].strip()
        await execute_search_action(message, query, 1)
        return

    await message.answer("🔍 Введите поисковый запрос (например: <b>rick owens</b>):", parse_mode="HTML")
    await state.set_state(SearchState.waiting_for_query)

@router.message(SearchState.waiting_for_query)
async def process_search_query(message: Message, state: FSMContext):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        await state.clear()
        return

    query = message.text.strip()
    if not query:
        await message.answer("⚠️ Пустой запрос. Попробуйте еще раз.")
        return

    await state.clear()
    await execute_search_action(message, query, 1)

async def execute_search_action(
    message: Message, 
    query: str, 
    page: int, 
    price_min: str = "", 
    price_max: str = "",
    edit_message_id: int = None
):
    """Core search execution: calls goofish api, parses items, outputs results."""
    # Load fresh active cookies from database
    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)
    
    # Callback to automatically persist dynamic cookie refreshes
    async def persist_refreshed_cookies(new_cookies_str: str):
        await db.save_cookies(new_cookies_str)
        logger.info("Auto-refreshed search cookies persisted successfully.")
    
    # Initialize client with dynamic cookies, proxy url and automatic database persistence callback
    client = GoofishSearchClient(
        cookies_str=active_cookies,
        proxy_url=settings.PROXY_URL,
        on_cookies_refreshed=persist_refreshed_cookies
    )

    status_msg = await message.answer("⏳ Выполняю поиск на Goofish...")
    
    try:
        p_min = int(price_min) if price_min else None
        p_max = int(price_max) if price_max else None
        
        # Execute H5 MTOP request
        raw_response = await client.search(
            keyword=query,
            page=page,
            price_min=p_min,
            price_max=p_max,
            rows_per_page=3  # 3 items per page — one photo per card, clean output
        )

        items = parse_search_response(raw_response)
        
        if not items:
            await status_msg.edit_text("🔍 Ничего не найдено. Проверьте запрос, ценовой фильтр или обновите cookies через /setcookies.")
            return

        # Batch translate item titles and areas concurrently using DeepL
        import asyncio
        from services.translation_service import translator
        
        async def translate_item(item):
            t_title = await translator.translate(item.title, source_lang="ZH", target_lang="RU")
            t_area = await translator.translate(item.area, source_lang="ZH", target_lang="RU") if item.area else ""
            return t_title, t_area

        translation_results = await asyncio.gather(*(translate_item(item) for item in items))

        # Delete waiting message before sending results
        await status_msg.delete()

        # Send each item as its own photo message with correct image + full caption
        for i, item in enumerate(items):
            translated_title, translated_area = translation_results[i]
            escaped_title = html.escape(translated_title[:80])
            escaped_price = html.escape(str(item.price))
            escaped_area = html.escape(translated_area) if translated_area else "Не указан"
            escaped_nickname = html.escape(item.user_nickname) if item.user_nickname else "Не указан"
            escaped_want = html.escape(item.want_count) if item.want_count else ""

            # Build badge line
            badges = []
            if item.is_verified:
                badges.append("✅ Проверен платформой")
            if item.is_new_item:
                badges.append("🆕 Новый")
            if item.freshness_label:
                badges.append(item.freshness_label)
            badge_line = "  ".join(badges)

            num = f"[{i+1}/{len(items)}]"
            caption_parts = [
                f"🐟 <b>{num} {escaped_title}...</b>",
                "",
                f"💰 Цена: <code>{escaped_price}</code>",
                f"📍 Регион: {escaped_area}",
                f"👤 Продавец: {escaped_nickname}",
            ]
            if escaped_want:
                caption_parts.append(f"📦 {escaped_want}")
            if badge_line:
                caption_parts.append(badge_line)
            caption_parts.append("")
            caption_parts.append(f'<a href="{item.share_url}">🔗 Открыть на Xianyu</a>')
            caption = "\n".join(caption_parts)

            # Each item sends its own correct picture
            pic = item.pic_url if item.pic_url else "https://img.alicdn.com/tps/i4/TB1OKd7JXXXXXb1XFXXc8s9FXXX-400-400.png"

            try:
                await message.answer_photo(
                    photo=pic,
                    caption=caption,
                    parse_mode="HTML"
                )
            except Exception as photo_err:
                logger.warning(f"Failed to send photo for item {item.item_id}: {photo_err}. Sending text fallback.")
                await message.answer(caption, parse_mode="HTML")

        # Navigation keyboard after the results
        keyboard = build_search_keyboard(query, page, price_min, price_max)
        escaped_query = html.escape(query)
        await message.answer(
            f"👇 Управление результатами по запросу <b>'{escaped_query}'</b> (стр. {page}):",
            reply_markup=keyboard,
            parse_mode="HTML"
        )

    except Exception as e:
        logger.exception("Error executing Xianyu search")
        try:
            await status_msg.edit_text(f"❌ Ошибка поиска: {html.escape(str(e))}", parse_mode="HTML")
        except Exception:
            # Fallback if status_msg was already deleted or not found
            await message.answer(f"❌ Ошибка поиска: {html.escape(str(e))}", parse_mode="HTML")

# --- Callback query navigation handlers ---

@router.callback_query(F.data.startswith("page:"))
async def process_page_callback(callback: CallbackQuery):
    if not is_authorized(callback.from_user.id):
        await callback.answer("❌ У вас нет доступа.", show_alert=True)
        return

    data_parts = callback.data.split(":")
    page = int(data_parts[1])
    query = data_parts[2]
    price_min = data_parts[3] if len(data_parts) > 3 else ""
    price_max = data_parts[4] if len(data_parts) > 4 else ""

    if page < 1:
        await callback.answer("⚠️ Вы уже на первой странице.", show_alert=True)
        return

    await callback.message.delete()
    await execute_search_action(
        message=callback.message,
        query=query,
        page=page,
        price_min=price_min,
        price_max=price_max
    )
    await callback.answer()

@router.callback_query(F.data.startswith("price:"))
async def process_price_callback(callback: CallbackQuery):
    if not is_authorized(callback.from_user.id):
        await callback.answer("❌ У вас нет доступа.", show_alert=True)
        return

    data_parts = callback.data.split(":")
    query = data_parts[1]
    price_min = data_parts[2] if len(data_parts) > 2 else ""
    price_max = data_parts[3] if len(data_parts) > 3 else ""

    await callback.message.delete()
    await execute_search_action(
        message=callback.message,
        query=query,
        page=1,
        price_min=price_min,
        price_max=price_max
    )
    await callback.answer(f"Применен фильтр цен: {price_min}-{price_max}")

@router.callback_query(F.data == "noop")
async def process_noop_callback(callback: CallbackQuery):
    await callback.answer()
