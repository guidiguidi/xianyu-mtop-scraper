from aiogram import Router, F
from aiogram.types import Message, CallbackQuery
from aiogram.filters import Command
from loguru import logger
from database import Database
from config import settings
from handlers.search import is_authorized, get_db

router = Router()

@router.message(Command("monitor"))
async def cmd_add_monitor(message: Message):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    # Expected arguments: /monitor <keyword> [<price_min> <price_max>]
    args = message.text.split(maxsplit=1)
    if len(args) < 2:
        await message.answer(
            "⚠️ Некорректный синтаксис.\n\n"
            "Примеры:\n"
            "• `/monitor rickowens` - следить за всеми новыми товарами\n"
            "• `/monitor rickowens 5000 15000` - следить в диапазоне цен от ¥5000 до ¥15000",
            parse_mode="Markdown"
        )
        return

    payload = args[1].strip()
    
    # Try parsing keyword and prices
    price_min = None
    price_max = None
    
    # Regex matching trailing integer pairs (prices) at the end of the query string
    match = re_match = None
    parts = payload.rsplit(maxsplit=2)
    if len(parts) >= 3:
        try:
            # Check if last two arguments are numbers
            p_min = int(parts[-2])
            p_max = int(parts[-1])
            keyword = parts[0].strip()
            price_min = p_min
            price_max = p_max
        except ValueError:
            keyword = payload
    else:
        keyword = payload

    db = get_db()
    monitor_id = await db.add_monitor(
        user_id=message.from_user.id,
        keyword=keyword,
        price_min=price_min,
        price_max=price_max,
        interval_sec=300 # Default to 5 minutes
    )

    price_filter_msg = f" в диапазоне ¥{price_min}-¥{price_max}" if price_min is not None or price_max is not None else ""
    await message.answer(
        f"✅ *Мониторинг успешно запущен!*\n\n"
        f"🆔 ID Задачи: `{monitor_id}`\n"
        f"🔍 Ключевое слово: *{keyword}*\n"
        f"💰 Фильтр цен: {price_filter_msg if price_filter_msg else 'отсутствует'}\n"
        f"⏱ Интервал проверки: `5 минут`",
        parse_mode="Markdown"
    )

@router.message(Command("monitors"))
async def cmd_list_monitors(message: Message):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    db = get_db()
    monitors = await db.get_user_monitors(message.from_user.id)
    
    if not monitors:
        await message.answer("🔍 У вас нет активных задач мониторинга. Запустите одну с помощью /monitor")
        return

    response = "📋 *Список ваших активных задач мониторинга:*\n\n"
    for mon in monitors:
        price_min = mon.get("price_min")
        price_max = mon.get("price_max")
        price_str = f"¥{price_min}-¥{price_max}" if price_min is not None or price_max is not None else "Любая"
        
        response += (
            f"🔸 *ID {mon.get('id')}*: *{mon.get('keyword')}*\n"
            f"   💰 Цена: `{price_str}` | ⏱ Интервал: `5 мин` | Статус: `Активен`\n"
            f"   ❌ Удалить: `/unmonitor {mon.get('id')}`\n\n"
        )
        
    await message.answer(response, parse_mode="Markdown")

@router.message(Command("unmonitor"))
async def cmd_remove_monitor(message: Message):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    args = message.text.split()
    if len(args) < 2:
        await message.answer("⚠️ Укажите ID задачи. Пример: `/unmonitor 1`", parse_mode="Markdown")
        return

    try:
        monitor_id = int(args[1])
    except ValueError:
        await message.answer("⚠️ ID должен быть числом.", parse_mode="Markdown")
        return

    db = get_db()
    success = await db.delete_monitor(monitor_id, message.from_user.id)
    
    if success:
        await message.answer(f"✅ Задача мониторинга *ID {monitor_id}* успешно удалена.", parse_mode="Markdown")
    else:
        await message.answer(f"❌ Задача с *ID {monitor_id}* не найдена или принадлежит не вам.", parse_mode="Markdown")

# Callback handler for direct monitoring creation from search keyboard
@router.callback_query(F.data.startswith("addmon:"))
async def process_add_monitor_callback(callback: CallbackQuery):
    if not is_authorized(callback.from_user.id):
        await callback.answer("❌ У вас нет доступа.", show_alert=True)
        return

    data_parts = callback.data.split(":")
    query = data_parts[1]
    price_min = int(data_parts[2]) if len(data_parts) > 2 and data_parts[2] else None
    price_max = int(data_parts[3]) if len(data_parts) > 3 and data_parts[3] else None

    db = get_db()
    monitor_id = await db.add_monitor(
        user_id=callback.from_user.id,
        keyword=query,
        price_min=price_min,
        price_max=price_max,
        interval_sec=300
    )

    await callback.answer(f"🔔 Задача добавлена в мониторинг! ID: {monitor_id}", show_alert=True)
