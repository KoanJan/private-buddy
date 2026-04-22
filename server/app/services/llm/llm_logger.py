"""
LLM callback logger for token usage and latency tracking.
"""

import time
from langchain_core.callbacks import BaseCallbackHandler
from langchain_core.outputs import LLMResult
from app.logger import logger
from typing import Any, Dict, Optional, List
from uuid import UUID


class TokenUsageLogger(BaseCallbackHandler):
    """
    Callback handler that logs token usage and latency for each LLM call.
    
    Usage:
        callback = TokenUsageLogger()
        response = await chat_model.ainvoke(messages, config={"callbacks": [callback]})
    """
    
    def __init__(self):
        self._start_time: Optional[float] = None
        self._model: Optional[str] = None
    
    def on_chat_model_start(
        self,
        serialized: Dict[str, Any],
        messages: List[List[Any]],
        *,
        run_id: UUID,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[List[str]] = None,
        metadata: Optional[Dict[str, Any]] = None,
        **kwargs: Any
    ) -> None:
        """
        Called when chat model starts. Records start time and model info.
        """
        self._start_time = time.perf_counter()
        
        # Extract model from invocation_params
        invocation_params = kwargs.get("invocation_params", {})
        self._model = invocation_params.get("model") or invocation_params.get("model_name")
    
    def on_llm_end(
        self,
        response: LLMResult,
        *,
        run_id: UUID,
        parent_run_id: Optional[UUID] = None,
        tags: Optional[List[str]] = None,
        **kwargs: Any
    ) -> None:
        """
        Called when LLM call ends. Logs token usage and latency.
        """
        latency_ms = 0.0
        if self._start_time:
            latency_ms = (time.perf_counter() - self._start_time) * 1000

        # Try llm_output.token_usage (standard format)
        if response.llm_output and "token_usage" in response.llm_output:
            parts = ["llm usage", f"latency={latency_ms:.0f}ms"]
            usage = response.llm_output["token_usage"]
            parts.append(f"prompt_tokens: {usage.get('prompt_tokens')}")
            parts.append(f"completion_tokens: {usage.get('completion_tokens')}")
            parts.append(f"total_tokens: {usage.get('total_tokens')}")
            if self._model:
                parts.append(f"model={self._model}")
            logger.debug(" | ".join(parts))
