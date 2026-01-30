### Instructions

* Use @./docs/ai/OVERVIEW.md to understand the high-level project organization.
* Before writing tests ALWAYS look into @./docs/ai/TESTING.md for guidance.
* When creating core types from a2a package use constructor functions from @./a2a/core.go (eg. a2a.NewMessage(...), a2a.NewStatusUpdateEvent(...)).
* Do not leave comments in the code unless they explain a non-trivial implementation detail or highlight a suboptimally handled edge-case.
* Prefer early `return`-s and `continue` over deeply nested blocks.