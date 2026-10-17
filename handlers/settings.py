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
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    await message.answer(
        "🍪 *Обновление Cookies Goofish*\n\n"
        "Отправьте мне всю строку Cookies (из Antidetect браузера или DevTools).\n"
        "Обязательно должны присутствовать `cookie2` и `_m_h5_tk`.\n\n"
        "Отправьте /cancel для отмены.",
        parse_mode="Markdown"
    )
    await state.set_state(SettingsState.waiting_for_cookies)

@router.message(Command("cancel"))
@router.message(SettingsState.waiting_for_cookies, Command("cancel"))
async def cmd_cancel_cookies(message: Message, state: FSMContext):
    await state.clear()
    await message.answer("🔄 Действие отменено.")

@router.message(SettingsState.waiting_for_cookies)
async def process_cookies_input(message: Message, state: FSMContext):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа.")
        await state.clear()
        return

    cookie_str = message.text.strip()
    if not cookie_str:
        await message.answer("⚠️ Пустая строка. Попробуйте еще раз.")
        return

    if "_m_h5_tk" not in cookie_str:
        await message.answer("⚠️ В строке Cookies отсутствует обязательный параметр `_m_h5_tk`. Проверьте копирование и попробуйте еще раз.")
        return

    db = get_db()
    await db.save_cookies(cookie_str)
    await state.clear()

    # Try verifying the new cookies
    client = GoofishSearchClient(cookies_str=cookie_str, proxy_url=settings.PROXY_URL)
    status_msg = await message.answer("💾 Cookies сохранены в базу данных. Проверяю соединение с API...")
    
    try:
        raw_res = await client.search(keyword="test", page=1, rows_per_page=1)
        ret_msg = raw_res.get("ret", ["Неизвестно"])[0]
        
        if "SUCCESS" in ret_msg:
            await status_msg.edit_text("✅ *Cookies успешно сохранены и проверены!* Соединение с Goofish API работает.", parse_mode="Markdown")
        else:
            await status_msg.edit_text(f"⚠️ *Cookies сохранены, но тест API вернул ошибку:* `{ret_msg}`. Возможно, куки истекли или требуется проверка.", parse_mode="Markdown")
    except Exception as e:
        await status_msg.edit_text(f"⚠️ *Cookies сохранены, но при проверке произошла ошибка:* `{str(e)}`.\nУбедитесь, что прокси работает корректно.", parse_mode="Markdown")

@router.message(Command("status"))
async def cmd_status(message: Message):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    db = get_db()
    active_cookies = await db.get_cookies(settings.XIANYU_COOKIES)
    client = GoofishSearchClient(cookies_str=active_cookies, proxy_url=settings.PROXY_URL)
    
    has_token = "_m_h5_tk" in client.cookies
    token_val = client.cookies.get("_m_h5_tk", "отсутствует")
    
    proxy_status = f"`{settings.PROXY_URL}`" if settings.PROXY_URL else "❌ Отключен"
    
    await message.answer(
        f"🖥 *Статус Системы Xianyu Bot*\n\n"
        f"📡 Прокси: {proxy_status}\n"
        f"🍪 Наличие H5 токена: {'✅ Да' if has_token else '❌ Нет'}\n"
        f"🔑 Токен: `{token_val}`\n\n"
        f"Для обновления Cookies отправьте: /setcookies",
        parse_mode="Markdown"
    )
