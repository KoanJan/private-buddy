from typing import List
from openai import OpenAI
from app.models.embedding_config import EmbeddingConfig
from app.logger import logger


class EmbeddingService:
    @staticmethod
    def create_client(embedding_config: EmbeddingConfig) -> OpenAI:
        return OpenAI(
            api_key=embedding_config.api_key,
            base_url=embedding_config.base_url
        )

    @staticmethod
    def embed_texts_sync(
        embedding_config: EmbeddingConfig,
        texts: List[str]
    ) -> List[List[float]]:
        client = EmbeddingService.create_client(embedding_config)
        try:
            response = client.embeddings.create(
                model=embedding_config.model_id,
                input=texts
            )
            vectors = [item.embedding for item in response.data]
            logger.info(f"Embedded {len(texts)} texts successfully")
            return vectors
        except Exception as e:
            logger.error(f"Failed to embed texts: {str(e)}", exc_info=True)
            raise

    @staticmethod
    def embed_query_sync(
        embedding_config: EmbeddingConfig,
        query: str
    ) -> List[float]:
        client = EmbeddingService.create_client(embedding_config)
        try:
            response = client.embeddings.create(
                model=embedding_config.model_id,
                input=query
            )
            vector = response.data[0].embedding
            logger.info(f"Embedded query successfully")
            return vector
        except Exception as e:
            logger.error(f"Failed to embed query: {str(e)}", exc_info=True)
            raise

    @staticmethod
    async def embed_texts(
        embedding_config: EmbeddingConfig,
        texts: List[str]
    ) -> List[List[float]]:
        from openai import AsyncOpenAI
        client = AsyncOpenAI(
            api_key=embedding_config.api_key,
            base_url=embedding_config.base_url
        )
        try:
            response = await client.embeddings.create(
                model=embedding_config.model_id,
                input=texts
            )
            vectors = [item.embedding for item in response.data]
            logger.info(f"Embedded {len(texts)} texts successfully")
            return vectors
        except Exception as e:
            logger.error(f"Failed to embed texts: {str(e)}", exc_info=True)
            raise

    @staticmethod
    async def embed_query(
        embedding_config: EmbeddingConfig,
        query: str
    ) -> List[float]:
        from openai import AsyncOpenAI
        client = AsyncOpenAI(
            api_key=embedding_config.api_key,
            base_url=embedding_config.base_url
        )
        try:
            response = await client.embeddings.create(
                model=embedding_config.model_id,
                input=query
            )
            vector = response.data[0].embedding
            logger.info(f"Embedded query successfully")
            return vector
        except Exception as e:
            logger.error(f"Failed to embed query: {str(e)}", exc_info=True)
            raise
