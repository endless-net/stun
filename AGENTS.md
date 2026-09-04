# Agents

## Git workflow

- Work on a short-lived branch and submit every change through a pull request
  targeting `main`. Do not push changes directly to `main`.
- Format every commit message according to Conventional Commits, for example
  `feat: ...`, `fix: ...`, `docs: ...`, or `chore: ...`.

## Version increases

- Never increase any version or generation number, including schema, configuration,
  API, protocol, contract, manifest, migration, artifact, or rollout versions,
  without the user's direct explicit permission for that exact increase.
- A request to implement, refactor, fix, remove compatibility, or make a breaking
  change does not authorize a version increase. Without explicit permission, keep
  the current version number.
