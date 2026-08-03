# Preserve deep module ownership for v0 Turn execution

**Status:** accepted

As v0 adds Tool continuation, complete Session history, Approval, Validation, and Git safety, Deku will preserve the Agent as the primary deep module and keep the CLI out of Turn orchestration. Implementation proceeds in this order: protect the Agent seam, complete the Session Transcript, deepen Tool execution, then deepen Repository safety; the Repository Map remains part of Prompt assembly inside the Agent. This concentrates leverage at the Agent seam, keeps Provider wire types out of Session, and gives Git safety one Repository module rather than scattered policy.

## Considered options

- Let the CLI or individual callers coordinate Provider Steps, Tool results, Approval, and Git operations. Rejected because it creates multiple shallow orchestration paths and spreads safety policy.
- Persist only role/content messages and reconstruct Tool history from Provider data. Rejected because resume would lose the complete Transcript and couple Session to a Provider adapter.
- Put tool-name dispatch and argument handling directly in Agent. Rejected because Tool definitions, execution, and normalized results would drift across the Turn loop.
- Scatter Git status, dirty-tree choices, snapshots, Validation, and commit selection across Agent and CLI. Rejected because change attribution and pre-existing work protection would lack one deep owner.

## Consequences

The Agent remains the primary integration test seam. Session persists domain Transcript entries rather than Provider wire types. Tool owns built-in Tool execution while Edit and Approval retain their focused domain rules. Repository owns Git state transitions and Agent-owned change attribution; v0 does not add a general Repository interface for its single Git implementation.
