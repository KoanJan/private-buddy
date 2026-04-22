"""
LLM service module for creating and managing LLM instances.

This module provides utilities for creating LangChain chat models
and building message sequences for LLM processing.
"""

from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage, AIMessage, SystemMessage
from typing import List, Dict, Optional
from app.models.llm_config import LLMConfig


class LLMService:
    """
    Service for creating and managing LLM instances.
    
    This class provides static methods for creating LangChain chat models
    and building message sequences for LLM processing.
    """
    
    @staticmethod
    def create_chat_model(llm_config: LLMConfig) -> ChatOpenAI:
        """
        Create a ChatOpenAI instance from LLM configuration.
        
        Args:
            llm_config: LLM configuration containing model ID, API key, and base URL
            
        Returns:
            Configured ChatOpenAI instance with streaming enabled
        """
        return ChatOpenAI(
            model=llm_config.model_id,
            openai_api_base=llm_config.base_url,
            openai_api_key=llm_config.api_key,
            streaming=True
        )
    
    @staticmethod
    def build_messages(
        character_settings: Optional[str],
        history: List[Dict[str, str]],
        user_message: str
    ) -> List:
        """
        Build a list of LangChain messages for LLM processing.
        
        This method constructs a message sequence with character settings
        as system message, followed by conversation history, and ending
        with the current user message.
        
        Note: This method is deprecated. Use ContextAssemblyService instead
        for the new context engineering pipeline.
        
        Args:
            character_settings: Agent's personality, style, and identity settings
            history: List of historical messages with 'role' and 'content' keys
            user_message: The current user message
            
        Returns:
            List of LangChain messages ready for LLM processing
        """
        messages = []
        
        if character_settings:
            messages.append(SystemMessage(content=character_settings))
        
        for msg in history:
            if msg["role"] == "user":
                messages.append(HumanMessage(content=msg["content"]))
            elif msg["role"] == "assistant":
                messages.append(AIMessage(content=msg["content"]))
        
        messages.append(HumanMessage(content=user_message))
        
        return messages
