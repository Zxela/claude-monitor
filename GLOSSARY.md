# claude-monitor

A live and historical view of Claude Code activity: what each conversation is doing, what it costs, and when it needs a human.

## Language

### Sessions

**Session**:
One Claude Code transcript, whether a human started it or an agent spawned it.
_Avoid_: Conversation, run, agent (as the umbrella term)

**Top-level session**:
A Session started by a human, with no parent.
_Avoid_: Root session, main session, parent session (when you mean top-level)

**Subagent**:
A Session spawned by another Session to do delegated work.
_Avoid_: Child agent, team agent

**Workflow agent**:
A Session spawned as one step of a Workflow.

**Workflow**:
One Claude Code workflow run, grouping every Workflow agent it spawned.

**Parent session** / **Child session**:
The two ends of the spawning relationship between Sessions; the only relationship Sessions have with each other.

### Session state

**Active**:
A Session doing work: the model is generating or a tool is running.
_Avoid_: Using "active" for "had an Event recently"

**Awaiting user**:
A Session whose last turn ended and which is waiting for a human to reply. It stays Awaiting user, however long the wait, until someone replies.
_Avoid_: Waiting (ambiguous), idle

**Awaiting children**:
A Parent session doing no work itself while its Child sessions are still running.
_Avoid_: Waiting (ambiguous)

**Ended**:
A Session that Claude Code has reported as finished. An Ended Session is never Awaiting user and never Needs attention.
_Avoid_: Using "ended" for "no recent Events"; silence alone does not end a Session

**Abandoned**:
A Session Claude Code never reported as finished that has been silent for hours. Treated like Ended for attention, but kept distinct because the end was inferred.
_Avoid_: Idle, stale

**Needs attention**:
A Session that is Awaiting user or has errors the human hasn't seen yet. Awaiting children never counts.

### Cost

**Burn rate**:
How fast money is being spent right now, summed over the Sessions in view. A Session that has gone quiet contributes nothing, however much it spent earlier.
_Avoid_: Cost rate (for a lifetime average), velocity

### Activity

**Event**:
Any single line recorded from a Session's transcript.
_Avoid_: Entry, log line

**Message**:
An Event that is a conversation turn from the user or the assistant.
_Avoid_: Using "message" for hooks, progress or metadata Events
