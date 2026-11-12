---
type: Reference
title: Data ownership, CQRS, row- and column-level security, and tenant isolation
description: What database per service, CQRS, row-level and column-level security, and the silo, bridge and pool tenant-isolation models are, when each pays off, what they cost, how they map to the constitution, and the questions a hosted feature must answer.
tags: [references, data, tenancy, security, cqrs, postgresql]
status: draft
stale_after: 2027-11-12T00:00:00Z
sources:
  - id: aws-db-per-service
    resource: https://docs.aws.amazon.com/prescriptive-guidance/latest/modernization-data-persistence/database-per-service.html
    title: Database-per-service pattern (AWS Prescriptive Guidance)
  - id: ms-data-considerations
    resource: https://learn.microsoft.com/en-us/azure/architecture/microservices/design/data-considerations
    title: Data considerations for microservices (Azure Architecture Center)
  - id: ms-cqrs
    resource: https://learn.microsoft.com/en-us/azure/architecture/patterns/cqrs
    title: CQRS pattern (Azure Architecture Center)
  - id: pg-rls
    resource: https://www.postgresql.org/docs/current/ddl-rowsecurity.html
    title: Row Security Policies (PostgreSQL documentation)
  - id: pg-grant
    resource: https://www.postgresql.org/docs/current/sql-grant.html
    title: GRANT (PostgreSQL documentation)
  - id: ms-sql-rls
    resource: https://learn.microsoft.com/en-us/sql/relational-databases/security/row-level-security
    title: Row-level security (SQL Server and Azure SQL, Microsoft Learn)
  - id: aws-rls-blog
    resource: https://aws.amazon.com/blogs/database/multi-tenant-data-isolation-with-postgresql-row-level-security/
    title: Multi-tenant data isolation with PostgreSQL Row Level Security (AWS Database Blog, 2020-05-18)
  - id: gcp-bq-cls
    resource: https://docs.cloud.google.com/bigquery/docs/column-level-security-intro
    title: Introduction to column-level access control (BigQuery, Google Cloud)
  - id: aws-saas-lens-models
    resource: https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/silo-pool-and-bridge-models.html
    title: Silo, pool, and bridge models (AWS Well-Architected SaaS Lens)
  - id: aws-saas-lens-isolation
    resource: https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/tenant-isolation.html
    title: Tenant isolation (AWS Well-Architected SaaS Lens)
  - id: aws-saas-fundamentals
    resource: https://docs.aws.amazon.com/whitepapers/latest/saas-architecture-fundamentals/tenant-isolation.html
    title: Tenant isolation (SaaS Architecture Fundamentals whitepaper, marked historical by AWS)
  - id: ms-mt-storage
    resource: https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/approaches/storage-data
    title: Architectural approaches for storage and data in multitenant solutions (Azure Architecture Center)
  - id: ms-mt-postgresql
    resource: https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/service/postgresql
    title: Azure Database for PostgreSQL in a multitenant solution (Azure Architecture Center)
  - id: owasp-mt
    resource: https://cheatsheetseries.owasp.org/cheatsheets/Multi_Tenant_Security_Cheat_Sheet.html
    title: Multi-Tenant Security Cheat Sheet (OWASP)
  - id: constitution
    resource: ../../.specify/memory/constitution.md
    title: Agentic Platform Constitution
  - id: plan-001
    resource: ../../specs/001-prose-to-accepted-slice/plan.md
    title: Feature 001 implementation plan
generated: { by: claude-code/claude-opus-5-5, at: 2026-11-12T12:00:00Z }
---

# Summary

Store ownership, CQRS, row- and column-level security, and tenant-isolation models, as the
sources describe them and as they map to the [constitution](../../.specify/memory/constitution.md).
Nothing here is a decision; the last section lists the questions a hosted feature must answer.

# Data ownership: database per service

**What it is.** Each service manages its own private data store, and other services reach the
data only through that service's API. AWS: "microservices don't share a data layer", "individual
data stores cannot be directly accessed by other microservices, and persistent data is accessed
only by APIs". Microsoft: "Two services shouldn't share a data store." Microsoft adds that
services "can safely share the same physical database server"; problems start when they share a
schema or the same tables.

**When it pays off.** AWS lists three triggers: loose coupling is required, services have
different compliance or security requirements for their data, or scaling needs finer control.
Microsoft's reason is change scope: a shared schema makes every schema change a coordinated
change across services.

**Costs and pitfalls.**

- Cross-service transactions and queries become hard; AWS says queries then need API
  composition (which CQRS can speed up) or event sourcing. More databases to run (AWS).
- Data gets duplicated; Microsoft says to prefer eventual consistency where possible, keep "a
  single source of truth" per entity behind an API, and publish a schema for events.

**Constitution.** Principle V already requires it: components "MUST NOT reach into another
component's store". "Values have one home" says managed things "live in the owning component's
store", which matches Microsoft's single source of truth. Principle III's published schemas and
the contract-first rule cover the event schemas Microsoft recommends.

Sources: [Database-per-service pattern (AWS Prescriptive Guidance)](https://docs.aws.amazon.com/prescriptive-guidance/latest/modernization-data-persistence/database-per-service.html); [Data considerations for microservices (Azure Architecture Center)](https://learn.microsoft.com/en-us/azure/architecture/microservices/design/data-considerations); [Agentic Platform Constitution](../../.specify/memory/constitution.md).

# CQRS

**What it is.** Command Query Responsibility Segregation separates write operations (commands)
from read operations (queries) into different models. Commands "should represent specific
business tasks"; queries "never alter data" and return DTOs. The models can share one store or
use separate stores, in which case the write side usually publishes events that refresh the read
side.

**When it pays off.** Microsoft lists: collaborative editing of the same data, task-based user
interfaces or complex domains, reads that must be tuned separately from writes ("especially
beneficial when the number of reads is greater than the number of writes"), separate teams for
the two sides, evolving systems, and integration with other subsystems. It "might not be
suitable when" the domain or business rules are simple, or CRUD-style access is enough.

**Costs and pitfalls.**

- "Significant complexity", especially combined with event sourcing.
- With separate stores, reads are eventually consistent and may be stale; the user acting on
  stale data must be handled.
- Messaging brings failures, duplicates and retries; Microsoft recommends a Transactional
  Outbox and an idempotent read-model consumer.

**Constitution.** Principle IV already separates the two at the API: state-changing operations
are accepted, recorded, queued and run by workers, while "Queries MAY be answered
synchronously". That is command/query separation at the interface, not a requirement for
separate read models or stores. A read store fed by events is a copy, so the write store stays
the one home of each value.

Sources: [CQRS pattern (Azure Architecture Center)](https://learn.microsoft.com/en-us/azure/architecture/patterns/cqrs); [Database-per-service pattern (AWS Prescriptive Guidance)](https://docs.aws.amazon.com/prescriptive-guidance/latest/modernization-data-persistence/database-per-service.html); [Agentic Platform Constitution](../../.specify/memory/constitution.md).

# Row-level security

**What it is.** The database filters which rows each query can see or change, by a policy on the
table. In PostgreSQL, after `ALTER TABLE ... ENABLE ROW LEVEL SECURITY`, all normal access "must
be allowed by a row security policy"; with no policy, "a default-deny policy is used". `USING`
selects the visible rows, `WITH CHECK` the rows that may be written (it defaults to `USING`);
permissive policies combine with `OR`, restrictive ones with `AND`. SQL Server and Azure SQL have
the same feature as filter and block predicates, and name multitenant separation as a use case.

**When it pays off.** In a shared (pool) store, where tables hold every tenant's rows. AWS: it
centralizes isolation "at the database level" instead of relying on the correct `WHERE` clause
in every statement. Microsoft: the restriction runs "every time that data access is attempted
from any tier".

**Costs and pitfalls.**

- Bypass: PostgreSQL superusers and `BYPASSRLS` roles always bypass it; table owners do unless
  the table has `FORCE ROW LEVEL SECURITY`. AWS and OWASP: the application must not connect as
  the owner or as such a role.
- Tenant context: a policy that compares the tenant column with a setting such as
  `current_setting('app.current_tenant')` scales better than one database role per tenant (AWS).
  Session-scoped settings can leak to the next request on a pooled connection; OWASP says to set
  the context per transaction (`SET LOCAL`), fail closed when it is missing, and never fall back
  to an unscoped query. AWS warns that session variables may be incompatible with server-side
  poolers such as pgBouncer.
- Side channels: PostgreSQL's unique and foreign-key checks always bypass row security, so
  schemas must avoid "covert channel" leaks; Microsoft shows error-based leaks (divide by zero)
  and features that see unfiltered data (statistics, change data capture, history tables).
- Microsoft: propagating the tenant into every query "can be complex to design, implement,
  test, and maintain", so "many multitenant solutions don't use row-level security"; policies may
  also need extra indexes and performance tests.
- Testing: OWASP says to test through the application's own role, connection path and pooling,
  fail a test if that role is superuser or `BYPASSRLS`, fail when a new table has no RLS or
  policy, and prove a reused connection does not carry the previous tenant.

**Constitution.** Principle XII names row-level security as one accepted way to isolate
retrieval and memory "in the store itself", requires every store query to be scoped by tenant,
the tenant to come from the authenticated identity, and a cross-tenant test for every API.
OWASP's test list is a concrete form of that last rule. Cross-tenant roles must be separate and
audited, which is the `BYPASSRLS`/owner question in PostgreSQL terms.

Sources: [Row Security Policies (PostgreSQL documentation)](https://www.postgresql.org/docs/current/ddl-rowsecurity.html); [Row-level security (SQL Server and Azure SQL, Microsoft Learn)](https://learn.microsoft.com/en-us/sql/relational-databases/security/row-level-security); [Multi-tenant data isolation with PostgreSQL Row Level Security (AWS Database Blog)](https://aws.amazon.com/blogs/database/multi-tenant-data-isolation-with-postgresql-row-level-security/); [Multi-Tenant Security Cheat Sheet (OWASP)](https://cheatsheetseries.owasp.org/cheatsheets/Multi_Tenant_Security_Cheat_Sheet.html); [Architectural approaches for storage and data in multitenant solutions (Azure Architecture Center)](https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/approaches/storage-data); [Azure Database for PostgreSQL in a multitenant solution (Azure Architecture Center)](https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/service/postgresql); [Agentic Platform Constitution](../../.specify/memory/constitution.md).

# Column-level security

**What it is.** Access control on individual columns rather than whole tables or rows.
PostgreSQL grants `SELECT`, `INSERT`, `UPDATE` and `REFERENCES` per column. BigQuery attaches a
policy tag from a taxonomy to a column; reading it needs the Fine-Grained Reader role, enforced
"in addition to existing dataset ACLs", with optional dynamic data masking. SQL Server's RLS
example uses a column permission (`DENY UPDATE` on the tenant column) so a row cannot be moved to
another tenant.

**When it pays off.** When some fields need narrower access than the rest of the record. The
sources describe the mechanism, not criteria for choosing it.

**Costs and pitfalls.**

- PostgreSQL: a table-level grant covers every column, and revoking one column afterwards "will
  not do what one might wish"; the table-level privilege must not be granted at all.
- Per-tenant encryption keys (customer-managed keys, Always Encrypted) are a stronger, separate
  mechanism; Microsoft and OWASP treat them as a reason to isolate tenants, not as column grants.

**Constitution.** Principle XI requires captured protected content to be "stored separately with
its own access control and retention, and referenced from telemetry rather than embedded in it".
That reads as a separate store, not a protected column in a shared one; column-level control
could only add to it. Principle V's split of management and data permissions is the role design
any column grant would follow.

Sources: [GRANT (PostgreSQL documentation)](https://www.postgresql.org/docs/current/sql-grant.html); [Introduction to column-level access control (BigQuery, Google Cloud)](https://docs.cloud.google.com/bigquery/docs/column-level-security-intro); [Row-level security (SQL Server and Azure SQL, Microsoft Learn)](https://learn.microsoft.com/en-us/sql/relational-databases/security/row-level-security); [Architectural approaches for storage and data in multitenant solutions (Azure Architecture Center)](https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/approaches/storage-data); [Multi-Tenant Security Cheat Sheet (OWASP)](https://cheatsheetseries.owasp.org/cheatsheets/Multi_Tenant_Security_Cheat_Sheet.html); [Agentic Platform Constitution](../../.specify/memory/constitution.md).

# Tenant-isolation models

**What they are.** AWS's SaaS Lens: **silo** gives a tenant dedicated resources (a stack or a
database); **pool** shares resources across tenants; **bridge** is a mix, where some services
are siloed and others pooled. Applied to one PostgreSQL store, AWS's database blog uses bridge
for a schema per tenant on a shared instance, silo for an instance per tenant, and pool for
shared tables with a tenant key. Microsoft names the same choices shared multitenant database,
sharding, and dedicated database per tenant. Isolation is not authentication: a user "could be
authenticated and authorized, and still access the resources of another tenant" (AWS).

**When each pays off.**

| Model | Pays off when | Costs |
|-------|---------------|-------|
| Pool (shared tables + tenant key, RLS) | Many tenants, lowest cost; Microsoft: "highest density", single resource to manage | Scale limits of one resource, noisy neighbours, per-tenant metering, one configuration for all tenants |
| Bridge (schema per tenant) | Some separation on shared infrastructure | OWASP: needs "disciplined grants, role separation, `search_path` handling, and migrations"; AWS: setup and maintenance "quite complicated" |
| Silo (database or instance per tenant) | Tenants need their own keys, backup and restore policies, or data location (Microsoft); compliance or regulation | Highest cost; automated provisioning is required; schema updates across many databases |

Microsoft: there is "no single solution", models can be mixed, and the design should stay as
simple as the requirements allow; avoid a table per tenant and manual schema changes. Restoring
one tenant from a shared database may mean restoring the whole database elsewhere.

**Constitution.** Principle XII lists namespace, collection or row-level security as acceptable
in-store isolation, so pool with RLS, bridge and silo all satisfy it if every query is scoped by
the authenticated tenant and cross-tenant tests exist. Runs of different tenants must not share
a process, sandbox, working directory or credentials, which is silo for compute whatever the
store model. Principle XI's per-tenant retention and erasure across every store is harder to
prove in a pool, per Microsoft's restore note.

Sources: [Silo, pool, and bridge models (AWS Well-Architected SaaS Lens)](https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/silo-pool-and-bridge-models.html); [Tenant isolation (AWS Well-Architected SaaS Lens)](https://docs.aws.amazon.com/wellarchitected/latest/saas-lens/tenant-isolation.html); [Tenant isolation (SaaS Architecture Fundamentals whitepaper)](https://docs.aws.amazon.com/whitepapers/latest/saas-architecture-fundamentals/tenant-isolation.html); [Multi-tenant data isolation with PostgreSQL Row Level Security (AWS Database Blog)](https://aws.amazon.com/blogs/database/multi-tenant-data-isolation-with-postgresql-row-level-security/); [Architectural approaches for storage and data in multitenant solutions (Azure Architecture Center)](https://learn.microsoft.com/en-us/azure/architecture/guide/multitenant/approaches/storage-data); [Multi-Tenant Security Cheat Sheet (OWASP)](https://cheatsheetseries.owasp.org/cheatsheets/Multi_Tenant_Security_Cheat_Sheet.html); [Agentic Platform Constitution](../../.specify/memory/constitution.md).

# What feature 001 does today

- One developer on one machine; Principle XII is "Not applicable" until the hosted version.
- One store: append-only JSON Lines run records and a content-addressed artifact store in a named
  volume, under `src/engine/store`. The engine API is the only management path, so the engine
  alone owns the run store; no other component reads it.
- No database, no RLS, no read models. The plan rejected SQLite as "not needed for one writer".

Sources: [Feature 001 implementation plan](../../specs/001-prose-to-accepted-slice/plan.md).

# Open questions for a hosted feature

- Which isolation model does each store use (run store, artifact store, memory, vector index,
  caches, trace store, captured protected content), and does any tenant class need silo?
- In a relational store, is it pool with row-level security, schema per tenant, or database per
  tenant? If RLS, how is the tenant context set per transaction with the chosen connection
  pooler, and which role, if any, may bypass it?
- How do per-tenant retention, erasure, backup and restore work under the chosen model?
- Does any data need column-level protection, or is all protected content in separate stores?
- Do any read models (run lists, dashboards, cross-tenant operator views) justify CQRS with a
  separate read store, and what staleness is acceptable?
- Which component owns each new store, and which API do the others use instead of reading it?
