# Forge

A Go framework for building B2B micro-SaaS applications with Rails-like developer experience but without magic.

## Why Forge?

**Problem:** Starting a new micro-SaaS requires the same boilerplate every time — auth, multi-tenancy, billing, background jobs, emails. Copy-pasting from previous projects is error-prone and tedious.

**Solution:** A template repository with production-ready code and pre-built features. Clone it, own it completely, modify anything.

## Goals

- **Maximum scaffolding, minimum runtime magic** — explicit code, no reflection
- **Convention over configuration** — sensible defaults, escape hatches when needed
- **B2B SaaS focused** — multi-tenancy, team management, billing built-in
- **SSR-first** — optimized for server-rendered apps with templ/html templates

## Tech Stack

| Layer      | Technology              |
| ---------- | ----------------------- |
| Language   | Go 1.25+                |
| Database   | PostgreSQL (pgx/v5)     |
| Migrations | Goose                   |
| Router     | chi/v5                  |
| Queue      | River (Postgres-native) |
| Config     | caarlos0/env (env vars) |
| Templates  | templ                   |

## Status

**Concept stage** — this document describes the planned architecture.

## Inspiration

- [Loco](https://loco.rs/) (Rust) — Rails-like framework
- [Rails](https://rubyonrails.org/) — convention over configuration
