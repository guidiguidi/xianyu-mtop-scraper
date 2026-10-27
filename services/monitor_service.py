import asyncio
from aiogram import Bot
from loguru import logger
import time
from database import Database
from config import settings
from goofish_client import GoofishSearchClient
from services.parser import parse_search_response

class MonitorService:
    def __init__(self, bot: Bot, db_path: str):
        self.bot = bot
        self.db = Database(db_path)
        self.is_running = False

    async def start(self):
        """Starts the infinite background monitoring cycle."""
        logger.info("Starting Goofish background monitor service...")
        self.is_running = True
        
        while self.is_running:
            try:
                # Fetch active monitors
                monitors = await self.get_active_monitors()
                if not monitors:
                    logger.debug("No active monitors found. Sleeping for 30 seconds...")
                    await asyncio.sleep(30)
                    continue

                logger.info(f"Running monitoring cycle for {len(monitors)} active tasks...")
                
                # Fetch cookies from DB
                active_cookies = await self.db.get_cookies(settings.XIANYU_COOKIES)
                
                # Run search check for each keyword monitor
                for mon in monitors:
                    if not self.is_running:
                        break
                        
                    await self.check_monitor_task(mon, active_cookies)
                    
                    # Sleep 5 seconds between search tasks to avoid Taobao anti-scraping triggers
                    await asyncio.sleep(5)
                    
            except Exception as e:
                logger.error(f"Error in monitor cycle: {e}")
                
            # Sleep 60 seconds before executing next check cycle
            await asyncio.sleep(60)

    async def get_active_monitors(self):
        try:
            return await self.db.get_active_monitors()
        except Exception as e:
            logger.error(f"Failed to fetch monitors from DB: {e}")
            return []

    async def check_monitor_task(self, monitor: dict, cookies_str: str):
        """Executes a single monitoring query and notifies user on new items."""
        monitor_id = monitor["id"]
        user_id = monitor["user_id"]
        keyword = monitor["keyword"]
        price_min = monitor["price_min"]
        price_max = monitor["price_max"]

        logger.debug(f"Checking monitor ID {monitor_id} for keyword: '{keyword}'")

        async def persist_refreshed_cookies(new_cookies_str: str):
            await self.db.save_cookies(new_cookies_str)
            logger.info("Auto-refreshed monitor cookies persisted successfully.")

        client = GoofishSearchClient(
            cookies_str=cookies_str,
            proxy_url=settings.PROXY_URL,
            on_cookies_refreshed=persist_refreshed_cookies
        )

        try:
            # Query Goofish H5 search
            raw_response = await client.search(
                keyword=keyword,
                page=1,
                price_min=price_min,
                price_max=price_max,
                rows_per_page=15 # Pull top 15 matches to inspect
            )
                
            items = parse_search_response(raw_response)
            
            if not items:
                logger.debug(f"No results found for monitor '{keyword}'.")
                return

            new_items_count = 0
            for item in reversed(items): # Process from oldest to newest
                seen = await self.db.is_item_seen(item.item_id, monitor_id)
                if not seen:
                    # Save to DB cache
                    await self.db.add_seen_item(item.item_id, monitor_id)
                    new_items_count += 1
                    
                    # Dispatch Telegram alert message
                    await self.send_item_notification(user_id, keyword, item)
                    
                    # Small delay between sending message blocks to prevent Telegram rate limit
                    await asyncio.sleep(1)

            if new_items_count > 0:
                logger.info(f"Monitor ID {monitor_id} ('{keyword}'): Dispatched {new_items_count} new item alerts.")

        except Exception as e:
            logger.error(f"Failed checking monitor task {monitor_id}: {e}")

    async def send_item_notification(self, user_id: int, keyword: str, item):
        """Builds and sends a premium product listing card message."""
        import html
        from services.translation_service import translator

        # Translate Chinese title and region to Russian
        t_title = await translator.translate(item.title, source_lang="ZH", target_lang="RU")
        t_area = await translator.translate(item.area, source_lang="ZH", target_lang="RU") if item.area else ""

        escaped_keyword = html.escape(keyword)
        escaped_title = html.escape(t_title[:75])
        escaped_price = html.escape(str(item.price))
        escaped_area = html.escape(t_area) if t_area else "Unspecified"
        escaped_nickname = html.escape(item.user_nickname) if item.user_nickname else "Unspecified"
        escaped_want = html.escape(item.want_count) if item.want_count else "New listing"

        caption = (
            f"🔔 <b>New Listing Alert! (Monitor: {escaped_keyword})</b>\n\n"
            f"🐟 <b>{escaped_title}...</b>\n\n"
            f"💰 Price: <code>{escaped_price}</code>\n"
            f"📍 Region: {escaped_area}\n"
            f"👤 Seller: {escaped_nickname}\n"
            f"📦 {escaped_want}\n\n"
            f'<a href="{item.share_url}">🔗 View on Xianyu</a>'
        )
        
        pic = item.pic_url if item.pic_url else "https://img.alicdn.com/tps/i4/TB1OKd7JXXXXXb1XFXXc8s9FXXX-400-400.png"
        
        try:
            await self.bot.send_photo(
                chat_id=user_id,
                photo=pic,
                caption=caption,
                parse_mode="HTML"
            )
        except Exception as e:
            logger.error(f"Failed to dispatch Telegram photo message to {user_id}: {e}")
            # Try plain text fallback if image transmission fails
            try:
                await self.bot.send_message(
                    chat_id=user_id,
                    text=caption,
                    parse_mode="HTML",
                    disable_web_page_preview=False
                )
            except Exception as ex:
                logger.error(f"Text fallback message also failed: {ex}")

    def stop(self):
        self.is_running = False
        logger.info("Goofish background monitor service stopped.")
