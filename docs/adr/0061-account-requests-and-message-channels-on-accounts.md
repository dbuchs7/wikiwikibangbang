# ADR-0061: Account requests at the sign-in page and message channels on accounts

- **Status:** Accepted
- **Implementation:** Landed
- **Date:** 2026-10-20
- **Deciders:** Project initiators (the maintainer asked that people can ask for an account at the sign-in page, that accounts carry a mail address, and that further channels such as Slack and Microsoft Teams can be attached to an account, an empty one being not available, 2026-10-20)

## Context and problem statement

Accounts are created by administrators only (ADR-0013). People who need one must reach an administrator some other way, and the administrator must choose and hand over a first password. Accounts also carry no way to reach their owner: notifications (ADR-0046) are seen only inside the product. How does someone ask for an account, and where does an account keep the channels a message could take?

## Decision drivers

- No account comes into being without an administrator (ADR-0013); asking is not having.
- No password is handed over: the requester chooses it.
- A public endpoint is an attack surface: its cost and its memory are bounded (ADR-0015).
- The installation decides whether it takes requests at all.
- A channel left empty is a channel the user does not have; nothing is sent there.
- A webhook address is a credential: whoever has it can post to the channel.

## Considered options

1. **Requests are events of the accounts journal; an approval creates the account from the request; the requester sets the password at request time. Contacts are a field set of the account.**
2. **Self-registration: the account exists at once, unprivileged, and an administrator grants roles later.**
3. **Requests by mail to an address the installation publishes.**

## Decision outcome

Chosen option: **1**.

- **Opt in:** an administrator allows requests in Administration → Account requests (`AccessSet` in the site journal). While they are not allowed, the sign-in page offers nothing and `POST /api/signup` answers 403.
- **Request:** username, mail address (required), password (the usual rules) and a note of at most 500 characters. The password is hashed at once; the request (`AccountRequested`) keeps only the hash. A username held by an account or by a waiting request is refused.
- **Bounds:** one client address may ask `MaxFailures` times within `FailureWindow`, counted before the costly hash; at most `WWBB_BUDGET_ACCOUNT_REQUESTS` requests (100) wait at once, after that requests are refused with 503.
- **Decision:** administrators see the waiting requests in Administration and, as a dot on their account button, how many wait. Approving (`AccountRequestApproved`) creates the account with the requester's password hash and mail address; rejecting (`AccountRequestRejected`) drops the request and frees the username. Each step is audited (`account.request`, `account.approve`, `account.reject`, `access.change`). An approved account holds no role; the administrator grants what it needs as for any account.
- **Contacts:** an account carries `Contacts` (`ContactsSet`): a mail address, a Slack incoming webhook (on `hooks.slack.com`), a Microsoft Teams webhook and one other webhook, each https without credentials in the address. Empty is not available. Users set their own on the account page; administrators see every account's mail address and which channels it has, and may set the mail address, but never see webhook addresses. The audit trail names the channels changed, never their addresses.
- **Delivery** of messages over these channels, and a password reset by mail, are decided separately; this decision only gives them a place to go.

### Consequences

- **Positive:** nobody waits for an administrator to invent and transmit a password; the administrator only decides.
- **Positive:** an installation that does not want requests sees no change.
- **Positive:** channels are per user and optional; adding one more kind later is a field, not a model.
- **Negative / trade-offs accepted:** a requester learns whether a username is taken. Usernames are shown to every signed-in user as authors and are no secret; refusing silently would leave a requester waiting for an approval that cannot come.
- **Negative / trade-offs accepted:** the journal keeps a rejected request's username, mail address, note and password hash, as it keeps every event. Purging is the backup and retention tooling's concern, not this decision's.
- **Negative / trade-offs accepted:** the mail address is not verified; an administrator approving a request should know who is asking. Verification needs mail delivery and belongs with it.
- **Negative / trade-offs accepted:** a Teams address is checked only for being https: Microsoft offers incoming webhooks and workflow addresses on several hosts, and which ones are valid has changed over time. Where an address may be called is decided at delivery, against the addresses it resolves to (ADR-0039).

## Pros and cons of the options

### Option 2: self-registration

Strongest form: no waiting at all; least work for administrators; the account is useless until it receives a role, so little is exposed.

- Bad: any visitor gets an account, and everything readable by "any signed-in user" (user names, avatars, the build version) becomes public.
- Bad: accounts accumulate without anyone deciding.

### Option 3: requests by mail

Strongest form: nothing to build or secure in the product.

- Bad: the administrator still invents a password and hands it over; nothing is audited; nothing is bounded.

## Links

- relates to ADR-0013 (local accounts), ADR-0015 (budgets), ADR-0027 (accounts journal), ADR-0039 (public addresses only), ADR-0046 (notifications), ADR-0058 (data protection regulations)
