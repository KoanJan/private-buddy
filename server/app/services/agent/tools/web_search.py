"""
Web search tool for retrieving information from the internet.

Supports multiple search providers:
- Tavily: Requires API key, high quality results
- DuckDuckGo: No API key required, but may be blocked in some networks
"""

import json
from typing import Any, Dict, List, Optional

from app.services.agent.tools.base import Tool
from app.logger import logger


class WebSearchTool(Tool):
    """
    Tool for searching the web.

    Uses the configured search provider (Tavily or DuckDuckGo).
    Returns a list of search results with title, URL, and snippet.
    """

    DEFAULT_NUM_RESULTS = 5

    def __init__(self, search_config: Optional[Any] = None):
        """
        Initialize the web search tool.

        Args:
            search_config: SearchConfig instance containing provider and api_key.
                          If None, the tool will not be functional.
        """
        self._search_config = search_config

    @property
    def name(self) -> str:
        return "web_search"

    @property
    def schema(self) -> Dict[str, Any]:
        return {
            "type": "function",
            "function": {
                "name": "web_search",
                "description": (
                    "Search the web for information. Use this tool to find "
                    "current information, documentation, or answers to questions."
                ),
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {
                            "type": "string",
                            "description": "The search query",
                        },
                        "num_results": {
                            "type": "integer",
                            "description": "Number of results to return (default: 5)",
                            "default": self.DEFAULT_NUM_RESULTS,
                        },
                    },
                    "required": ["query"],
                },
            },
        }

    async def execute(self, **kwargs) -> str:
        """
        Execute a web search and return results.

        Args:
            query: The search query string.
            num_results: Number of results to return (default: 5).

        Returns:
            JSON string with a list of search results.
        """
        query = kwargs.get("query", "")
        num_results = kwargs.get("num_results", self.DEFAULT_NUM_RESULTS)

        if not query:
            return json.dumps({"results": [], "error": "Empty query"})

        if not self._search_config:
            return json.dumps({
                "results": [],
                "error": "Search engine not configured"
            })

        provider = self._search_config.provider
        api_key = self._search_config.api_key

        logger.info(
            f"WebSearchTool searching: query='{query[:50]}...', "
            f"provider={provider}, num_results={num_results}"
        )

        try:
            if provider == "tavily":
                results = await self._search_tavily(api_key, query, num_results)
            elif provider == "duckduckgo":
                results = await self._search_duckduckgo(query, num_results)
            else:
                return json.dumps({
                    "results": [],
                    "error": f"Unknown search provider: {provider}"
                })

            logger.info(f"WebSearchTool found {len(results)} results")
            return json.dumps({"results": results}, ensure_ascii=False)

        except Exception as e:
            logger.error(f"WebSearchTool error: {str(e)}", exc_info=True)
            return json.dumps({"results": [], "error": str(e)})

    async def _search_tavily(
        self, api_key: str, query: str, num_results: int
    ) -> List[Dict[str, str]]:
        """
        Perform a Tavily search.

        Args:
            api_key: Tavily API key.
            query: Search query string.
            num_results: Maximum number of results to return.

        Returns:
            List of dicts with title, url, and snippet keys.
        """
        from tavily import TavilyClient

        client = TavilyClient(api_key=api_key)
        result = client.search(query=query, max_results=num_results)

        results = []
        for item in result.get("results", []):
            results.append({
                "title": item.get("title", ""),
                "url": item.get("url", ""),
                "snippet": item.get("content", ""),
            })

        return results

    async def _search_duckduckgo(
        self, query: str, num_results: int
    ) -> List[Dict[str, str]]:
        """
        Perform a DuckDuckGo search.

        Args:
            query: Search query string.
            num_results: Maximum number of results to return.

        Returns:
            List of dicts with title, url, and snippet keys.
        """
        from duckduckgo_search import DDGS

        results = []
        with DDGS() as ddgs:
            search_results = list(ddgs.text(query, max_results=num_results))

        for item in search_results:
            results.append({
                "title": item.get("title", ""),
                "url": item.get("href", ""),
                "snippet": item.get("body", ""),
            })

        return results
