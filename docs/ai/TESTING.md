### Test Writing Instructions

* ALWAYS use existing test files as a reference when generating new tests. Prioritize files in the same package if they exist.
* Prioritize testing observable behavior via exported methods (`Execute`, `Events`, `Result`) over internal state.
* Use `google/go-cmp/cmp` for comparing structs, slices, or complex types. Do NOT use `reflect.DeepEqual` or manual field checks.
* Use existing mocking utilities from @./internal/testutil or create a new utility in the package if needed.
* Use `t.Parallel()` at the start of test cases.