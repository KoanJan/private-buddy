from pydantic_settings import BaseSettings
from functools import lru_cache


class Settings(BaseSettings):
    database_url: str
    secret_key: str
    summary_window_size: int = 5
    chroma_persist_dir: str = "./chroma_db"
    log_level: str = "INFO"
    task_max_iterations: int = 90
    workspace_root: str = ""
    llm_base_url: str = ""
    llm_model: str = ""
    llm_api_key: str = ""

    class Config:
        env_file = ".env"
        extra = "ignore"


@lru_cache()
def get_settings() -> Settings:
    return Settings()
