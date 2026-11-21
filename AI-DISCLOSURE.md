# AI transparency notice

I build Foreman with substantial help from generative AI tools, mainly
Claude Code agents working under my direction. This notice says where I
use AI and where Foreman does not, in the spirit of the transparency
principles in Article 50 of the EU Artificial Intelligence Act.

## Where AI helps me make Foreman

- **Code, tests and documentation.** I draft, review and revise the source
  code, the unit and browser tests, the specification, the operator guide
  and the interface text with AI help.
- **Design.** I explore and refine screen layouts and visual design with
  AI help.
- **Translations.** The seven languages other than English are translated
  by AI and checked by a second AI pass, not by native speakers. They can
  be wrong, and I welcome corrections as GitHub issues (see
  [docs/I18N.md](docs/I18N.md)).

I set the direction, I decide what is accepted, and I am responsible for
what I publish. Every change lands through a pull request that must pass
the project's automated checks, and I make every merge to `main`. I don't
take AI output as correct without review and tests.

## How I work

Using AI doesn't mean working without discipline. I explain my approach
in [AI-assisted software engineering](https://github.com/toddawhittaker/ipeds-oracle/blob/main/docs/AI_ASSISTED_ENGINEERING.md),
a note I wrote for another of my projects. In short: each new layer of
programming tools, from compilers onwards, was called "not real
engineering", and each time the engineer's judgment moved up a level
instead of going away. With AI writing much of the routine code, the work
that matters is the architecture, the interfaces and data, the test
strategy, the security, and deciding what is actually correct. Judge code
by its tests, its structure and its security, not by the tool that typed
it. Every change passes the same gates, whoever wrote it.

Here is how that works on Foreman:

- **Spec first.** I approved the vision, the numbered requirements in
  [docs/SPEC.md](docs/SPEC.md) and the technology choices in
  [docs/STACK.md](docs/STACK.md) line by line before any code was
  written. The work then proceeds one epic at a time against those
  sections.
- **Tests are part of done.** Each change ships with unit tests, and any
  change a user can see ships with browser tests too. A bug fix starts
  with a failing test. A coverage floor is enforced, and anything that
  touches the server, worlds, snapshots, mods or game versions is also
  tested against the real Factorio server image.
- **One merge gate.** `make verify` and the CI checks must pass, and
  `main` changes only by pull request.
- **Separate reviews.** Dedicated review agents check correctness,
  security and accessibility, the same specialties a mature team staffs.
  Their definitions are checked in under [.claude/agents/](.claude/agents/),
  with the project rules in [CLAUDE.md](CLAUDE.md).
- **Honest docs.** Each epic records what it delivered and the gaps it
  left in [docs/STATUS.md](docs/STATUS.md), and the guides change in the
  same pull request as the code.
- **Continual improvement.** I test Foreman by hand on my own servers,
  and each round of fixes, whole-codebase quality audit and graded design
  review feeds the next round of work.

## What Foreman itself does not do

Foreman is not an AI system. It contains no language model, image
generator or AI chat, and it sends no saves, maps, settings, secrets,
logs or usage data to any AI provider.

Foreman does offer an MCP (Model Context Protocol) endpoint, so that an
AI assistant you choose can read and run your server. It is off by
default. It works only after an admin turns it on and gives the
assistant a token with a limited scope, and everything a token does is
recorded in the audit log. What that assistant does with the answers it
gets is between you and its provider.

The real Factorio headless server draws the map previews; nothing is
generated. The screenshots in this repository are captures of Foreman
running real worlds and real mods, not mock-ups.

## References

This notice is for transparency and does not describe Foreman as an AI
system. See [Article 50 of Regulation (EU) 2024/1689](https://eur-lex.europa.eu/eli/reg/2024/1689/oj#art_50)
and the [German Federal Network Agency's overview of AI transparency
obligations](https://www.bundesnetzagentur.de/DE/Fachthemen/Digitales/KI/4_Transparenzpflichten/start.html).
