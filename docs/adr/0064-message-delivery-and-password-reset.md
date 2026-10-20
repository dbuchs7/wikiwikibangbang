# ADR-0064: Message delivery over the account's channels, and the password reset by mail

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that messages reach people over mail, Slack, Microsoft Teams and other channels attached to their accounts, and for a password reset at the sign-in page, 2026-10-20)

## Context and problem statement

Accounts carry channels (ADR-0061), but nothing is sent. People learn of changes only when they open the product (ADR-0046); administrators learn of account requests only in the administration; a forgotten password needs an administrator. What is sent, over which channel, how is a channel reached safely, and how does a forgotten password get reset?

## Decision drivers

- A channel left empty is not used.
- A message is a pointer, not a copy: content stays behind the sign-in and its permission checks (D10); a mailbox or chat channel may be read by others and kept forever.
- A webhook address is the user's input: posting to it must not let anyone reach into the server's network (ADR-0039).
- A password reset must not tell who has an account, must not be usable by whoever reads a shared chat channel, and must end what the old password opened.
- Configuration stays in the environment, like single sign-on (ADR-0030); secrets never in the journals.

## Considered options

1. **A digest of notifications plus event messages, over all channels; reset links by mail only; tokens in memory.**
2. **One message per notification, as it happens.**
3. **Reset links over any channel; tokens persisted.**

## Decision outcome

Chosen option: **1**.

- **Configuration:** `WWBB_SMTP_HOST`, `_PORT`, `_SECURITY` (`starttls` by default and enforced; `tls`; `none` for a relay, where the password login is refused unless the host is local), `_USERNAME`, `_PASSWORD`, `_FROM`; `WWBB_PUBLIC_URL` for links; `WWBB_DIGEST_MINUTES` (15; 0 off); `WWBB_WEBHOOKS_PRIVATE=on` to allow webhooks in private networks (never loopback or link-local). A misconfiguration stops the start. Administrators see the configuration, without secrets, at `/api/settings/delivery`.
- **Channels:** mail through the SMTP server (plain text, UTF-8, `Auto-Submitted: auto-generated`, header values from parsed addresses and an encoded subject); Slack incoming webhooks (`text`); Teams workflows and incoming webhooks (an Adaptive Card); any other webhook (JSON with `subject`, `text`, `link`). Webhooks use the link check's transport: the address actually connected to must be public, checked after name resolution; no proxy; no redirects; 15 s; errors never repeat the address, which is a credential.
- **What is sent:**
  - **Digest:** every interval, to each person with a channel, the number of new notifications since the later of their last digest and their last look, with a link to the notifications. The position is journaled (`DigestSent` in the accounts journal) so a restart neither repeats nor loses one. The first round for a person only marks where they stand.
  - **Account requests:** administrators are told of a new request; the requester is mailed the decision, in the deciding administrator's language.
  - **Test message:** a person sends one to all their channels from the account page, at most every 30 s; the outcome per channel is shown there with the last message's.
  - Texts are in the person's UI language (English or German).
- **Password reset:** *Forgot your password?* appears only with mail and a public address. It answers the same whether the account exists or not, and sends in the background, so neither the answer nor its timing tells. The link goes **by mail only** and carries a 256-bit token in the address's fragment (no server log, no referrer). A token is kept in memory as its hash, one per account, valid 30 minutes and once; a new password ends every session. Requests are throttled per address and per username (ADR-0013). Administrators can make a link valid for 24 hours to hand over, for accounts without mail. Each step is audited.

### Consequences

- **Positive:** people are reached where they work, without content leaving the product.
- **Positive:** an installation without configuration behaves as before.
- **Negative / trade-offs accepted:** a restart invalidates reset links; one asks again.
- **Negative / trade-offs accepted:** spamming resets for a username throttles that username's resets for the window, as failed logins do.
- **Negative / trade-offs accepted:** an unreachable channel is not retried; the next digest counts everything since the last one, and the person sees the failure on their account page.
- **Negative / trade-offs accepted:** whether Microsoft accepts the card on a given Teams address is not verified here; the test message shows it.

## Pros and cons of the options

### Option 2: one message per notification

Strongest form: immediate.

- Bad: a busy page floods every channel; a digest with a link is enough to bring people back.

### Option 3: reset links over any channel, persisted tokens

Strongest form: works without a mail server and survives restarts.

- Bad: a Slack or Teams channel may be shared; whoever reads it could take over the account. A persisted token is a password equivalent in the journal and the backups.

## Links

- relates to ADR-0013 (accounts and throttle), ADR-0030 (configuration from the environment), ADR-0039 (public addresses only), ADR-0046 (notifications), ADR-0061 (channels on accounts)
