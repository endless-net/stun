# Security policy

Do not open a public issue for a suspected vulnerability. Report it privately through GitHub's **Security → Report a vulnerability** flow for `unng-lab/endlessnet-stun`, including affected versions, reproduction details, impact, and any suggested mitigation.

Maintainers should acknowledge a complete report within five business days, validate severity, prepare a fixed immutable release, and coordinate disclosure after users have a reasonable upgrade window. Never include client packet captures, public IP inventories, deployment credentials, private keys, or environment files in a report unless they have been safely redacted.

Only supported tagged releases receive security fixes. CI runs Go vulnerability analysis and container scanning, and Dependabot tracks Go modules, Actions, and container bases.
