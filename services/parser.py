import time
from datetime import datetime
from typing import Dict, Any, List, Optional
from loguru import logger


class GoofishItem:
    """
    Represents a clean, structured product item from Goofish/Xianyu.
    Includes publish time, freshness score, verification status, and fishTags.
    """

    def __init__(
        self,
        item_id: str,
        title: str,
        price: str,
        pic_url: str,
        user_nickname: str,
        user_avatar: str,
        area: str,
        want_count: str,
        target_url: str,
        publish_ts: Optional[int] = None,       # Unix timestamp in ms
        relative_time: str = "",                 # e.g. "8小时前发布" → "8h ago"
        is_verified: bool = False,               # 验货宝 platform verification badge
        is_new_item: bool = False,               # 全新 — brand new condition tag
        age_minutes: Optional[int] = None,       # How many minutes ago published
    ):
        self.item_id = item_id
        self.title = title
        self.price = price
        self.pic_url = pic_url
        self.user_nickname = user_nickname
        self.user_avatar = user_avatar
        self.area = area
        self.want_count = want_count
        self.target_url = target_url
        self.publish_ts = publish_ts
        self.relative_time = relative_time
        self.is_verified = is_verified
        self.is_new_item = is_new_item
        self.age_minutes = age_minutes

    @property
    def share_url(self) -> str:
        """Standard Goofish web URL for the item."""
        return f"https://www.goofish.com/item?id={self.item_id}"

    @property
    def freshness_label(self) -> str:
        """Human-readable freshness label based on age in minutes."""
        if self.age_minutes is None:
            return ""
        if self.age_minutes < 10:
            return "🔥 Just now"
        if self.age_minutes < 60:
            return f"🟢 {self.age_minutes} min ago"
        if self.age_minutes < 1440:
            hours = self.age_minutes // 60
            return f"🟡 {hours} hr ago"
        days = self.age_minutes // 1440
        return f"⚪️ {days} d ago"

    def to_dict(self) -> Dict[str, Any]:
        return {
            "item_id": self.item_id,
            "title": self.title,
            "price": self.price,
            "pic_url": self.pic_url,
            "user_nickname": self.user_nickname,
            "user_avatar": self.user_avatar,
            "area": self.area,
            "want_count": self.want_count,
            "target_url": self.target_url,
            "share_url": self.share_url,
            "publish_ts": self.publish_ts,
            "relative_time": self.relative_time,
            "is_verified": self.is_verified,
            "is_new_item": self.is_new_item,
            "age_minutes": self.age_minutes,
            "freshness_label": self.freshness_label,
        }


def _fix_url(url: str) -> str:
    """Ensures URL has a proper scheme."""
    if not url:
        return ""
    if url.startswith("//"):
        return "https:" + url
    return url


def _parse_publish_time(click_param: dict) -> Optional[int]:
    """
    Extracts publishTime (ms) from clickParam.args.
    Path: item -> main -> clickParam -> args -> publishTime
    """
    try:
        args = click_param.get("args", {})
        ts_raw = args.get("publishTime")
        if ts_raw:
            return int(ts_raw)
    except Exception:
        pass
    return None


def _calc_age_minutes(publish_ts_ms: Optional[int]) -> Optional[int]:
    """Calculates how many minutes ago the item was published."""
    if not publish_ts_ms:
        return None
    now_ms = int(time.time() * 1000)
    delta_ms = now_ms - publish_ts_ms
    if delta_ms < 0:
        return 0
    return delta_ms // 60000


def _parse_fish_tags(ex_content: dict) -> tuple[bool, bool, str]:
    """
    Parses fishTags for:
    - is_verified: 验货宝 (Platform Inspection tag)
    - is_new_item: 全新 (Brand new condition)
    - relative_time: human time text (e.g. "8小时前发布")

    Searches both r1, r2, r3 tag groups.
    Returns (is_verified, is_new_item, relative_time)
    """
    is_verified = False
    is_new_item = False
    relative_time = ""

    fish_tags = ex_content.get("fishTags") or {}

    # Collect all tagLists from r1, r2, r3 ...
    all_tags = []
    for group_key in ["r1", "r2", "r3", "r4"]:
        group = fish_tags.get(group_key, {})
        tag_list = group.get("tagList", [])
        if isinstance(tag_list, list):
            all_tags.extend(tag_list)

    for tag in all_tags:
        if not isinstance(tag, dict):
            continue
        content = tag.get("content", "") or ""
        label_id = str(tag.get("labelId", ""))

        # Platform verification badge
        if "验货宝" in content or label_id in ("5", "6"):
            is_verified = True

        # Brand new condition tag
        if "全新" in content:
            is_new_item = True

        # Relative publish time (labelId "752" or text containing 前发布/小时前/分钟前)
        if (label_id == "752" or "发布" in content or "小时前" in content or "分钟前" in content or "天前" in content):
            if content and not relative_time:
                relative_time = content

    return is_verified, is_new_item, relative_time


def _parse_price(ex_content: dict) -> str:
    """
    Extracts price string. Tries multiple fallback paths:
    1. exContent -> detailParams -> soldPrice
    2. exContent -> price (array of {text: ...})
    3. exContent -> price (plain string)
    """
    # Primary: detailParams.soldPrice
    detail_params = ex_content.get("detailParams") or {}
    sold_price = detail_params.get("soldPrice") or detail_params.get("price") or ""
    if sold_price:
        return f"¥{sold_price}"

    # Fallback: price as array of text nodes
    price_components = ex_content.get("price") or []
    if isinstance(price_components, list) and price_components:
        joined = "".join(
            p.get("text", "") for p in price_components if isinstance(p, dict)
        )
        if joined:
            return joined

    # Fallback: plain string
    if isinstance(price_components, str) and price_components:
        return price_components

    return "¥?"


def _parse_title(ex_content: dict) -> str:
    """
    Extracts title. Tries:
    1. exContent -> detailParams -> title
    2. exContent -> title (plain string)
    """
    detail_params = ex_content.get("detailParams") or {}
    title = detail_params.get("title") or ex_content.get("title") or "Untitled"
    return title.strip()


def _parse_nickname(ex_content: dict) -> str:
    """
    Extracts seller nickname. Tries:
    1. exContent -> detailParams -> userNick
    2. exContent -> userNickName
    """
    detail_params = ex_content.get("detailParams") or {}
    nick = (
        detail_params.get("userNick")
        or ex_content.get("userNickName")
        or "Seller"
    )
    return nick


def parse_search_response(response_json: Dict[str, Any]) -> List[GoofishItem]:
    """
    Parses a raw Goofish MTOP API JSON response into a list of GoofishItem objects.

    Correct JSON structure:
        response_json
        └── data
            └── resultList[]          ← main loop
                └── data
                    └── item          ← item node
                        ├── itemId
                        ├── main
                        │   ├── clickParam -> args -> publishTime
                        │   └── targetUrl
                        └── exContent
                            ├── picUrl
                            ├── area
                            ├── want
                            ├── detailParams
                            │   ├── title
                            │   ├── soldPrice
                            │   └── userNick
                            └── fishTags
                                ├── r1 -> tagList[]
                                └── r2 -> tagList[]
    """
    parsed_items: List[GoofishItem] = []

    try:
        data_node = response_json.get("data") or {}
        result_list = data_node.get("resultList") or []

        for element in result_list:
            try:
                # Only process real item blocks (type == "DX")
                if element.get("type") not in ("DX", None, ""):
                    continue

                item_node = (element.get("data") or {}).get("item") or {}
                if not item_node:
                    continue

                main_node = item_node.get("main") or {}
                # exContent moved from itemNode level into mainNode in recent API version
                ex_content = main_node.get("exContent") or item_node.get("exContent") or {}

                # ── Item ID ───────────────────────────────────────────────
                item_id = str(item_node.get("itemId") or "")
                if not item_id:
                    # Fallback from clickParam.args.id
                    click_args = (main_node.get("clickParam") or {}).get("args") or {}
                    item_id = str(click_args.get("id") or "")
                if not item_id:
                    logger.debug("Skipping element with no itemId.")
                    continue

                # ── Title ─────────────────────────────────────────────────
                title = _parse_title(ex_content)

                # ── Price ─────────────────────────────────────────────────
                price_str = _parse_price(ex_content)

                # ── Image URL ─────────────────────────────────────────────
                pic_url = _fix_url(ex_content.get("picUrl") or "")

                # ── Seller Info ───────────────────────────────────────────
                user_nickname = _parse_nickname(ex_content)
                user_avatar = _fix_url(ex_content.get("userAvatarUrl") or "")

                # ── Area / Region ─────────────────────────────────────────
                area = ex_content.get("area") or ""

                # ── Want count ────────────────────────────────────────────
                want_count = ex_content.get("want") or ""

                # ── Target mobile URL ─────────────────────────────────────
                target_url = main_node.get("targetUrl") or ""

                # ── Publish Time ──────────────────────────────────────────
                click_param = main_node.get("clickParam") or {}
                publish_ts = _parse_publish_time(click_param)
                age_minutes = _calc_age_minutes(publish_ts)

                # ── Fish Tags ─────────────────────────────────────────────
                is_verified, is_new_item, relative_time = _parse_fish_tags(ex_content)

                item_obj = GoofishItem(
                    item_id=item_id,
                    title=title,
                    price=price_str,
                    pic_url=pic_url,
                    user_nickname=user_nickname,
                    user_avatar=user_avatar,
                    area=area,
                    want_count=want_count,
                    target_url=target_url,
                    publish_ts=publish_ts,
                    relative_time=relative_time,
                    is_verified=is_verified,
                    is_new_item=is_new_item,
                    age_minutes=age_minutes,
                )
                parsed_items.append(item_obj)

            except Exception as item_err:
                logger.warning(f"Skipped one item due to parse error: {item_err}")
                continue

    except Exception as e:
        logger.error(f"Critical error parsing search response: {e}")

    logger.info(f"Parser extracted {len(parsed_items)} items from response.")
    return parsed_items
