### General

* Use @./docs/ai/OVERVIEW.md to understand the high-level project organization.
* Use @./docs/ai/CONCURRENCY_MODEL.md if understanding concurrency model of request and execution handling is important for the task.
* Ask clarifying questions from the user if details important for the task are missing.
* When working on a bug fix, follow the RED-GREEN-BLUE TDD approach.
* After building a mental model of a complicated flow, update or create a document in @./docs/ai/ to capture that knowledge so future sessions don't have to re-derive it.
* After correcting a mistake based on user feedback, if it could have been avoided with better instructions, update the "AI learnings" section of @AGENTS.md with a rule that prevents the same class of mistake and notify the user about the update.

### Code Style

* When creating core types from a2a package use constructor functions from @./a2a/core.go (eg. `a2a.NewMessage(...)`, `a2a.NewStatusUpdateEvent(...)`).
* Do not leave comments in the code unless they explain a non-trivial implementation detail or highlight a suboptimally handled edge-case.
* Prefer early `return`-s and `continue` over deeply nested blocks.

### Test Writing

* ALWAYS use existing test files as a reference when generating new tests. Prioritize files in the same package if they exist.
* Prioritize testing observable behavior of exported methods, not the internal state.
* Use `google/go-cmp/cmp` and `cmp.Diff(want, got)` for comparing structs, slices, maps, or complex types. Do NOT use `reflect.DeepEqual` or manual field checks.
* Use existing mocking utilities from @./internal/testutil or create a new utility in the package if needed.
* Use `t.Parallel()` at the start of test cases.
* Use `receiver.Operation() error = %v, want %v` as a template for printing test error check failures.
* Use `receiver.Operation() = %v, want %v` as a template for printing test error check failures.
* Use `receiver.Operation() wrong result (+got,-want) diff = %s` as a template for printing test errors received when using `cmp.Diff`.
* Prefer using `t.Fatalf` over `t.Errorf` unless printing all the failed checks is justified or the method is called not from the main goroutine. 
* Always check return type event if discarding it `_ = smth.Close()`.

### AI learnings

* ...