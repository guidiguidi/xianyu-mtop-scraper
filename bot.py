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
        await message.answer("❌ Access denied.")
        return

    welcome_text = (
        "🐟 *Welcome to Xianyu / Goofish Search & Monitor Bot!*\n\n"
        "Automated assistant for searching Goofish / Xianyu marketplace and receiving real-time alerts.\n\n"
        "📖 *Core Commands:*\n"
        "• `/search <query>` - Interactive product search\n"
        "• `/monitor <query> [<min_price> <max_price>]` - Monitor new listings\n"
        "• `/monitors` - List active monitor tasks\n"
        "• `/unmonitor <ID>` - Remove monitor task\n"
        "• `/setcookies` - Update session cookies\n"
        "• `/status` - Check proxy and session token status\n\n"
        "ℹ️ Use /help for detailed instructions"
    )
    await message.answer(welcome_text, parse_mode="Markdown")

@dp.message(Command("help"))
async def cmd_help(message: Message):
    if not is_authorized(message.from_user.id):
        return

    help_text = (
        "📖 *Help & Usage Guide*\n\n"
        "🔍 *How to Search Listings?*\n"
        "Use `/search <query>`. The bot returns structured item cards with images and action buttons.\n\n"
        "🔔 *How Does Monitoring Work?*\n"
        "Subscribe to keywords. Background worker polls Goofish and notifies on new items.\n\n"
        "🍪 *Session Authentication:*\n"
        "Goofish H5 API requires MTOP signatures. Use /setcookies to renew session tokens.\n\n"
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
