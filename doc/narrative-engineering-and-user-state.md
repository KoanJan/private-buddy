# Narrative Engineering & User State Inference

How we apply narratology and intent recognition theory to make AI conversations feel more natural.

---

## The Problem

When a conversation grows long, we can't feed the entire history to the LLM every time. We compress older messages into a summary and keep only the most recent ones verbatim. These components are then assembled into a single prompt — the "one big message" — that the LLM uses to generate its next response.

The challenge: **how do we assemble this prompt so the LLM naturally continues the conversation, rather than merely "retelling" it?**

Two issues stood out:

1. **The summary reads like a report, not a memory.** Compressed history written in third person ("The assistant and the user discussed...") creates distance — the LLM reads about a conversation rather than feeling like it's *in* one.
2. **The LLM has no sense of who it's talking to right now.** The same words mean different things depending on the user's emotional state, purpose, and physical context. Without this awareness, responses can feel tone-deaf.

---

## Part 1: Narrative Optimization

### Theory

We draw on **focalization** from narratology (Toolan, 2001) — the distinction between *who speaks* (narrative voice) and *who sees* (focalization). Internal focalization lets the reader perceive events through a character's eyes, creating immersion. External focalization keeps the reader at a distance, observing from outside.

### Narrative Techniques

**Internal focalization for background story**

The background story (compressed history) uses internal focalization — addressing the agent as "You":

```
"You have been discussing travel plans with the user. The user mentioned..."
```

The background story represents the agent's *own* history. Writing it from the agent's viewpoint ("You have been...") lets the LLM naturally step into the role and continue the conversation, rather than narrating it from the sidelines.

**Cohesive transitions between sections**

Section headers between prompt components use narrative-style transitions rather than bracketed metadata labels:

```
Background context from earlier in the conversation (messages 1-10):
---
Recent conversation (messages 11-15):
```

The narrative-style transitions ("Background context from earlier in the conversation") preserve the metadata while maintaining discourse cohesion — the LLM reads a flowing narrative rather than a form with fields.

### What We Considered But Didn't Adopt

| Direction | Why not |
|-----------|---------|
| **Evaluation function** (narrative "tellability") | Introduces value judgments that conflict with information fidelity |
| **Speech presentation gradient** (NRSA → IS → FIS → DS → FDS) | Engineering complexity is high, and direct speech for recent messages already provides sufficient fidelity |
| **Temporal span annotation** | No clear evidence of benefit in real usage scenarios |
| **Transitivity guidance** for character settings | Requires UI changes beyond the current scope |

### The Key Constraint

Narrative techniques for LLM prompts must preserve **information fidelity**. We can change *how* information is presented (focalization, cohesion), but not *how* it's interpreted (evaluation, judgment). This is the boundary between narrative engineering and creative writing.

---

## Part 2: User State Inference

### Theory

We build on four frameworks from pragmatics and cognitive science:

| Framework | Contribution |
|-----------|-------------|
| **Theory of Mind** (Premack & Woodruff, 1978; Baron-Cohen et al., 1985) | Understanding the user's goals and knowledge gaps |
| **Relevance Theory** (Sperber & Wilson, 1986) | Estimating the user's cognitive environment to determine what information is relevant |
| **Cooperative Principle** (Grice, 1975) | Deciding how much and what type of information to provide |
| **Cognitive Load Theory** (Sweller et al., 1998) | Optimizing information presentation to avoid overwhelming the user |

### The Three-Dimensional Model

We model user state along three dimensions. Intent type is *not* a separate dimension — it's implicitly derived from purpose + physical situation.

| Dimension | What it captures | How it affects the response |
|-----------|-----------------|---------------------------|
| **Emotion** | calm / anxious / frustrated / urgent / curious | Tone and empathy level |
| **Purpose** | seek_help / seek_advice / seek_confirmation / express_feeling / casual_chat | Content direction and depth |
| **Physical situation** | Free text (e.g. "at work on desktop", "late evening on mobile") | Constraints and format |

**Why intent is implicit**: The same utterance carries different intents depending on context. "Is this approach viable?" means *seeking advice* when a discussion just started, but *seeking confirmation* when a decision is near. Physical situation + purpose jointly constrain intent — modeling it separately would be redundant.

### How It Works

```
1. Infer user state from recent messages
   (independent LLM call with structured output)

2. Convert to natural language
   "The user appears frustrated, is seeking help with a problem,
    and is likely at work on desktop."

3. Inject into the instruction area of the prompt
   (after the narrative sections, before the response directive)
```

**Why structured output → natural language, not XML tags**: The prompt is a narrative. Dropping `<emotion>frustrated</emotion>` into the middle of it breaks the flow that focalization and cohesion carefully built. Instead, we use `with_structured_output(PersonState)` to constrain the inference LLM's output, then convert the result to a natural language sentence that fits seamlessly into the instruction area.

**Why a separate LLM call, not combined with narrative generation**: The inputs differ (summary+segments vs. recent messages), the logic differs (storytelling vs. psychological inference), and the output formats differ (free text vs. structured enumeration). Forcing both into one prompt risks doing neither well.

**Why no persistence**: User state is ephemeral by nature — it changes with every message. Persisting it across turns would propagate inference errors and serve stale state. Each inference starts fresh from the current conversation.

### Cost and Resilience

| Aspect | Design |
|--------|--------|
| Latency | Runs in parallel with narrative generation (`asyncio.gather`), zero added latency |
| Token cost | ~250 input + ~50 output tokens per inference |
| Failure mode | Returns `None` on error; the prompt simply omits the user state instruction — the conversation continues normally |

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│                   Prompt Assembly                   │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │ Narrative Area                                │  │
│  │                                               │  │
│  │  [Your Character]                             │  │
│  │  Background context from earlier...           │  │
│  │  ───                                          │  │
│  │  Recent conversation...                       │  │
│  │  ───                                          │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │ Instruction Area                              │  │
│  │                                               │  │
│  │  The user appears [emotion], is [purpose],    │  │
│  │  and is likely [situation].                   │  │
│  │  Adjust your response accordingly.            │  │
│  │                                               │  │
│  │  Please respond directly to the user...       │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
└─────────────────────────────────────────────────────┘
```

The narrative area uses internal focalization and cohesive transitions to create immersion. The instruction area uses natural language user state to guide response strategy. The two areas are structurally separated — narrative content flows without interruption, while strategic instructions are clearly delineated.

---

## References

- Toolan, M. (2001). *Narrative: A Critical Linguistic Introduction*. Routledge.
- Grice, H. P. (1975). Logic and conversation. In *Syntax and Semantics, Vol. 3*.
- Sperber, D., & Wilson, D. (1986). *Relevance: Communication and Cognition*. Blackwell.
- Sweller, J., van Merriënboer, J. J. G., & Paas, F. G. W. C. (1998). Cognitive architecture and instructional design. *Educational Psychology Review, 10*(3).
- Premack, D., & Woodruff, G. (1978). Does the chimpanzee have a theory of mind? *Behavioral and Brain Sciences, 1*(4).
- Baron-Cohen, S., Leslie, A. M., & Frith, U. (1985). Does the autistic child have a "theory of mind"? *Cognition, 21*(1).
