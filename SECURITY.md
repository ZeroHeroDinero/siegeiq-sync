# Security

SiegeIQ Sync is deliberately narrow in what it can touch.

- It **never touches the game** — it does not read or write the Rainbow Six Siege process, its
  memory, or its network traffic, and it injects nothing into it. It is irrelevant to BattlEye.
- It watches exactly one directory: `Documents\My Games\Rainbow Six - Siege\<id>\MatchReplay`.
- It uploads only `.rec` replay files, to SiegeIQ, over HTTPS.
- It **records the screen** while Siege is running, using FFmpeg through the standard Windows screen
  capture interfaces (DXGI Desktop Duplication, or GDI window capture). This is the same class of
  capture OBS and ShadowPlay use, performed from outside the game.
- **Recordings are local by default.** Clips are written to `Videos\SiegeIQ` on your own machine.
  Nothing is uploaded unless you send a specific clip, or turn automatic sending on yourself.
- Uploaded clips are held for **30 days** and then deleted.
- Its device token is **upload-only** and can be revoked at any time from your SiegeIQ profile.

## Reporting a vulnerability

Please open a **private security advisory** on this repository (Security → Report a vulnerability),
or contact us through [siegeiq.gg](https://siegeiq.gg). Please don't file a public issue for a
security matter. We'll respond as quickly as we can.
