"""
Query preprocessing module for intelligent routing and query transformation.

This module handles the preprocessing of user queries before they are sent to the
retrieval or LLM systems. It includes:
- Query type classification (clear, ambiguous, vague, no_query)
- Query rewriting for ambiguous queries with context
- Clarification generation for vague queries

The preprocessing pipeline ensures that queries are optimized for retrieval
and LLM processing.
"""

from typing import Optional, Dict, Any, List, Literal
from pydantic import BaseModel, Field, model_validator
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI
from app.models.llm_config import LLMConfig
from app.services.llm.llm_logger import TokenUsageLogger
from app.logger import logger


# Query type constants for classification
QUERY_TYPE_CLEAR = "clear"           # Query is complete and unambiguous
QUERY_TYPE_AMBIGUOUS = "ambiguous"   # Query contains references to previous context
QUERY_TYPE_VAGUE = "vague"           # Query is too vague to understand intent
QUERY_TYPE_NO_QUERY = "no_query"     # Query doesn't need retrieval (greetings, etc.)


class QueryRoutingResult(BaseModel):
    """
    Structured result for query routing and processing.
    
    This model defines the expected output format when the LLM classifies
    and processes a user query.
    """
    type: Literal["no_query", "clear", "ambiguous", "vague"] = Field(
        description="Query type classification"
    )
    rewritten_query: Optional[str] = Field(
        default=None,
        description="Rewritten query that is self-contained and clear (required for ambiguous type)"
    )
    reason: Optional[str] = Field(
        default=None,
        description="Reason why the query is vague and needs clarification (required for vague type)"
    )
    
    @model_validator(mode="after")
    def validate_fields(self) -> "QueryRoutingResult":
        """
        Validate that required fields are present based on query type.
        
        - ambiguous: rewritten_query is required
        - vague: reason is required
        """
        if self.type == QUERY_TYPE_AMBIGUOUS and not self.rewritten_query:
            raise ValueError("rewritten_query is required when type is 'ambiguous'")
        if self.type == QUERY_TYPE_VAGUE and not self.reason:
            raise ValueError("reason is required when type is 'vague'")
        return self


class QueryPreprocessingService:
    """
    Service for preprocessing user queries before retrieval and LLM processing.
    
    This service classifies queries into different types and applies appropriate
    transformations:
    - clear: Pass through unchanged
    - ambiguous: Rewrite with context from conversation history
    - vague: Generate clarification question for user
    - no_query: Skip retrieval entirely
    """
    
    ROUTING_PROMPT = """Analyze the user query type and process accordingly.

Conversation history:
{history}

Current user query: {query}

Classify the query type and process:
1. "no_query" - No retrieval needed: greetings, chitchat, emotional expressions, simple responses, etc. that can be answered without retrieving historical information.
2. "clear" - Clear query: the query is complete and unambiguous, requiring relevant information to answer.
3. "ambiguous" - Ambiguous reference: the query contains pronouns (like "it", "that", "this") or references to previous content, requiring context to understand. For this type, you MUST rewrite the user's query into a complete, clear query that can be understood independently without relying on conversation history.
4. "vague" - Too vague: the query is too brief or ambiguous, making it difficult to determine user intent even with context. For this type, explain the reason for vagueness."""

    CLARIFY_PROMPT = """The user's query is too vague and needs clarification.

Conversation history:
{history}

User query: {query}

Reason for vagueness: {reason}

Generate a clarification question to help the user clarify their intent. The question should be concise, specific, and provide possible options.

IMPORTANT: The clarification question MUST be in the SAME LANGUAGE as the user's query.
- If the user query is in Chinese, respond in Chinese.
- If the user query is in English, respond in English.

Output only the clarification question, without any additional content."""

    @staticmethod
    def create_chat_model(llm_config: LLMConfig) -> ChatOpenAI:
        """
        Create a ChatOpenAI instance from LLM configuration.
        
        Args:
            llm_config: LLM configuration containing model ID, API key, and base URL
            
        Returns:
            Configured ChatOpenAI instance with low temperature for consistent outputs
        """
        return ChatOpenAI(
            model=llm_config.model_id,
            openai_api_base=llm_config.base_url,
            openai_api_key=llm_config.api_key,
            temperature=0.1
        )

    @staticmethod
    def format_history_for_preprocessing(history: List[Dict[str, str]], max_messages: Optional[int] = None) -> str:
        """
        Format conversation history for preprocessing prompts.
        
        Converts message history into a human-readable format for LLM prompts.
        Limits the number of messages if max_messages is specified.
        
        Args:
            history: List of message dictionaries with 'role' and 'content' keys
            max_messages: Maximum number of recent messages to include (None for all)
            
        Returns:
            Formatted string with role prefixes (User/Assistant)
        """
        if not history:
            return "(No conversation history)"
        
        # Include all messages if no limit specified
        if max_messages is None:
            recent = history
        else:
            recent = history[-max_messages:] if len(history) > max_messages else history
        
        formatted = []
        for msg in recent:
            role = "User" if msg["role"] == "user" else "Assistant"
            formatted.append(f"{role}: {msg['content']}")
        return "\n".join(formatted)

    @staticmethod
    async def route_query(
        llm_config: LLMConfig,
        query: str,
        history: List[Dict[str, str]],
        max_messages: Optional[int] = None
    ) -> QueryRoutingResult:
        """
        Classify the query type and rewrite if ambiguous.
        
        Determines whether the query is clear, ambiguous, vague, or doesn't need
        retrieval. For ambiguous queries, also rewrites the query with context.
        
        Args:
            llm_config: LLM configuration for the routing model
            query: The user's query text
            history: Conversation history for context
            max_messages: Maximum messages to include in routing context
            
        Returns:
            QueryRoutingResult with type, rewritten_query (if ambiguous), and reason (if vague)
        """
        chat_model = QueryPreprocessingService.create_chat_model(llm_config)
        structured_model = chat_model.with_structured_output(QueryRoutingResult)
        
        history_text = QueryPreprocessingService.format_history_for_preprocessing(history, max_messages)
        
        prompt = QueryPreprocessingService.ROUTING_PROMPT.format(
            history=history_text,
            query=query
        )
        
        try:
            messages = [HumanMessage(content=prompt)]
            result = await structured_model.ainvoke(messages, config={"callbacks": [TokenUsageLogger()]})
            
            logger.info(f"Query routing result: type={result.type}")
            if result.type == QUERY_TYPE_AMBIGUOUS:
                logger.info(f"Query rewritten: '{query}' -> '{result.rewritten_query}'")
            
            return result
            
        except Exception as e:
            logger.error(f"Query routing failed: {str(e)}", exc_info=True)
            # Default to clear type on error
            return QueryRoutingResult(type=QUERY_TYPE_CLEAR)

    @staticmethod
    async def generate_clarification(
        llm_config: LLMConfig,
        query: str,
        history: List[Dict[str, str]],
        reason: str,
        character_settings: Optional[str] = None,
        max_messages: Optional[int] = None
    ) -> str:
        """
        Generate a clarification question for vague queries.
        
        When a query is too vague to understand, this method generates
        a question to ask the user for more information.
        
        Args:
            llm_config: LLM configuration for generating clarification
            query: The vague user query
            history: Conversation history for context
            reason: Explanation of why the query is vague
            character_settings: Optional character settings for the agent
            max_messages: Maximum messages to include in context
            
        Returns:
            Clarification question to present to the user
        """
        chat_model = QueryPreprocessingService.create_chat_model(llm_config)
        
        history_text = QueryPreprocessingService.format_history_for_preprocessing(history, max_messages)
        
        prompt = QueryPreprocessingService.CLARIFY_PROMPT.format(
            history=history_text,
            query=query,
            reason=reason
        )
        
        try:
            if character_settings:
                prompt = f"[Your Character]\n{character_settings}\n\n{prompt}"
            messages = [HumanMessage(content=prompt)]
            
            response = await chat_model.ainvoke(messages, config={"callbacks": [TokenUsageLogger()]})
            clarification = response.content.strip()
            
            logger.info(f"Generated clarification for query: '{query}'")
            return clarification
            
        except Exception as e:
            logger.error(f"Clarification generation failed: {str(e)}", exc_info=True)
            return "Your question is a bit vague. Could you please provide more details about your needs?"

    @staticmethod
    async def preprocess_query(
        llm_config: LLMConfig,
        query: str,
        history: List[Dict[str, str]],
        character_settings: Optional[str] = None,
        max_messages: Optional[int] = None
    ) -> Dict[str, Any]:
        """
        Main entry point for query preprocessing.
        
        Routes the query, applies appropriate transformations, and returns
        a comprehensive result with all preprocessing decisions.
        
        Args:
            llm_config: LLM configuration for preprocessing models
            query: The user's query text
            history: Conversation history for context
            character_settings: Optional character settings for the agent
            max_messages: Maximum messages to include in preprocessing context
            
        Returns:
            Dictionary containing:
            - original_query: The original query text
            - processed_query: The processed/rewritten query
            - query_type: Classification (clear, ambiguous, vague, no_query)
            - needs_clarification: Whether user clarification is needed
            - clarification: Clarification question (if needed)
            - skip_retrieval: Whether to skip RAG retrieval
        """
        result = {
            "original_query": query,
            "processed_query": query,
            "query_type": QUERY_TYPE_CLEAR,
            "needs_clarification": False,
            "clarification": None,
            "skip_retrieval": False
        }
        
        # Step 1: Route the query to determine its type (and rewrite if ambiguous)
        routing = await QueryPreprocessingService.route_query(llm_config, query, history, max_messages)
        query_type = routing.type
        result["query_type"] = query_type
        
        # Step 2: Apply type-specific processing
        if query_type == QUERY_TYPE_NO_QUERY:
            # No retrieval needed for greetings, chitchat, etc.
            result["processed_query"] = query
            result["skip_retrieval"] = True
            
        elif query_type == QUERY_TYPE_CLEAR:
            # Clear query passes through unchanged
            result["processed_query"] = query
            result["skip_retrieval"] = False
            
        elif query_type == QUERY_TYPE_AMBIGUOUS:
            # Use rewritten query from routing result
            result["processed_query"] = routing.rewritten_query or query
            
        elif query_type == QUERY_TYPE_VAGUE:
            # Generate clarification for vague queries
            reason = routing.reason or "Query is too vague"
            clarification = await QueryPreprocessingService.generate_clarification(
                llm_config, query, history, reason, character_settings, max_messages
            )
            result["needs_clarification"] = True
            result["clarification"] = clarification
        
        logger.info(f"Query preprocessing complete: type={query_type}, processed='{result['processed_query'][:50]}...'")
        return result
