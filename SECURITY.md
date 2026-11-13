# Security

## How Iddler handles your data

- **Passwords** are sent only to Steam, encrypted with Steam's RSA key, and are never written to disk.
- **Session tokens** and `.maFile` secrets are encrypted at rest: with the Keychain on macOS and DPAPI on Windows.
  On Linux they are encrypted with a local key, and every file is readable only by your user.
- **The interface** listens on `127.0.0.1` only and requires a random key that changes with every launch.
  The optional phone panel for your local network is off until you turn it on.
- Iddler sends nothing anywhere except to Steam, and to Telegram if you set up your own bot.

## Reporting a vulnerability

Please do not open a public issue for security problems.
Write to [@excruciation69 on Telegram](https://t.me/excruciation69) with a description and steps to reproduce.
You will get an answer within a few days, and a fix will be released as soon as possible.
