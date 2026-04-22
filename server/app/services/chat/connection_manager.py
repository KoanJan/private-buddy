"""
Connection manager module for SSE (Server-Sent Events) streaming.

This module manages real-time connections between the server and clients
for streaming LLM responses. It uses asyncio queues to enable multiple
clients to connect to the same session.

The connection manager supports:
- Multiple concurrent connections per session
- Message broadcasting to all connected clients
- Clean disconnection handling
"""

import asyncio
from typing import Dict, List
from app.logger import logger


class ConnectionManager:
    """
    Manager for SSE connections to enable real-time streaming.
    
    This class manages active connections using asyncio queues,
    allowing multiple clients to receive streaming updates for
    the same session simultaneously.
    
    Attributes:
        active_connections: Dictionary mapping session IDs to lists of client queues
    """
    
    def __init__(self):
        """Initialize the connection manager with empty connection map."""
        self.active_connections: Dict[int, List[asyncio.Queue]] = {}
    
    async def connect(self, session_id: int) -> asyncio.Queue:
        """
        Register a new client connection for a session.
        
        Creates a new queue for the client and adds it to the
        session's connection list.
        
        Args:
            session_id: Session ID to connect to
            
        Returns:
            asyncio.Queue for the client to receive messages
        """
        queue = asyncio.Queue()
        if session_id not in self.active_connections:
            self.active_connections[session_id] = []
        self.active_connections[session_id].append(queue)
        logger.info(f"Client connected to session {session_id}, total connections: {len(self.active_connections[session_id])}")
        return queue
    
    async def disconnect(self, session_id: int, queue: asyncio.Queue):
        """
        Remove a client connection from a session.
        
        Cleans up the connection and removes the session entry
        if no connections remain.
        
        Args:
            session_id: Session ID to disconnect from
            queue: The client's queue to remove
        """
        if session_id in self.active_connections:
            if queue in self.active_connections[session_id]:
                self.active_connections[session_id].remove(queue)
                logger.info(f"Client disconnected from session {session_id}, remaining connections: {len(self.active_connections[session_id])}")
            # Clean up empty session entries
            if not self.active_connections[session_id]:
                del self.active_connections[session_id]
                logger.info(f"No more connections for session {session_id}")
    
    async def notify(self, session_id: int, message: dict):
        """
        Broadcast a message to all clients connected to a session.
        
        This method is called during LLM streaming to push chunks
        and completion notifications to all connected clients.
        
        Args:
            session_id: Session ID to broadcast to
            message: Message dictionary to send (contains 'type' and optional 'content')
        """
        if session_id in self.active_connections:
            logger.debug(f"Notifying {len(self.active_connections[session_id])} clients for session {session_id}")
            for queue in self.active_connections[session_id]:
                try:
                    await queue.put(message)
                except Exception as e:
                    logger.error(f"Failed to notify client: {e}")
    
    def has_connections(self, session_id: int) -> bool:
        """
        Check if a session has any active connections.
        
        Args:
            session_id: Session ID to check
            
        Returns:
            True if there are active connections, False otherwise
        """
        return session_id in self.active_connections and len(self.active_connections[session_id]) > 0


# Global connection manager instance
manager = ConnectionManager()
