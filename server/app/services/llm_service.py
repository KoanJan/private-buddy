from langchain_openai import ChatOpenAI
from langchain.schema import HumanMessage, AIMessage, SystemMessage
from typing import List, Dict, Optional
from app.models.llm_config import LLMConfig


class LLMService:
    @staticmethod
    def create_chat_model(llm_config: LLMConfig) -> ChatOpenAI:
        return ChatOpenAI(
            model=llm_config.model_id,
            openai_api_base=llm_config.base_url,
            openai_api_key=llm_config.api_key,
            streaming=True
        )
    
    @staticmethod
    def build_messages(
        system_prompt: Optional[str],
        history: List[Dict[str, str]],
        user_message: str
    ) -> List:
        messages = []
        
        if system_prompt:
            messages.append(SystemMessage(content=system_prompt))
        
        for msg in history:
            if msg["role"] == "user":
                messages.append(HumanMessage(content=msg["content"]))
            elif msg["role"] == "assistant":
                messages.append(AIMessage(content=msg["content"]))
        
        messages.append(HumanMessage(content=user_message))
        
        return messages
