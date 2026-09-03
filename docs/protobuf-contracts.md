# Protobuf API contracts

This repository is prepared to own a versioned protobuf API contract. No
contract is declared until the owning service accepts an API task.

## First contract task

1. Add the source schema under `proto/stun/v1/`; package names and the
   Go `go_package` option must be versioned.
2. Add `buf.gen.yaml` with local Go generation. Generate into the service-owned
   API module: `stunapi` for this repository.
3. Add a generated-artifacts CI job that runs `buf generate` and rejects a
   generated-file diff.
4. Run `buf lint` and `buf build`. Do not add dependencies until the schema
   imports them; after adding one, run `buf dep update` and commit `buf.lock`.
5. After the first accepted release, create
   `contracts/proto-baseline/stun.binpb` with
   `buf build proto -o contracts/proto-baseline/stun.binpb`. Commit both.
   Subsequent changes are compared with that immutable released baseline.

The CI workflow is advisory by design: it publishes lint, build, and breaking
results without blocking a merge. A failed report still requires the contract
owner to decide whether to release a new API version or replace the baseline.
