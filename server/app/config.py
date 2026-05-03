from pydantic_settings import BaseSettings
from functools import lru_cache
from pathlib import Path

_DEFAULT_DATA_ROOT = Path(__file__).resolve().parent.parent / "data"


class Settings(BaseSettings):
    data_root: str = str(_DEFAULT_DATA_ROOT)
    summary_window_size: int = 5
    log_level: str = "INFO"
    task_max_iterations: int = 50
    workspace_root: str = ""
    context_window_iterations: int = 10
    notes_max_chars: int = 5000

    class Config:
        env_file = ".env"
        extra = "ignore"

    def get_data_root(self) -> Path:
        return Path(self.data_root)

    @property
    def database_url(self) -> str:
        return f"sqlite:///{self.get_data_root() / 'db' / 'private_buddy.db'}"

    @property
    def vector_db_file(self) -> str:
        return str(self.get_data_root() / "db" / "vectors.db")


@lru_cache()
def get_settings() -> Settings:
    return Settings()
