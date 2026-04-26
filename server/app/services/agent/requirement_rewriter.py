"""
Task requirement rewriting service for agent execution.

This module handles the rewriting of user messages into clear, self-contained
task requirements before they are passed to the agent execution system.

Purpose:
- Transform ambiguous user messages (e.g., "change xxx") into explicit task requirements
- Utilize conversation history to resolve references and provide context
- Keep this logic separate from chat query preprocessing (which is for RAG)

Example:
    User message: "改一下那个文件"
    History: [User: "帮我创建一个 README.md", Assistant: "已创建..."]
    Rewritten: "修改 README.md 文件，具体修改内容需要用户确认"
"""

from typing import Optional, List, Dict
from pydantic import BaseModel, Field
from langchain_core.messages import HumanMessage
from langchain_openai import ChatOpenAI

from app.models.llm_config import LLMConfig
from app.services.llm.llm_logger import TokenUsageLogger
from app.logger import logger


class RewrittenRequirement(BaseModel):
    """
    Structured result for task requirement rewriting.
    """
    requirement: str = Field(
        description="The rewritten, self-contained task requirement"
    )
    context_summary: Optional[str] = Field(
        default=None,
        description="Brief summary of relevant context used for rewriting"
    )


class TaskRequirementRewriter:
    """
    Service for rewriting user messages into clear task requirements.
    
    Unlike QueryPreprocessingService (which is for RAG retrieval optimization),
    this service focuses on making task requirements explicit and actionable
    for the agent execution system.
    """
    
    REWRITE_PROMPT = """You are a task requirement rewriter. Your job is to transform the user's message into a clear, self-contained task requirement that can be executed by an AI agent.

Conversation history:
{history}

Current user message: {query}

Your task:
1. Analyze the user's message in the context of the conversation history
2. Identify any references to previous content (files, code, topics discussed)
3. Extract the actual task the user wants to accomplish
4. Write a clear, complete task requirement that:
   - Can be understood without the conversation history
   - Specifies what needs to be done
   - Includes relevant details from the context (file paths, specific content, etc.)

IMPORTANT RULES:
- If the user's message is already clear and complete, output it as-is
- If the message references previous context, incorporate that context
- If the message is too vague even with context, state what information is missing
- Keep the rewritten requirement concise but complete
- The output should be in the SAME LANGUAGE as the user's message

Output a JSON object with:
- requirement: The rewritten task requirement (required)
- context_summary: Brief note on what context was used (optional)"""

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
    def format_history(history: List[Dict[str, str]], max_messages: int = 10) -> str:
        """
        Format conversation history for the rewrite prompt.
        
        Args:
            history: List of message dictionaries with 'role' and 'content' keys
            max_messages: Maximum number of recent messages to include
            
        Returns:
            Formatted string with role prefixes
        """
        if not history:
            return "(No conversation history)"
        
        recent = history[-max_messages:] if len(history) > max_messages else history
        
        formatted = []
        for msg in recent:
            role = "User" if msg["role"] == "user" else "Assistant"
            formatted.append(f"{role}: {msg['content']}")
        return "\n".join(formatted)

    @staticmethod
    async def rewrite(
        llm_config: LLMConfig,
        user_message: str,
        history: List[Dict[str, str]],
        max_history_messages: int = 10
    ) -> str:
        """
        Rewrite a user message into a clear task requirement.
        
        This is the main entry point. It uses conversation history to
        resolve references and create a self-contained task requirement.
        
        Args:
            llm_config: LLM configuration for the rewriting model
            user_message: The user's message that triggered agent execution
            history: Conversation history for context
            max_history_messages: Maximum messages to include for context
            
        Returns:
            Rewritten task requirement string
        """
        chat_model = TaskRequirementRewriter.create_chat_model(llm_config)
        structured_model = chat_model.with_structured_output(RewrittenRequirement)
        
        history_text = TaskRequirementRewriter.format_history(history, max_history_messages)
        
        prompt = TaskRequirementRewriter.REWRITE_PROMPT.format(
            history=history_text,
            query=user_message
        )
        
        try:
            messages = [HumanMessage(content=prompt)]
            result = await structured_model.ainvoke(messages, config={"callbacks": [TokenUsageLogger()]})
            
            logger.info(f"Task requirement rewritten: '{user_message[:50]}...' -> '{result.requirement[:50]}...'")
            if result.context_summary:
                logger.info(f"Context used: {result.context_summary}")
            
            return result.requirement
            
        except Exception as e:
            logger.error(f"Task requirement rewrite failed: {str(e)}", exc_info=True)
            # Return original message on error
            return user_message
