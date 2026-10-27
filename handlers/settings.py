from aiogram import Router
from aiogram.types import Message
from aiogram.filters import Command
from aiogram.fsm.context import FSMContext
from aiogram.fsm.state import State, StatesGroup
from loguru import logger
from database import Database
from config import settings
from handlers.search import is_authorized, get_db
from goofish_client import GoofishSearchClient

router = Router()

class SettingsState(StatesGroup):
    waiting_for_cookies = State()

@router.message(Command("setcookies"))
async def cmd_set_cookies_prompt(message: Message, state: FSMContext):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ Access denied.")
        return

    await message.answer(
        "🍪 *Update Goofish Session Cookies*\n\n"
        "Send raw session Cookie header (from anti-detect browser or DevTools).\n"
        "Must include `cookie2` and `_m_h5_tk`.\n\n"
        "Send /cancel to abort.",
        parse_mode="Markdown"
    )
    await state.set_state(SettingsState.waiting_for_cookies)

@router.message(Command("cancel"))
@router.message(SettingsState.waiting_for_cookies, Command("cancel"))
async def cmd_cancel_cookies(message: Message, state: FSMContext):
    await state.clear()
    await message.answer("🔄 Action cancelled.")

@router.message(SettingsState.waiting_for_cookies)
async def process_cookies_input(message: Message, state: FSMContext):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ Access denied.")
        await state.clear()
        return

    cookie_str = message.text.strip()
    if not cookie_str:
        await message.answer("⚠️ Empty input. Please try again.")
        return

    if "_m_h5_tk" not in cookie_str:
        await message.answer("⚠️ Missing required parameters `_m_h5_tk` or `cookie2`. Please check and resend.")
        return

    db = get_db()
    await db.save_cookies(cookie_str)
    await state.clear()

    # Try verifying the new cookies
    client = GoofishSearchClient(cookies_str=cookie_str, proxy_url=settings.PROXY_URL)
    status_msg = await message.answer("💾 Cookies saved to database. Validating API connectivity...")
    
    try:
        raw_res = await client.search(keyword="test", page=1, rows_per_page=1)
        ret_msg = raw_res.get("ret", ["Unknown"])[0]
        
        if "SUCCESS" in ret_msg:
        await status_msg.edit_text("✅ *Cookies saved and verified!* API connection stable.")
        else:
            await status_msg.edit_text(f"⚠️ *Cookies persisted but API probe returned error:* `{ret_msg}`. Cookies may have expired or verification is required.", parse_mode="Markdown")
    except Exception as e:
        await status_msg.edit_text(f"⚠️ *Cookies persisted but verification threw error:* `{str(e)}`.\nEnsure proxy connection is operational.", parse_mode="Markdown")

@router.message(Command("status"))
async def cmd_status(message: Message):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ Access denied.")
        return

    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)
    client = GoofishSearchClient(cookies_str=active_cookies, proxy_url=settings.PROXY_URL)
    
    has_token = "_m_h5_tk" in client.cookies
    token_val = client.cookies.get("_m_h5_tk", "missing")
    
    proxy_status = f"`{settings.PROXY_URL}`" if settings.PROXY_URL else "❌ Disabled"
    
    await message.answer(
        f"🖥 *Xianyu Bot System Status*\n\n"
        f"📡 Proxy: {proxy_status}\n"
        f"🍪 H5 Token Status: {'✅ Active' if has_token else '❌ Inactive'}\n"
        f"🔑 Token: `{token_val}`\n\n"
        f"To refresh cookies, run: /setcookies",
        parse_mode="Markdown"
    )
