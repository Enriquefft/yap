yap is a lightweight voice-to-text tool that runs as a daemon with near-zero idle footprint. Hold a hotkey to record, release to transcribe, and watch your text appear exactly where you're typing.

## Development Philosophy

### 1. Decision quality is the constraint, not dev time
One person orchestrating agent swarms equals 10+ senior engineers in parallel. Labor is solved; the bottleneck is *what to build and how to structure it*. The only valid reason to simplify is that simpler is *better*, not *faster*.

### 2. Perfection is the target.
Not an MVP. The thing that makes someone say: *"this is how it should have always worked."* Own the critical path, every byte of it. Dependencies belong on the non-critical path. The hard parts are where the value lives.

### 3. Single source of truth, always.
One canonical representation for every piece of state, type, and contract. Duplication is decay, if the same fact lives in two places, one is already wrong. Schemas, types, configs, and docs derive from one origin.

### 4. Zero workarounds.
No bandaids, no hacks, no "fix later" TODOs. A workaround shipped is a workaround owned forever. If something is hard to do right, do it right anyway.

### 5. Production-ready is the floor.
Robust, long-term correct, scalable only. Enterprise-grade isn't a milestone, it's the baseline. Everything above is the actual work. If something is hard to do right, do it right anyway.
