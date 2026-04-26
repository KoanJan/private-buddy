"""
Application-wide logging configuration.

Reads LOG_LEVEL from environment (defaults to INFO) and configures
both file and stream handlers. Uses force=True to ensure the
configuration takes effect even if basicConfig was called earlier.
"""

import logging
import os
from datetime import datetime
from dotenv import load_dotenv

load_dotenv()

log_dir = "logs"
if not os.path.exists(log_dir):
    os.makedirs(log_dir)

log_file = os.path.join(log_dir, f"app_{datetime.now().strftime('%Y%m%d')}.log")

log_level = os.getenv("LOG_LEVEL", "INFO").upper()

logging.basicConfig(
    level=getattr(logging, log_level, logging.INFO),
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    handlers=[
        logging.FileHandler(log_file, encoding="utf-8"),
        logging.StreamHandler(),
    ],
    force=True,
)

logger = logging.getLogger(__name__)
