# Session end comes from a SessionEnd hook, with a long-timeout fallback

A transcript never says when a Session is over, so a Session that is Awaiting user would otherwise need attention forever once its terminal closes. We take the end from Claude Code's `SessionEnd` hook, installed by `claude-monitor hook install` alongside `SessionStart`. Sessions that never get the signal (no hook, containers, history from before the hook) become Abandoned after hours of silence.

## Considered Options

- **Short inactivity timeout** (the previous 30s rule): cleared the attention flag while the human was still away, which is when it matters most.
- **Human dismisses it**: no false clears, but every closed terminal leaves a Session the human has to tidy up by hand.
- **Long timeout only**: simple, but a Session someone closed keeps the badge up for hours.

## Consequences

Session lifecycle now depends on a hook the user has to install, so the dashboard must stay correct without it and treat Abandoned as a guess, not a fact.
