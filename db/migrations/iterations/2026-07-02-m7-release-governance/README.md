# 2026-07-02 M7 Release Governance Migration

This iteration adds the R1 data foundation for M7 release governance.

- `business/0001_business_release_governance.*.sql` creates release batches, migration jobs, contract fixture runs, runtime health metrics, and operational incidents in the Business DB.
- `agent/0001_agent_release_audit.*.sql` creates release audit and runtime health snapshot tables in the Agent DB.

All table relationships are stored as text identifiers. No database-level foreign keys are created.
