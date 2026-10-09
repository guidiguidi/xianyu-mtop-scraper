import aiosqlite
from typing import List, Dict, Any, Optional, Tuple
from loguru import logger
import os

class Database:
    def __init__(self, db_path: str):
        self.db_path = db_path

    async def initialize(self):
        """Initializes tables for monitors, seen items, and active cookie states."""
        async with aiosqlite.connect(self.db_path) as db:
            # Table for keyword monitors
            await db.execute("""
                CREATE TABLE IF NOT EXISTS monitors (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    user_id INTEGER NOT NULL,
                    keyword TEXT NOT NULL,
                    price_min INTEGER,
                    price_max INTEGER,
                    interval_sec INTEGER DEFAULT 300,
                    is_active INTEGER DEFAULT 1,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """)
            
            # Table to track already notified items
            await db.execute("""
                CREATE TABLE IF NOT EXISTS seen_items (
                    item_id TEXT NOT NULL,
                    monitor_id INTEGER NOT NULL,
                    first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    PRIMARY KEY (item_id, monitor_id),
                    FOREIGN KEY (monitor_id) REFERENCES monitors (id) ON DELETE CASCADE
                )
            """)
            
            # Table for persisting updated cookies
            await db.execute("""
                CREATE TABLE IF NOT EXISTS system_config (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL
                )
            """)
            await db.commit()
        logger.info("Database initialized successfully.")

    # --- Cookies Management ---
    async def get_cookies(self, default_value: str = "") -> str:
        async with aiosqlite.connect(self.db_path) as db:
            async with db.execute("SELECT value FROM system_config WHERE key = 'cookies'") as cursor:
                row = await cursor.fetchone()
                return row[0] if row else default_value

    async def save_cookies(self, cookies_str: str):
        async with aiosqlite.connect(self.db_path) as db:
            await db.execute(
                "INSERT OR REPLACE INTO system_config (key, value) VALUES ('cookies', ?)",
                (cookies_str,)
            )
            await db.commit()

    # --- Monitors CRUD ---
    async def add_monitor(self, user_id: int, keyword: str, price_min: Optional[int], price_max: Optional[int], interval_sec: int = 300) -> int:
        async with aiosqlite.connect(self.db_path) as db:
            cursor = await db.execute(
                """INSERT INTO monitors (user_id, keyword, price_min, price_max, interval_sec) 
                   VALUES (?, ?, ?, ?, ?)""",
                (user_id, keyword, price_min, price_max, interval_sec)
            )
            await db.commit()
            return cursor.lastrowid

    async def get_active_monitors(self) -> List[Dict[str, Any]]:
        async with aiosqlite.connect(self.db_path) as db:
            db.row_factory = aiosqlite.Row
            async with db.execute("SELECT * FROM monitors WHERE is_active = 1") as cursor:
                rows = await cursor.fetchall()
                return [dict(row) for row in rows]

    async def get_user_monitors(self, user_id: int) -> List[Dict[str, Any]]:
        async with aiosqlite.connect(self.db_path) as db:
            db.row_factory = aiosqlite.Row
            async with db.execute("SELECT * FROM monitors WHERE user_id = ?", (user_id,)) as cursor:
                rows = await cursor.fetchall()
                return [dict(row) for row in rows]

    async def delete_monitor(self, monitor_id: int, user_id: int) -> bool:
        async with aiosqlite.connect(self.db_path) as db:
            cursor = await db.execute("DELETE FROM monitors WHERE id = ? AND user_id = ?", (monitor_id, user_id))
            await db.commit()
            return cursor.rowcount > 0

    # --- Seen Items Management ---
    async def is_item_seen(self, item_id: str, monitor_id: int) -> bool:
        async with aiosqlite.connect(self.db_path) as db:
            async with db.execute(
                "SELECT 1 FROM seen_items WHERE item_id = ? AND monitor_id = ?",
                (item_id, monitor_id)
            ) as cursor:
                row = await cursor.fetchone()
                return row is not None

    async def add_seen_item(self, item_id: str, monitor_id: int):
        async with aiosqlite.connect(self.db_path) as db:
            await db.execute(
                "INSERT OR IGNORE INTO seen_items (item_id, monitor_id) VALUES (?, ?)",
                (item_id, monitor_id)
            )
            await db.commit()

    async def clean_old_seen_items(self, days: int = 14):
        """Periodically runs to keep database clean from stale entries."""
        async with aiosqlite.connect(self.db_path) as db:
            await db.execute(
                "DELETE FROM seen_items WHERE first_seen < datetime('now', ?)",
                (f"-{days} days",)
            )
            await db.commit()
