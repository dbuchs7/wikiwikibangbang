# Security policy

wikiwikibangbang is an early prototype with no supported release yet. Security findings are welcome all the same: the project's rules on authentication, permissions and content rendering (ADR-0013, ADR-0014, ADR-0022) are meant to hold from the start.

## Reporting a vulnerability

Report it privately through GitHub's **private vulnerability reporting**: the *Security* tab of this repository → *Report a vulnerability*. Do not open a public issue.

Examples of what to report this way:

- seeing an item, or anything about it (its title, key or existence), that your role should not let you see;
- changing something your role should not let you change, including through the MCP adapter;
- content that runs as a script or turns into markup in the browser;
- a password, session or API token that appears in a log, a response or the data directory in plain text.

Please include the version (`wikiwikibangbang version`) and the steps to reproduce. Reports are acknowledged as soon as the maintainer sees them; there is no guaranteed response time while the project is a prototype.
