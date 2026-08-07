# Superscience Workbench V2 Design Specification

## Design Reference

The generated contact sheet is stored at
`docs/design/prototypes/v2/superscience-laboratory-console-contact-sheet.png`.
It is a design reference only. The product must render native React controls and
must not use the bitmap as a background or interaction surface.

## Product Principles

- Production paths show only observed state. Missing credentials, unavailable
  connectors, and failed tools are explicit states, never substituted results.
- The primary interaction is a dense desktop scientific IDE. The minimum
  supported window is 1280 by 720; the reference window is 1440 by 900.
- Neutral white and graphite form the base palette. Teal means verified or
  active, amber means approval or degraded, and red means a concrete error.
- Panels use stable tracks, 4-6px radii, restrained borders, and compact type.
- Expensive scientific renderers load only when their artifact type is active.

## Screen Map

1. **Provider onboarding** blocks agent execution until a cloud provider and
   model pass a real authenticated health check. It exposes key storage,
   endpoint, model, validation progress, and actionable errors.
2. **Project launcher** lists real recent projects and readiness checks. Counts
   represent healthy capabilities, not catalog size.
3. **Idle workspace** uses session navigation, an agent composer, and a context
   inspector. Provider and model selection come from validated catalogs.
4. **Active run** shows an ordered plan, model turns, tool requests,
   observations, permissions, reviewer state, usage, and cancellation controls.
5. **Scientific search** combines a result table with renderer, metadata,
   artifact, citation, and provenance actions.
6. **Notebook workspace** combines cells, rich outputs, variables, files, LSP
   diagnostics, and terminal tabs without nesting cards.
7. **Ecosystem settings** manages providers, skills, MCP servers, and plugins
   with real health, discovery, validation, and lifecycle actions.
8. **Share replay** is read-only and presents the run timeline, artifacts,
   research graph, audit trail, revocation state, and export controls.

## Interaction Rules

- First launch opens onboarding. The launcher remains unavailable until one
  provider/model pair is verified; settings and diagnostics remain reachable.
- Agent execution streams typed run events into the timeline. Permission and
  question events suspend the run and resume from a persisted pending action.
- Errors remain attached to the originating provider, connector, tool, cell, or
  LSP session and include retry or configuration actions where applicable.
- Search hits can be sent to a session, saved as artifacts, cited, or published.
  Every transition writes a provenance edge.
- The right inspector is context-sensitive and remembers width per workspace.
- English is the default locale. Every visible string is sourced from locale
  resources and can switch to `zh-CN` without reloading the application.

## Accessibility And Density

- Interactive controls have keyboard focus, accessible names, and a minimum
  30px hit target in dense desktop mode.
- Text, status, and icons remain legible at 100% and 125% system scaling.
- Status is never communicated by color alone.
- No toolbar, tab strip, permission drawer, or composer may overlap at
  1280x720, 1440x900, or 1728x1117 viewports.
