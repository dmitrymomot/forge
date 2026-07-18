// Package rbac is role-based access control: predefined roles with permission
// grants, inheritance (nesting + multi-parent), out-of-hierarchy standalone
// roles, and wildcard grants. It resolves a subject's role names into an
// effective permission set (Can), reports inheritance-aware role membership
// (HasRole), and plugs into the auth/access decision seam as one Decider —
// Allow when a role grants the action, else Abstain (never Deny; acl/abac
// layer on top). Runtime subject→role assignments live behind a storage-
// agnostic Store (in-memory built in; rbac/pgstore for Postgres), scoped by an
// optional WithScope tenancy hook that fails closed.
//
// Roles reach the decider automatically: guard.Identity.Roles →
// access.Subject.Roles → rbac.Decider(rs, rbac.FromSubject()). Gate routes with
// access.RequirePermission and toggle views with access.Can.
package rbac
