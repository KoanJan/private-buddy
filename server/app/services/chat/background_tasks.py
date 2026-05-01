"""
Background task module for asynchronous chat processing.

This module contains the core background tasks that handle:
- Chat message processing with context engineering
- Agent execution for world-interaction requests
- LLM streaming responses
- Summary generation triggers

The tasks are designed to run asynchronously without blocking the API,
enabling real-time streaming responses via SSE (Server-Sent Events).

Flow:
1. User sends message -> API creates user_msg + ai_msg placeholders
2. Background task infers user state (including needs_world_interaction)
3. If needs_world_interaction=true:
   - Set ai_msg.has_interactions=1 (exists)
   - Execute agent via TaskExecutor with session workspace
   - Record interactions to database
   - Pass task_result to context assembly for LLM response
4. If needs_world_interaction=false:
   - Set ai_msg.has_interactions=2 (none)
   - Continue with normal LLM chat flow (context engineering + streaming)
"""

from pathlib import Path
from typing import Optional, Tuple

from app.models.session import SESSION_STATUS_IDLE
from app.models.message import Message, MESSAGE_STATUS_COMPLETED, HAS_INTERACTIONS_EXISTS, HAS_INTERACTIONS_NONE
from app.services.llm import LLMService
from app.services.data_service import DataService
from app.services.chat.connection_manager import manager
from app.services.chat.context import SummaryService, RetrievalService, ContextAssemblyService, QueryPreprocessingService
from app.services.chat.context.user_state import UserStateService
from app.services.dto.task_result import TaskResult
from app.services.task.task_executor import TaskExecutor
from app.services.task.workspace import init_session_workspace
from app.services.task.requirement_rewriter import TaskRequirementRewriter
from app.database import SessionLocal
from app.config import get_settings
from app.logger import logger
import asyncio


# User-friendly error message for unexpected failures
USER_FRIENDLY_ERROR_MESSAGE = "抱歉，服务器遇到了一些问题，请稍后再试。"


async def process_chat_task(
    trigger_message_id: int,
    ai_message_id: int
):
    """
    Process chat task triggered by a message.
    
    This is the main background task that handles the complete chat processing pipeline:
    1. Load session, agent, and LLM configuration
    2. Infer user state (including needs_world_interaction)
    3. If needs_world_interaction=true: execute agent and pass result to context assembly
    4. If needs_world_interaction=false: apply context engineering
    5. Stream LLM response
    6. Trigger summary generation if needed
    
    Context Engineering Variables:
        V = current message count in session
        N = summary window size (configurable via settings.summary_window_size)
        
        - V < N: Skip context engineering, use all messages directly (no summary exists)
        - V >= N: Apply full context engineering pipeline (summary + retrieval + assembly)
    
    Args:
        trigger_message_id: The ID of the message that triggers this task
        ai_message_id: The ID of the AI message placeholder (created by API)
    """
    db = SessionLocal()
    try:
        # Load trigger message
        trigger_msg = db.query(Message).filter(Message.id == trigger_message_id).first()
        if not trigger_msg:
            logger.error(f"[process_chat_task] Trigger message not found: trigger_message_id={trigger_message_id}")
            return
        
        # Load AI message placeholder
        ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
        if not ai_msg:
            logger.error(f"[process_chat_task] AI message not found: ai_message_id={ai_message_id}, trigger_message_id={trigger_message_id}")
            return
        
        session_id = trigger_msg.session_id
        logger.info(f"Starting background chat task: session_id={session_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}")
        
        # Load session
        session = DataService.get_session(db, session_id)
        if not session:
            logger.error(f"[process_chat_task] Session not found: session_id={session_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}")
            ai_msg.status = MESSAGE_STATUS_COMPLETED
            ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
            db.commit()
            return
        
        # Load agent
        agent = DataService.get_agent(db, session.agent_id)
        if not agent:
            logger.error(f"[process_chat_task] Agent not found: session_id={session_id}, agent_id={session.agent_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}")
            ai_msg.status = MESSAGE_STATUS_COMPLETED
            ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
            db.commit()
            return
        
        # Load LLM configuration
        llm_config = DataService.get_llm_config(db, agent.llm_config_id)
        if not llm_config:
            logger.error(f"[process_chat_task] LLM config not found: session_id={session_id}, agent_id={agent.id}, llm_config_id={agent.llm_config_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}")
            ai_msg.status = MESSAGE_STATUS_COMPLETED
            ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
            db.commit()
            return
        
        # Current limitation: trigger message must be from user
        if trigger_msg.role != "user":
            logger.error(f"[process_chat_task] Trigger message is not from user: session_id={session_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}, role={trigger_msg.role}")
            ai_msg.status = MESSAGE_STATUS_COMPLETED
            ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
            db.commit()
            return
        
        # Get message count in session (V = session message count, not global ID)
        message_count = db.query(Message).filter(
            Message.session_id == session_id
        ).count()
        
        logger.info(f"AI message {ai_message_id} for session {session_id}, V={message_count}")
        
        settings = get_settings()
        window_size = settings.summary_window_size
        
        # --- Parallel: User State + Query Preprocessing (when V >= N) ---
        # These two LLM calls have no data dependency and can run concurrently.
        # When V < N, preprocessing is not needed, so only user state is invoked.
        recent_messages_for_state = RetrievalService.get_recent_messages(
            db, session_id, limit=min(message_count, window_size), status=MESSAGE_STATUS_COMPLETED
        )
        
        if message_count >= window_size:
            # Speculative parallel execution: both calls start simultaneously
            preprocessing_history = DataService.get_message_history(
                db, session_id, ai_message_id, limit=window_size
            )
            
            user_state_coro = UserStateService.infer_user_state(llm_config, recent_messages_for_state)
            preprocessing_coro = QueryPreprocessingService.preprocess_query(
                llm_config, trigger_msg.content, preprocessing_history,
                agent.character_settings if agent.character_settings else None,
                max_messages=window_size
            )
            
            user_state_result, preprocessing_result = await asyncio.gather(
                user_state_coro, preprocessing_coro
            )
            logger.info(f"Parallel execution completed: user_state and preprocessing for session {session_id}")
        else:
            # V < N: only user state is needed
            user_state_result = await UserStateService.infer_user_state(llm_config, recent_messages_for_state)
            preprocessing_result = None
        
        needs_world_interaction = user_state_result.needs_world_interaction if user_state_result else False
        logger.info(
            f"User state inference: needs_world_interaction={needs_world_interaction}, "
            f"session_id={session_id}, trigger_message_id={trigger_message_id}"
        )
        
        # --- Agent execution (if needed) ---
        task_result: Optional[TaskResult] = None
        if needs_world_interaction:
            # Preprocessing result is discarded when agent path is taken
            ai_msg.has_interactions = HAS_INTERACTIONS_EXISTS
            db.commit()
            task_result = await _execute_agent(
                db=db,
                ai_msg=ai_msg,
                session=session,
                agent=agent,
                llm_config=llm_config,
                trigger_msg=trigger_msg,
                session_id=session_id,
                message_count=message_count,
                window_size=window_size,
            )
        else:
            ai_msg.has_interactions = HAS_INTERACTIONS_NONE
            db.commit()
        
        chat_model = LLMService.create_chat_model(llm_config)
        
        # Branch 1: V < N - Skip context engineering, use all messages directly
        if message_count < window_size:
            logger.info(f"V({message_count}) < N({window_size}), skipping context engineering")
            recent_messages = RetrievalService.get_recent_messages(
                db, session_id, limit=message_count, status=MESSAGE_STATUS_COMPLETED
            )
            
            # Validate trigger_message is the latest completed message
            if not recent_messages or recent_messages[-1]['id'] != trigger_message_id:
                logger.error(f"[process_chat_task] Trigger message is not the latest completed message: session_id={session_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}, recent_messages_count={len(recent_messages)}, latest_message_id={recent_messages[-1]['id'] if recent_messages else None}")
                ai_msg.status = MESSAGE_STATUS_COMPLETED
                ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
                db.commit()
                return
            
            # Assemble simple context without background story
            langchain_messages = ContextAssemblyService.assemble_context(
                character_settings=agent.character_settings if agent.character_settings else None,
                background_story=None,
                recent_messages=recent_messages,
                summary_version=None,
                recent_start=1,
                recent_end=len(recent_messages),
                task_result=task_result,
            )
            has_embedding = False
            
        # Branch 2: V >= N - Apply full context engineering pipeline
        else:
            # Handle clarification needed case (from parallel preprocessing)
            if preprocessing_result["needs_clarification"]:
                ai_msg.status = MESSAGE_STATUS_COMPLETED
                ai_msg.content = preprocessing_result["clarification"]
                session.status = SESSION_STATUS_IDLE
                db.commit()
                
                await manager.notify(session_id, {'type': 'done'})
                logger.info(f"Query needed clarification for session {session_id}")
                return
            
            processed_query = preprocessing_result["processed_query"]
            logger.info(f"Query type: {preprocessing_result['query_type']}, processed: '{processed_query[:50]}...'")
            
            # Context retrieval (with or without RAG)
            if preprocessing_result.get("skip_retrieval"):
                context = RetrievalService.get_context_without_rag(
                    db, session_id, recent_count=window_size
                )
                has_embedding = False
                logger.info(f"Context assembled (skip RAG) - summary: {context['summary'] is not None}")
            else:
                context = RetrievalService.get_context_for_chat(
                    db, session_id, processed_query,
                    recent_count=window_size, rag_count=5
                )
                has_embedding = context.get('has_embedding', False)
                logger.info(f"Context assembled - has_embedding: {has_embedding}, "
                           f"recent: {len(context['recent_messages'])}, "
                           f"segments: {len(context['relevant_segments'])}, "
                           f"summary: {context['summary'] is not None}")
            
            # Validate trigger_message is the latest completed message
            if not context['recent_messages'] or context['recent_messages'][-1]['id'] != trigger_message_id:
                logger.error(f"[process_chat_task] Trigger message is not the latest completed message: session_id={session_id}, trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}, recent_messages_count={len(context['recent_messages'])}, latest_message_id={context['recent_messages'][-1]['id'] if context['recent_messages'] else None}")
                ai_msg.status = MESSAGE_STATUS_COMPLETED
                ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
                db.commit()
                return
            
            # Use cached narrative (generated in background with summary)
            background_story = context.get('narrative')
            
            # Convert user state to natural language description for prompt injection
            user_state_description = user_state_result.to_natural_language() if user_state_result else None
            
            # Calculate message sequence numbers for metadata
            summary_version = context['summary']['version'] if context['summary'] else None
            recent_start = message_count - len(context['recent_messages']) + 1
            
            # Assemble context with metadata, user state, and segments
            langchain_messages = ContextAssemblyService.assemble_context(
                character_settings=agent.character_settings if agent.character_settings else None,
                background_story=background_story,
                recent_messages=context['recent_messages'],
                relevant_segments=context.get('relevant_segments', []),
                summary_version=summary_version,
                recent_start=recent_start,
                recent_end=message_count,
                user_state_description=user_state_description,
                task_result=task_result,
            )
            logger.info(f"Using context assembly with cached_narrative: {background_story is not None}, segments: {len(context.get('relevant_segments', []))}, user_state: {user_state_description is not None}, task_result: {task_result is not None}")
        
        logger.debug(f"Message list for LLM: {[{'type': type(m).__name__, 'content': m.content} for m in langchain_messages]}")
        
        # Stream LLM response and notify clients
        logger.info("Starting LLM stream in background...")
        full_response = ""
        
        async for chunk in chat_model.astream(langchain_messages):
            if chunk.content:
                full_response += chunk.content
                
                # Update AI message content in database
                ai_msg.content = full_response
                db.commit()
                
                # Notify client via SSE
                await manager.notify(session_id, {
                    'type': 'chunk',
                    'content': chunk.content
                })
        
        # Mark AI message as completed and release session status
        ai_msg.status = MESSAGE_STATUS_COMPLETED
        ai_msg.content = full_response
        session.status = SESSION_STATUS_IDLE
        db.commit()
        
        # Notify client that streaming is done
        await manager.notify(session_id, {'type': 'done'})
        
        logger.info(f"Background chat task completed for session {session_id}, response length: {len(full_response)}")
        
        # Get updated message count after AI response
        message_count = db.query(Message).filter(
            Message.session_id == session_id
        ).count()
        
        # Index messages for RAG if embedding is available
        if has_embedding:
            if RetrievalService.index_messages(db, session_id, [trigger_message_id, ai_message_id]):
                logger.info(f"Indexed messages {trigger_message_id} and {ai_message_id} for session {session_id}")
        
        # Trigger summary generation if V >= N
        if message_count >= window_size:
            logger.info(f"Triggering summary generation for session {session_id}, V={message_count}, N={window_size}")
            asyncio.create_task(generate_summary_task(session_id, message_count))
        
    except Exception as e:
        logger.error(f"[process_chat_task] Unexpected error: trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}, error={str(e)}", exc_info=True)
        
        # Handle failure gracefully
        try:
            ai_msg = db.query(Message).filter(Message.id == ai_message_id).first()
            if ai_msg:
                ai_msg.status = MESSAGE_STATUS_COMPLETED
                ai_msg.content = USER_FRIENDLY_ERROR_MESSAGE
                db.commit()
            
            session = DataService.get_session(db, trigger_msg.session_id) if 'trigger_msg' in dir() else None
            if session:
                session.status = SESSION_STATUS_IDLE
                db.commit()
            
            await manager.notify(trigger_msg.session_id if 'trigger_msg' in dir() else 0, {'type': 'done'})
        except Exception as inner_e:
            logger.error(f"[process_chat_task] Error handling failure: trigger_message_id={trigger_message_id}, ai_message_id={ai_message_id}, inner_error={str(inner_e)}", exc_info=True)
    
    finally:
        db.close()


async def _execute_agent(
    db,
    ai_msg: Message,
    session,
    agent,
    llm_config,
    trigger_msg: Message,
    session_id: int,
    message_count: int,
    window_size: int,
) -> Optional[TaskResult]:
    """
    Execute agent for world-interaction requests.
    
    This function handles the agent execution path when needs_world_interaction=true:
    1. Notify frontend that agent is processing
    2. Rewrite user message into clear task requirement
    3. Execute agent via TaskExecutor
    4. Return TaskResult for context assembly
    
    Args:
        db: Database session.
        ai_msg: The AI message placeholder.
        session: The session model.
        agent: The agent model.
        llm_config: LLM configuration.
        trigger_msg: The user message that triggered execution.
        session_id: The session ID.
        message_count: Current message count in session.
        window_size: Summary window size.
    
    Returns:
        TaskResult if execution succeeded, None otherwise.
    """
    logger.info(f"Agent execution path: session_id={session_id}, ai_msg_id={ai_msg.id}")
    
    # Notify frontend that agent is processing
    await manager.notify(session_id, {
        'type': 'agent_processing',
        'message': 'Agent is processing your request...'
    })
    
    try:
        # Rewrite user message into clear task requirement
        recent_messages = RetrievalService.get_recent_messages(
            db, session_id, limit=min(message_count, window_size), status=MESSAGE_STATUS_COMPLETED
        )
        
        rewritten_requirement = await TaskRequirementRewriter.rewrite(
            llm_config=llm_config,
            user_message=trigger_msg.content,
            history=recent_messages,
        )
        logger.info(
            f"Task requirement rewritten: session_id={session_id}, "
            f"original='{trigger_msg.content[:50]}...', rewritten='{rewritten_requirement[:50]}...'"
        )
        
        # Execute agent via TaskExecutor
        task_executor = TaskExecutor(db)
        delivery = await task_executor.execute(
            task_requirement=rewritten_requirement,
            llm_config=llm_config,
            session_id=session_id,
            user_msg_id=trigger_msg.id,
            agent_msg_id=ai_msg.id,
        )
        
        logger.info(
            f"Agent execution completed: session_id={session_id}, "
            f"status={delivery.status}, has_notes={delivery.notes is not None}"
        )
        
        return delivery
        
    except Exception as e:
        logger.error(f"Agent execution error: session_id={session_id}, error={str(e)}", exc_info=True)
        return None


async def generate_summary_task(session_id: int, version: int):
    """
    Generate a summary for the session at the specified version.
    
    This task is triggered after each message when V >= N. It generates
    a summary that covers messages up to the specified version number.
    
    The summary generation follows these rules:
    - If version < N: Skip (no summary needed)
    - If N <= version < 2N: Use all messages (1 to version) with empty baseline
    - If version >= 2N: Use baseline summary (version - N) + recent messages
    
    Args:
        session_id: The session ID to generate summary for
        version: The message count (V) at which to generate the summary
    """
    db = SessionLocal()
    try:
        logger.info(f"Starting summary generation for session {session_id}, version {version}")
        
        # Load session
        session = DataService.get_session(db, session_id)
        if not session:
            logger.error(f"[generate_summary_task] Session not found: session_id={session_id}")
            return
        
        # Load agent for LLM config
        agent = DataService.get_agent(db, session.agent_id)
        if not agent:
            logger.error(f"[generate_summary_task] Agent not found: session_id={session_id}, agent_id={session.agent_id}")
            return
        
        llm_config = DataService.get_llm_config(db, agent.llm_config_id)
        if not llm_config:
            logger.error(f"[generate_summary_task] LLM config not found: session_id={session_id}, llm_config_id={agent.llm_config_id}")
            return
        
        # Generate summary
        settings = get_settings()
        await SummaryService.generate_summary(db, session_id, version, llm_config, settings.summary_window_size)
        
        logger.info(f"Summary generation completed for session {session_id}, version {version}")
        
    except Exception as e:
        logger.error(f"[generate_summary_task] Error: session_id={session_id}, version={version}, error={str(e)}", exc_info=True)
    
    finally:
        db.close()
