import asyncio
from typing import Dict, List
from app.logger import logger


class ConnectionManager:
    def __init__(self):
        self.active_connections: Dict[int, List[asyncio.Queue]] = {}
    
    async def connect(self, session_id: int) -> asyncio.Queue:
        queue = asyncio.Queue()
        if session_id not in self.active_connections:
            self.active_connections[session_id] = []
        self.active_connections[session_id].append(queue)
        logger.info(f"Client connected to session {session_id}, total connections: {len(self.active_connections[session_id])}")
        return queue
    
    async def disconnect(self, session_id: int, queue: asyncio.Queue):
        if session_id in self.active_connections:
            if queue in self.active_connections[session_id]:
                self.active_connections[session_id].remove(queue)
                logger.info(f"Client disconnected from session {session_id}, remaining connections: {len(self.active_connections[session_id])}")
            if not self.active_connections[session_id]:
                del self.active_connections[session_id]
                logger.info(f"No more connections for session {session_id}")
    
    async def notify(self, session_id: int, message: dict):
        if session_id in self.active_connections:
            logger.debug(f"Notifying {len(self.active_connections[session_id])} clients for session {session_id}")
            for queue in self.active_connections[session_id]:
                try:
                    await queue.put(message)
                except Exception as e:
                    logger.error(f"Failed to notify client: {e}")
    
    def has_connections(self, session_id: int) -> bool:
        return session_id in self.active_connections and len(self.active_connections[session_id]) > 0


manager = ConnectionManager()