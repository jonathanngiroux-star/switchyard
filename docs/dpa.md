# Data Processing Addendum (DPA) — stub

**Status: TEMPLATE — not legal advice. Have counsel review before signing
with any customer. This stub exists so procurement sees the shape of the
document on day one.**

## 1. Scope

Switchyard (the "Processor") processes customer flag configuration data,
evaluation contexts, and provisioned identity records (the "Personal Data")
solely to provide the feature-flag control plane service.

## 2. Roles

- **Customer** = Controller. Customer determines what user attributes flow
  into evaluation contexts.
- **Switchyard** = Processor. We process on documented instructions only.

## 3. Subprocessors

| Subprocessor | Purpose | Location |
|---|---|---|
| Cloud infrastructure provider (TBD) | hosting | TBD |
| Transactional email provider (TBD) | operational notices | TBD |

Customer will be notified 30 days before any new subprocessor.

## 4. Data categories

| Category | Examples | Retention |
|---|---|---|
| Flag configuration | keys, rules, rollouts, values | life of account |
| Evaluation contexts | user keys + attributes sent by customer apps | not persisted by Switchyard core; evaluation is local in the customer's services |
| SCIM identities | user id, email, group membership | life of account + 30 days |
| Audit log | actor, action, resource, timestamp | 1 year default |

**The most important row is the second one:** evaluation happens in the
customer's own services — Switchyard's core design means user-level
evaluation data never needs to reach the Switchyard server. This is a
data-minimization feature, not an accident.

## 5. Security measures

Encryption in transit (TLS 1.2+), encryption at rest, least-privilege
access, audit logging per `docs/audit-log.md`, and the incident process in
Section 7.

## 6. Data location

TBD — single-region deployment. Customer may pin the region in the
Enterprise tier.

## 7. Breach notification

Notify the customer within 72 hours of becoming aware of a Personal Data
breach, with scope, impact, and remediation status.

## 8. Deletion

On account termination: full deletion of Personal Data within 30 days,
except where retention is legally required; a deletion certificate on
request.

## 9. Self-hosted deployments

For customers running the self-hosted binary: Switchyard the company
processes **no** customer data — the DDP is between the customer and their
own infrastructure. This document applies only to the hosted tier.
