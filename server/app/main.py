from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles
from starlette.responses import Response
from app.database import engine, Base, SessionLocal
from app.api import llm_configs, sessions, messages, chat, agents, interactions, search_config, uploads
from app.services.task.workspace import get_avatars_dir
from app.config import get_settings
from app.logger import logger
from app.models.db_version import DBVersion
import os


class CachedStaticFiles(StaticFiles):
    """
    StaticFiles with Cache-Control header for browser caching.
    
    Avatar filenames include timestamps, so the URL changes when avatar is updated.
    This allows aggressive caching (24h) without stale content issues.
    """
    
    async def __call__(self, scope, receive, send) -> None:
        async def send_with_cache(message):
            if message["type"] == "http.response.start":
                headers = dict(message.get("headers", []))
                headers[b"cache-control"] = b"public, max-age=86400"
                message["headers"] = list(headers.items())
            await send(message)
        
        await super().__call__(scope, receive, send_with_cache)

settings = get_settings()
db_dir = os.path.join(settings.data_root, 'db')
os.makedirs(db_dir, exist_ok=True)

Base.metadata.create_all(bind=engine)

app = FastAPI(
    title="Private Buddy API",
    description="Private AI Assistant Backend API",
    version="0.0.8"
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(llm_configs.router)
app.include_router(sessions.router)
app.include_router(messages.router)
app.include_router(chat.router)
app.include_router(agents.router)
app.include_router(interactions.router)
app.include_router(search_config.router)
app.include_router(uploads.router)

app.mount(
    "/avatars",
    CachedStaticFiles(directory=str(get_avatars_dir())),
    name="avatars"
)


@app.get("/")
def root():
    logger.info("Root endpoint accessed")
    return {"message": "Private Buddy API is running"}


@app.get("/api/version")
def get_version():
    """
    Return the database schema version.
    
    Version is read from db_versions table, which tracks schema migrations.
    Returns '0.0.0' if no version record exists (fresh database).
    """
    db = SessionLocal()
    try:
        version_record = db.query(DBVersion).order_by(DBVersion.id.desc()).first()
        version = version_record.version if version_record else "0.0.0"
        return {"version": version}
    finally:
        db.close()


@app.on_event("startup")
async def startup_event():
    logger.info("Application starting up...")


@app.on_event("shutdown")
async def shutdown_event():
    logger.info("Application shutting down...")