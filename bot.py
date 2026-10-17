import asyncio
from aiogram import Bot, Dispatcher
from aiogram.types import Message
from aiogram.filters import CommandStart, Command
from loguru import logger
import sys
import os

# Ensure project root is in python path
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from config import settings
from database import Database
from handlers.search import router as search_router, is_authorized
from handlers.monitor import router as monitor_router
from handlers.settings import router as settings_router
from services.monitor_service import MonitorService

# Setup logger configuration
logger.remove()
logger.add(
    sys.stdout, 
    format="<green>{time:YYYY-MM-DD HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - <level>{message}</level>",
    level="INFO"
)
logger.add(
    "xianyu_bot.log",
    rotation="10 MB",
    retention="7 days",
    level="DEBUG",
    encoding="utf-8"
)

async def init_database():
    db = Database(settings.DATABASE_PATH)
    await db.initialize()
    logger.info("Database schema initialized and verified.")

dp = Dispatcher()

# Include routers
dp.include_router(search_router)
dp.include_router(monitor_router)
dp.include_router(settings_router)

@dp.message(CommandStart())
async def cmd_start(message: Message):
    if not is_authorized(message.from_user.id):
        await message.answer("❌ У вас нет доступа к этому боту.")
        return

    welcome_text = (
        "🐟 *Привет! Добро пожаловать в Xianyu/Goofish Search & Monitor Bot!*\n\n"
        "Я помогу вам искать товары на китайской барахолке Goofish и получать моментальные оповещения о новых предложениях.\n\n"
        "📖 *Основные Команды:*\n"
        "• `/search <запрос>` - Быстрый интерактивный поиск товаров\n"
        "• `/monitor <запрос> [<мин_цена> <макс_цена>]` - Начать следить за новыми лотами\n"
        "• `/monitors` - Показать активные задачи мониторинга\n"
        "• `/unmonitor <ID>` - Удалить задачу мониторинга\n"
        "• `/setcookies` - Обновить H5 куки из Antidetect браузера\n"
        "• `/status` - Проверить статус токенов и прокси\n\n"
        "ℹ️ Для вызова справки используйте: /help"
    )
    await message.answer(welcome_text, parse_mode="Markdown")

@dp.message(Command("help"))
async def cmd_help(message: Message):
    if not is_authorized(message.from_user.id):
        return

    help_text = (
        "📖 *Справка по использованию бота*\n\n"
        "🔍 *Как искать товары?*\n"
        "Используйте команду `/search rickowens`. Бот вернет карточки товаров с фотографиями, ценами в юанях и прямой ссылкой на покупку. Кнопки позволят перелистывать страницы или мгновенно отфильтровать товары по цене.\n\n"
        "🔔 *Как работает мониторинг?*\n"
        "Вы можете подписаться на отслеживание новых товаров. Бот опрашивает Goofish раз в 5 минут, сравнивает результаты и присылает фото-уведомление, если на рынке появился новый лот. Пример: `/monitor rickowens 5000 15000` (следить за Rick Owens от ¥5000 до ¥15000).\n\n"
        "🍪 *Что делать, если запросы не работают?*\n"
        "Goofish H5 API защищен подписью MTOP. Если сессия истекает, получите новые cookies в Antidetect браузере на сайте goofish.com и обновите их в боте с помощью `/setcookies`."
    )
    await message.answer(help_text, parse_mode="Markdown")

async def main():
    logger.info("Initializing Xianyu/Goofish Telegram Bot...")
    
    # Validate bot token configuration
    if "example_token" in settings.BOT_TOKEN or len(settings.BOT_TOKEN) < 15:
        logger.error("Invalid Telegram BOT_TOKEN config. Please configure it in your .env file!")
        sys.exit(1)

    # Initialize SQLite database
    await init_database()

    # Initialize bot client
    bot = Bot(token=settings.BOT_TOKEN)

    # Initialize and startup background polling service
    monitor_service = MonitorService(bot, settings.DATABASE_PATH)
    monitor_task = asyncio.create_task(monitor_service.start())

    try:
        logger.info("Bot is starting long polling. Welcome!")
        await dp.start_polling(bot, skip_updates=True)
    finally:
        logger.info("Shutting down bot services gracefully...")
        monitor_service.stop()
        await monitor_task
        await bot.session.close()
        logger.info("Shutdown sequence complete. Bye!")

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        logger.info("Bot manually terminated by KeyboardInterrupt.")
