# whatever-it-takes

A Claude Code skill: set up an acceptance gate before starting work, and keep working until it reports OK.

The gate can be a mechanical command (exit code decides pass/fail) or an independent claude code session acting as judge. Claude-mode results are signed with ed25519, so the working session cannot rewrite or casually fabricate them.

Full documentation is in Japanese: see [README.ja.md](README.ja.md).
