# AI transparency notice

Foreman is built with substantial help from generative AI tools, mainly
Claude Code agents working under the maintainer's direction. This notice
says where AI is used and where it is not, in the spirit of the
transparency principles in Article 50 of the EU Artificial Intelligence
Act.

## Where AI helps make Foreman

- **Code, tests and documentation.** Source code, unit and browser tests,
  the specification, the operator guide and the interface text are
  drafted, reviewed or revised with AI help.
- **Design.** Screen layouts and visual design are explored and refined
  with AI help.
- **Translations.** The seven languages other than English are translated
  by AI and checked by a second AI pass, not by native speakers. They can
  be wrong; corrections are welcome as GitHub issues (see
  [docs/I18N.md](docs/I18N.md)).

The maintainer sets the direction, chooses what is accepted, and stays
responsible for what is published. Every change lands through a pull
request that the project's automated checks must pass, and merging to
`main` is the maintainer's decision. AI output is not taken as correct
without review and tests.

## How the work is done

Using AI does not mean working without discipline. The maintainer's note
[AI-assisted software engineering](https://github.com/toddawhittaker/ipeds-oracle/blob/main/docs/AI_ASSISTED_ENGINEERING.md),
written for another of his projects, explains the approach. In short:
each new layer of programming tools, from compilers onwards, was called
"not real engineering", and each time the engineer's judgment moved up a
level instead of going away. With AI writing much of the routine code,
the work that matters is the architecture, the interfaces and data,
the test strategy, the security, and deciding what is actually correct.
Code is judged by its tests, its structure and its security, not by the
tool that typed it, and every change passes the same gates whoever wrote
it.

Foreman follows the same approach:

- **Spec first.** The vision, the numbered requirements in
  [docs/SPEC.md](docs/SPEC.md) and the technology choices in
  [docs/STACK.md](docs/STACK.md) were each approved line by line before
  any code. Work then proceeds one epic at a time against those sections.
- **Tests are part of done.** Each change ships with unit tests, and any
  change a user can see ships with browser tests too. A bug fix starts
  with a failing test. A coverage floor is enforced, and anything that
  touches the server, worlds, snapshots, mods or game versions is also
  tested against the real Factorio server image.
- **One merge gate.** `make verify` and the CI checks must pass, and
  `main` changes only by pull request.
- **Separate reviews.** Dedicated review agents check correctness,
  security and accessibility, the same specialties a mature team
  staffs. Their definitions are checked in under
  [.claude/agents/](.claude/agents/), with the project rules in
  [CLAUDE.md](CLAUDE.md).
- **Docs kept honest.** Each epic records what it delivered and the gaps
  it left in [docs/STATUS.md](docs/STATUS.md), and the guides change in
  the same pull request as the code.
- **Improved continually.** Batches of fixes follow hands-on testing,
  and whole-codebase quality audits and graded design reviews feed the
  next round of work.

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

Map previews are drawn by the real Factorio headless server, not
generated. The screenshots in this repository are captures of Foreman
running real worlds and real mods, not mock-ups.

## References

This notice is for transparency and does not describe Foreman as an AI
system. See [Article 50 of Regulation (EU) 2024/1689](https://eur-lex.europa.eu/eli/reg/2024/1689/oj#art_50)
and the [German Federal Network Agency's overview of AI transparency
obligations](https://www.bundesnetzagentur.de/DE/Fachthemen/Digitales/KI/4_Transparenzpflichten/start.html).
