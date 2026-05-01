from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles
from starlette.responses import Response
from app.database import engine, Base
from app.api import llm_configs, sessions, messages, chat, agents, embedding_configs, interactions, search_config, uploads
from app.services.task.workspace import get_avatars_dir
from app.logger import logger


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

Base.metadata.create_all(bind=engine)

app = FastAPI(
    title="Private Buddy API",
    description="Private AI Assistant Backend API",
    version="0.0.4"
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(llm_configs.router)
app.include_router(embedding_configs.router)
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


@app.on_event("startup")
async def startup_event():
    logger.info("Application starting up...")


@app.on_event("shutdown")
async def shutdown_event():
    logger.info("Application shutting down...")