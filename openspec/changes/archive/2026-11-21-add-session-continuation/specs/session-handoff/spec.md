# Session handoff spec delta

## MODIFIED Requirements

### Requirement: Authored brief content

`spool handoff create` SHALL accept a version-1 JSON brief from an explicitly named input file or, when explicitly requested, stdin. The brief SHALL require a nonempty goal and MAY contain a title and lists of constraints, decisions, progress, outstanding work, and notes. `spool handoff create` SHALL store the supplied text literally and SHALL NOT infer, summarize, or generate content. A separate explicitly authorized continuation workflow can draft content for review; only the reviewed, approved brief is supplied to creation.

#### Scenario: Minimal brief
- **WHEN** a brief contains only a version and a goal
- **THEN** Spool creates a handoff whose other content lists are empty and whose title is null

#### Scenario: Full brief preserves authored order
- **WHEN** a brief contains constraints, decisions, progress, outstanding work, and notes
- **THEN** the stored handoff contains each decoded item exactly and in the supplied order, and the Markdown rendering presents each item literally in the same order

#### Scenario: No content is invented
- **WHEN** a brief references sessions but supplies no decisions
- **THEN** the handoff has an empty decision list rather than decisions derived from the referenced history

#### Scenario: Reviewed model draft is supplied explicitly
- **WHEN** a separately authorized drafting workflow supplies a user-reviewed and approved version-1 brief
- **THEN** creation validates and stores those supplied bytes without invoking a model or accepting unreviewed generated content
