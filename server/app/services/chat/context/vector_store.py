"""
Vector store service using sqlite-vec for local vector storage and search.

Replaces ChromaDB with sqlite-vec for lighter native extension footprint
(0.16 MB vs 126.8 MB) and unified SQLite storage. Uses sqlite-vec's
native Python API directly instead of LangChain's SQLiteVec wrapper,
because the wrapper lacks delete/update support.

Each session gets its own vec0 virtual table in the shared vector database file.
Table naming convention: session_vec_{session_id}
"""

import sqlite3
from typing import List, Optional, Dict, Any

import sqlite_vec
from sqlite_vec import serialize_float32

from langchain_core.embeddings import Embeddings
from app.models.embedding_config import EmbeddingConfig
from app.services.llm.embedding import EmbeddingService
from app.config import get_settings
from app.logger import logger


class CustomEmbeddings(Embeddings):
    """Embeddings adapter that delegates to the project's EmbeddingService."""

    def __init__(self, embedding_config: EmbeddingConfig):
        self.embedding_config = embedding_config

    def embed_documents(self, texts: List[str]) -> List[List[float]]:
        return EmbeddingService.embed_texts_sync(self.embedding_config, texts)

    def embed_query(self, text: str) -> List[float]:
        return EmbeddingService.embed_query_sync(self.embedding_config, text)


class VectorStoreService:
    """
    Vector store backed by sqlite-vec.

    Uses a single SQLite database file for all sessions. Each session
    gets a vec0 virtual table named session_vec_{session_id}. The table
    stores the embedding vector plus metadata (role, message_id) as
    auxiliary columns.

    The embedding dimension is determined at first insert time from the
    embedding config's model output.
    """

    _instances: Dict[int, "VectorStoreService"] = {}

    def __init__(self, session_id: int, embedding_config: EmbeddingConfig):
        self.session_id = session_id
        self.embedding_config = embedding_config
        self.embedding_func = CustomEmbeddings(embedding_config)
        settings = get_settings()
        self.db_file = settings.vector_db_file
        self.table_name = f"session_vec_{session_id}"
        self._connection: Optional[sqlite3.Connection] = None
        self._dimension: Optional[int] = None
        self._table_created = False

    @classmethod
    def get_instance(cls, session_id: int, embedding_config: EmbeddingConfig) -> "VectorStoreService":
        if session_id not in cls._instances:
            cls._instances[session_id] = cls(session_id, embedding_config)
        return cls._instances[session_id]

    def _get_connection(self) -> sqlite3.Connection:
        if self._connection is None:
            import os
            os.makedirs(os.path.dirname(self.db_file), exist_ok=True)
            self._connection = sqlite3.connect(self.db_file)
            self._connection.enable_load_extension(True)
            sqlite_vec.load(self._connection)
            self._connection.enable_load_extension(False)
        return self._connection

    def _ensure_table(self, dimension: int) -> None:
        if self._table_created and self._dimension == dimension:
            return

        conn = self._get_connection()
        cursor = conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name=?",
            (self.table_name,)
        )
        if cursor.fetchone():
            self._table_created = True
            self._dimension = dimension
            return

        conn.execute(f"""
            CREATE VIRTUAL TABLE IF NOT EXISTS {self.table_name} USING vec0(
                embedding float[{dimension}] distance_metric=cosine,
                +content text,
                +role text,
                +message_id integer
            )
        """)
        conn.commit()
        self._table_created = True
        self._dimension = dimension
        logger.info(f"Created vec0 table {self.table_name} with dimension {dimension}")

    def add_messages(
        self,
        message_ids: List[int],
        contents: List[str],
        metadatas: Optional[List[Dict[str, Any]]] = None
    ) -> None:
        if not message_ids:
            return

        embeddings = self.embedding_func.embed_documents(contents)
        if not embeddings:
            return

        dimension = len(embeddings[0])
        self._ensure_table(dimension)

        conn = self._get_connection()
        for i, (mid, embedding, content) in enumerate(zip(message_ids, embeddings, contents)):
            role = ""
            if metadatas and i < len(metadatas):
                role = metadatas[i].get("role", "")

            conn.execute(
                f"INSERT INTO {self.table_name}(rowid, embedding, content, role, message_id) VALUES (?, ?, ?, ?, ?)",
                (mid, serialize_float32(embedding), content, role, mid)
            )
        conn.commit()
        logger.info(f"Added {len(message_ids)} messages to vector store for session {self.session_id}")

    def search(
        self,
        query: str,
        k: int = 5,
        filter_metadata: Optional[Dict[str, Any]] = None
    ) -> List[Dict[str, Any]]:
        query_embedding = self.embedding_func.embed_query(query)
        if not query_embedding:
            return []

        dimension = len(query_embedding)
        self._ensure_table(dimension)

        conn = self._get_connection()
        query_vec = serialize_float32(query_embedding)

        results = conn.execute(
            f"""
            SELECT rowid, distance, content, role, message_id
            FROM {self.table_name}
            WHERE embedding MATCH ?
            AND k = ?
            ORDER BY distance
            """,
            [query_vec, k]
        ).fetchall()

        formatted_results = []
        for rowid, distance, content, role, message_id in results:
            formatted_results.append({
                "content": content or "",
                "metadata": {
                    "message_id": message_id,
                    "role": role,
                    "distance": distance
                }
            })

        logger.info(f"Found {len(formatted_results)} results for query in session {self.session_id}")
        return formatted_results

    def search_with_scores(
        self,
        query: str,
        k: int = 5,
        filter_metadata: Optional[Dict[str, Any]] = None
    ) -> List[tuple[Dict[str, Any], float]]:
        query_embedding = self.embedding_func.embed_query(query)
        if not query_embedding:
            return []

        dimension = len(query_embedding)
        self._ensure_table(dimension)

        conn = self._get_connection()
        query_vec = serialize_float32(query_embedding)

        results = conn.execute(
            f"""
            SELECT rowid, distance, content, role, message_id
            FROM {self.table_name}
            WHERE embedding MATCH ?
            AND k = ?
            ORDER BY distance
            """,
            [query_vec, k]
        ).fetchall()

        formatted_results = []
        for rowid, distance, content, role, message_id in results:
            formatted_results.append(({
                "content": content or "",
                "metadata": {
                    "message_id": message_id,
                    "role": role
                }
            }, distance))

        logger.info(f"Found {len(formatted_results)} results with scores for session {self.session_id}")
        return formatted_results

    def delete_messages(self, message_ids: List[int]) -> None:
        if not message_ids:
            return

        conn = self._get_connection()
        for mid in message_ids:
            conn.execute(f"DELETE FROM {self.table_name} WHERE rowid = ?", (mid,))
        conn.commit()
        logger.info(f"Deleted {len(message_ids)} messages from vector store for session {self.session_id}")

    def clear(self) -> None:
        conn = self._get_connection()
        conn.execute(f"DROP TABLE IF EXISTS {self.table_name}")
        conn.commit()
        self._table_created = False
        self._dimension = None
        logger.info(f"Cleared vector store for session {self.session_id}")

    def get_message_count(self) -> int:
        conn = self._get_connection()
        cursor = conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name=?",
            (self.table_name,)
        )
        if not cursor.fetchone():
            return 0
        count: int = conn.execute(f"SELECT count(*) FROM {self.table_name}").fetchone()[0]
        return count
