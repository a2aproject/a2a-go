### Test Writing Instructions

* ALWAYS use existing test files as a reference when generating new tests. Prioritize files in the same package if they exist.
* Prioritize testing observable behavior via exported methods (`Execute`, `Events`, `Result`) over internal state.
* Use `google/go-cmp/cmp` and `cmp.Diff(want, got)` for comparing structs, slices, maps, or complex types. Do NOT use `reflect.DeepEqual` or manual field checks.
* Use existing mocking utilities from @./internal/testutil or create a new utility in the package if needed.
* Use `t.Parallel()` at the start of test cases.
* Use "receiver.Operation() error = %v, want nil" or "receiver.Operation() error = %v, want %v" as a template for printing test error check failures.
* Use "receiver.Operation() = %v, want %v" as a template for printing test error check failures.
* Use "receiver.Operation() wrong result (+got,-want) diff = %s" as a template for printing test errors received when using `cmp.Diff`.
* Prefer using `t.Fatalf` over `t.Errorf` unless printing all the failed checks is justified. 