# Security

SiegeIQ Sync is deliberately narrow in what it can touch.

- It **reads files only** — it never reads or writes the Rainbow Six Siege game process, its memory,
  or its network traffic. It is irrelevant to BattlEye.
- It watches exactly one directory: `Documents\My Games\Rainbow Six - Siege\<id>\MatchReplay`.
- It uploads only `.rec` replay files, to SiegeIQ, over HTTPS.
- Its device token is **upload-only** and can be revoked at any time from your SiegeIQ profile.

## Reporting a vulnerability

Please open a **private security advisory** on this repository (Security → Report a vulnerability),
or contact us through [siegeiq.gg](https://siegeiq.gg). Please don't file a public issue for a
security matter. We'll respond as quickly as we can.
