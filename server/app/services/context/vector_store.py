from typing import List, Optional, Dict, Any
from langchain_community.vectorstores import Chroma
from langchain_core.embeddings import Embeddings
from app.models.embedding_config import EmbeddingConfig
from app.services.llm.embedding import EmbeddingService
from app.config import get_settings
from app.logger import logger
import os


class CustomEmbeddings(Embeddings):
    def __init__(self, embedding_config: EmbeddingConfig):
        self.embedding_config = embedding_config

    def embed_documents(self, texts: List[str]) -> List[List[float]]:
        return EmbeddingService.embed_texts_sync(self.embedding_config, texts)

    def embed_query(self, text: str) -> List[float]:
        return EmbeddingService.embed_query_sync(self.embedding_config, text)


class VectorStoreService:
    _instances: Dict[int, "VectorStoreService"] = {}

    def __init__(self, session_id: int, embedding_config: EmbeddingConfig):
        self.session_id = session_id
        self.embedding_config = embedding_config
        self.embedding_func = CustomEmbeddings(embedding_config)
        settings = get_settings()
        self.persist_dir = os.path.join(settings.chroma_persist_dir, f"session_{session_id}")
        self.collection_name = f"session_{session_id}"
        self._vectorstore: Optional[Chroma] = None

    @classmethod
    def get_instance(cls, session_id: int, embedding_config: EmbeddingConfig) -> "VectorStoreService":
        if session_id not in cls._instances:
            cls._instances[session_id] = cls(session_id, embedding_config)
        return cls._instances[session_id]

    @property
    def vectorstore(self) -> Chroma:
        if self._vectorstore is None:
            os.makedirs(self.persist_dir, exist_ok=True)
            self._vectorstore = Chroma(
                collection_name=self.collection_name,
                embedding_function=self.embedding_func,
                persist_directory=self.persist_dir
            )
        return self._vectorstore

    def add_messages(
        self,
        message_ids: List[int],
        contents: List[str],
        metadatas: Optional[List[Dict[str, Any]]] = None
    ) -> None:
        if not message_ids:
            return

        if metadatas is None:
            metadatas = [{"message_id": mid} for mid in message_ids]
        else:
            for i, mid in enumerate(message_ids):
                metadatas[i]["message_id"] = mid

        self.vectorstore.add_texts(
            texts=contents,
            metadatas=metadatas,
            ids=[str(mid) for mid in message_ids]
        )
        logger.info(f"Added {len(message_ids)} messages to vector store for session {self.session_id}")

    def search(
        self,
        query: str,
        k: int = 5,
        filter_metadata: Optional[Dict[str, Any]] = None
    ) -> List[Dict[str, Any]]:
        results = self.vectorstore.similarity_search(
            query,
            k=k,
            filter=filter_metadata
        )

        formatted_results = []
        for doc in results:
            formatted_results.append({
                "content": doc.page_content,
                "metadata": doc.metadata
            })

        logger.info(f"Found {len(formatted_results)} results for query in session {self.session_id}")
        return formatted_results

    def search_with_scores(
        self,
        query: str,
        k: int = 5,
        filter_metadata: Optional[Dict[str, Any]] = None
    ) -> List[tuple[Dict[str, Any], float]]:
        results = self.vectorstore.similarity_search_with_score(
            query,
            k=k,
            filter=filter_metadata
        )

        formatted_results = []
        for doc, score in results:
            formatted_results.append(({
                "content": doc.page_content,
                "metadata": doc.metadata
            }, score))

        logger.info(f"Found {len(formatted_results)} results with scores for query in session {self.session_id}")
        return formatted_results

    def delete_messages(self, message_ids: List[int]) -> None:
        ids = [str(mid) for mid in message_ids]
        self.vectorstore._collection.delete(ids=ids)
        logger.info(f"Deleted {len(message_ids)} messages from vector store for session {self.session_id}")

    def clear(self) -> None:
        self.vectorstore.delete_collection()
        self._vectorstore = None
        logger.info(f"Cleared vector store for session {self.session_id}")

    def get_message_count(self) -> int:
        return self.vectorstore._collection.count()
