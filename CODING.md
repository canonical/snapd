# Coding/Review checklist

## Why reviews?

* Reviews can give input on whether the proposed code is seemingly correct and reasonable in the context of project practices, and whether it seems sufficiently tested.

* Code can have a long lifetime; the effort to maintain and adapt it in the future can be much larger than the original effort to produce the first version of it. Reviews from other team members should therefore focus on:
  * Is the new code readable and understandable, alongside other attributes that can help future maintainability?
  * Could the code be simplified?

## Naming conventions

To a large extent we follow [golang naming conventions](https://go.dev/doc/effective_go#names):

* Names should strike a balance between concision and clarity, where for a local variable more weight might be put on concision while for an exported name clarity might have a larger weight.

* Consistency is important in a somewhat large and long lived project, it is always a good idea to check whether there are similar entities or concepts in the code from which to borrow terminology or naming patterns, especially in the neighbourhood of the new code. For example when using a verb in a method name, it is good to check whether the verb is used for similar behaviour in other names or some other verb is more common for the usage.

* Regarding concision, golang is a typed language so a slightly more concise name might still work because purpose is clarified by the parameter types of the to-be-named function or the type of the to-be-named field.

* One should remember that unexported symbols are scoped to a whole package, not their code file, so they should be named accordingly. Even for unexported helpers and symbols clarity is important - prefer something specific in their name over very generic names: `func findRev(needle snap.Revision, haystack []snap.Revision) bool` vs `find` or `findStuff`.

## Comments

Ideally all exported names should have doc comments for them following [golang conventions](https://go.dev/doc/comment).

We sometimes also use long code comments or separate markdown README files for higher-level descriptions of mechanisms or concepts.

Inline code comments should usually address non-obvious or unexpected parts of the code. Repeating what the code does is not usually very informative:

* Code comments should either address the why something is done
* Or clarify the more abstract impact of the low-level manipulation in the code

It might be appropriate and useful also to give proper doc comments even to complex unexported helpers.

## Function signatures

Example: in `overlord/snapstate`

```
Install(ctx context.Context, st *state.State, name string, opts *RevisionOptions, userID int, flags Flags) (*state.TaskSet, error)
```

* We try to follow this kind of ordering for parameters of functions and methods:
  * `context.Context` if provided
  * Long lived/ambient objects like `state.State`
  * The main entities the function or method operates on
  * Any optional and ancillary parameters in some order of relevance
* For return parameters, they should be in some order of importance with error last as per golang conventions.
* Consistency is important, so parallel/similar functions/methods should try to have the same/any shared parameters in the same order.
* For exported functions, generally try to avoid asking callers to pass values that can be computed by the called methods/functions anyway. Sometimes some optimisation pattern might make this worthwhile but consider if that is really the case. Even then things should always be organised in ways that avoid breaking/confusing responsibility boundaries.

## Error and error messages

* We tend not to introduce **Error* structs until we know of caller code that will need to inspect them.

* We use `fmt.Errorf` and `errors.New` as much as possible.

* Error messages start with lowercase and not end with a period, as they often end up embedded in one another

* Error messages should be formulated as *"cannot …"* whenever possible, so avoid *"failed to …"* for example.

* OTOH as error messages often end up being embedded in one another/chained. It is also important to pay attention so that the final messages do not have too much repetition, when possible, to avoid things like *"cannot …: cannot …: cannot …"* for example. Tests for the error paths can help find those repetitions.

* Error messages should be clear, and when possible, actionable. They should also use concepts and terminology familiar to the user instead of internal-only concepts unless they are really unexpected internal errors.

* Prefixing errors with *"internal error: …"* should be used for programming errors or other unexpected internal inconsistencies but preferably not for situations where external state that is not completely under snapd control is involved.

## Other style points

* We rely on and apply `go fmt` consistently.
* Our PR CI static checks run [`golangci-lint`](https://github.com/golangci/golangci-lint), for details see our `.golangci-lint.yml` config.
* We tend to avoid naked returns, we run the `nakedret` test with an accepted function length of at most 5.
* `run-checks –static` runs also various linting plus some project specific checks:
  * For example, in the face of mixed usage in the end we agreed to use numbers directly instead and avoid `http.Status*` constants. This is checked by `run-checks -–static`.

## Code structuring

* Packages should have a clear responsibility, and present an exported interface with a relatively consistent level of abstraction. As an exception, there might be a few higher-level convenience functions or lower level ones. Generally if a responsibility is split across multiple packages, with the aim to produce focused and readable code, it should be split by having packages with growing application-specific levels of abstraction, instead of splitting the same level of abstraction across multiple packages. As current examples of this in the code base, consider (in higher-level to lower-level abstraction level):
  * overlord/snapstate -> overlord/snapstate/backend -> boot -> bootloader
  * overlord/snapstate -> overlord/snapstate/backend -> wrappers -> systemd
  * overlord/servicestate -> wrappers -> systemd

* Symmetry should always be applied to structure code when it is useful. Code is easier to reason about and to review if do and undo code paths, save and restore paths, etc. are written in obvious structurally symmetric ways when possible. Managers' tasks being defined with do and undo handlers tries to guide and facilitate this for example.

* When trying to keep functions and methods readable by introducing helpers, trying to aim for a mostly consistent level of abstraction inside each function could be useful.

* *Do not repeat yourself* is a balancing act. Complex behaviour should ideally be encoded only once in the code base when possible. When extracting and deciding how to extract some behaviour it is important to consider the readability of both the now encapsulated code and its consumers. For example, if it's hard to give the extracted code a good name and signature it might show that a different approach should be looked at. For simpler helpers, it might be worth seeing a couple of usages before creating them as local helpers, and a bit more before creating an exported helper that can be imported from all the used places. When creating helpers and avoiding repetition the aim should also be first to improve maintainability and readability. If the consumer code is less readable then maybe the extraction in this case might not be a good idea in the end.

* See [Tests](#tests) for consideration about repetition and reuse specifically in test code.

* Given that golang does not support mutual/circular imports we have a few patterns and rules:
  * Across overlord state managers' packages:
    * `snapstate` can and is imported by other managers but cannot import them directly
    * `assertstate` and `hookstate` should also be mostly consumed and not consume other managers
  * When unavoidable, we break circular import issues by using exported function hook variables from the normally imported package. These are assigned to in the package that normally imports and uses the first one, either directly or indirectly.
  * These variables need to be assigned in `init` code or `Manager` constructor functions.
    * Examples are:
      * `snapstate.ValidateRefreshes` assigned from a `assertstate.delayedCrossMgrInit` called by `assertsstate.Manager`
      * The hooks in `snapstate` assigned from a `hookstate init`
      * `boot.HasFDESetupHook` assigned from `devicestate.Manager`
  * When applicable, we might also use hook registration mechanisms. Examples:
    * `snapstate.AddCheckSnapCallback`
    * `snapstate.RegisterAffectedSnapsByAttr`
  * `*util` packages should not import not `*util` packages, and whenever possible just use standard library packages and as few as strictly necessary other `*util` packages

* Most packages can be imported as needed by any snapd tool and service with the exception of most code under overlord and the state managers' packages. This code is meant to implement only the snapd daemon itself and not be imported by any other tool, as it will also grow the size of the latter significantly. (As an exception, a subset of `overlord/configstate/configcore` is consumed and built into tools outside of the snapd daemon. This subset is kept under control via the `nomanagers` build tag).

## System properties

* snapd should complete initiated operations even in the case of snapd restarts or system reboots. In the case of failure, it should try to bring back the system to a known good state.
* snapd should avoid or minimise cases and time windows where the external state of the system can cause unexpected errors for users and the rest of the system.

## Tests

Tests should help with these aspects:

* Illustrate (and help clarify while writing the code) the intent and behaviour of code.
* Anchor the correctness of the code, to the extent possible, from this POV while we try to keep a high coverage without overdoing. One should keep in mind that line coverage might itself not be enough. Lines of code can be covered without being tested as only part of a predicate might be triggered, or some code could be removed and no test breaks unless tests were targeting it. So when writing tests it is always important to take on an expectations and behaviours mindset.
* Help later to have some confidence when refactoring.

We do not mandate TDD, but it's always a good idea when possible to start at least from happy path tests for new code/behaviour.

Bug fixes should be covered by new tests, for which is important to verify that they do not pass and fail as expected prior to the fix.

Coverage of error handling is important as well:

* Complex error handling should be tested
* Undo/restore behaviour on error, if any, should be tested
* Error generation that produces errors that are expected/inspected by callers should be tested
* Simple unexpected/give up error paths may not necessarily need to be tested
* The return paths in code like:

  ```
      if […;] err != nil {
         return err
      }
  ```

  might not need to be tested if it's too cumbersome to trigger them, but the other considerations need to be taken into account. Reviewers should keep under consideration that golang error handling conventions are followed.

We use [gocheck](https://labix.org/gocheck) and our own testutil package for snapd tests, to complement what is provided by go and golang standard library.

We definitely prefer to write tests in dedicated `<package-under-test>_test` packages, this means that tests should mainly explore the exported interface of the tested packages. There might be helpers and unexported details that sometimes warrant testing, in which case we use re-assignment or type aliasing in conventional `export_test.go` or `export_foo_test.go` files in the package under test, to get access to what we need to test. This is usually needed if there is algorithmic complexity or error handling behaviour that is hard to explore through the exported API, or is important to illustrate the chosen behaviour of the helper in itself.

There are varying opinions on this, but mocking is definitely a double-edged sword. Our pattern for mocking is `Mock*` functions defined in `export_test.go` returning a parameter-less restore function. These usually change some package global variable through which original values or functions are indirectly accessed and therefore can be replaced.

```
var timeNow = time.Now // close to usages in package code

func MockTimeNow(f func() time.Time) (restore func()) { // in export_test.go
     restore = testutil.Backup(&timeNow)
     timeNow = f
     return restore
}
```

If something cannot avoid being mocked across package boundaries, we sometimes have `Mock*` functions or constructors exported in the API of packages.

Because of this complexity with mocking, and because mock-heavy tests might risk needing large rewrites when refactoring (which goes against their confidence enhancement use), we are not very strict about unit tests testing exactly single functions and structs. We should do that whenever possible without mocking, but otherwise it is not atypical for snapd tests to concentrate on mocking points of interaction with the actual external system and state, as it might require less overall mocking support and it might be easier to reason on expectations for effects. For example, we have support to mock our systemd interactions and observe the involved `systemctl` invocations (`systemd.MockSystemctl`).

So, many of our unit tests might end up testing more than one package, and test instead across two levels (rarely more) of packages in our architecture; a package of lower-level primitives, for example, and a more high-level behaviour one using the former.

Full direct mocking might still make perfect sense when the API of the consumed packages is very complex but its details should be fully or largely transparent to the consumer. This is mostly the case, for example, when testing API functionality in the `daemon` package vs the API offered by the [overlord state managers](https://github.com/snapcore/snapd/blob/master/overlord/README.md).

The cost of our approach is sometimes a complex fixture setup. To help mitigate this, and in other cases when it makes sense for a package to offer test-dedicated helpers related to it, we can introduce matching `<main-package>test` packages one level deeper than `<main-package>` (e.g. `asserts/assertstest` or `overlord/devicestate/devicestatetest`).

Related to tests in and for overlord state manager packages `overlord/<concern>state`, we have a few rules:

* Ideally they should limit themselves to test the manager defined by the package
* If that's not possible they should limit themselves to as few managers as possible
* If what needs to be tested is the full interaction across many or all managers then we have or can write tests for this in `overlord/managers_test.go`. Fixture setup in these cases is very costly but they are still easier to iterate on and can be useful to probe behaviour in more internal details than functional/integration tests.

We do not have strong policies against repetition in test code, as usual the important consideration is readability. This area is mostly left to the personal judgement of developers. If any general advice can be given is that:

* Investing in clear helpers to setup complex fixtures is often valuable, while compressing actual ad hoc testing and checking code less so, as it might result in if-trees that might be hard to follow.
* Wherever applicable, tabular tests (where cases are expressed as a slice of anonymous structs) should be used. For example, they are often appropriate when testing error cases of functions.

## Functional/integration tests

We write them using [spread](https://github.com/snapcore/spread). Generally all externally visible features and behaviour should have spread tests. It's also important to have them for robustness and fallback behaviour as related to system properties.

In order to keep the integration testing harness easy to read and consistent, there are some rules about the order and the existence of the different spread tests sections.

This is the ordered sections list to follow:
 1. summary (required)
 1. details (required)
 1. backends
 1. systems
 1. manual
 1. priority
 1. warn-timeout
 1. kill-timeout
 1. environment
 1. prepare
 1. restore
 1. debug
 1. execute (required)

The CI tooling will check and enforce the order and required sections when a spread test is created or updated.

## PRs and refactorings

* PR should ideally have diffs of around 500 lines or less. There might be exceptions when size is due to large repetitive tests, but not for the production code. Experience indicates that smaller PRs are easier to review, while it is hard to do careful and punctual reviews for very large diffs.

* It is fair for reviewers to ask for large PRs to be split. It is also fair to ask for discussion on best strategies to do this with colleagues and architects.

* Whenever reasonable, avoid spurious differences between the code in master and the new code.

* Large mechanical refactoring and changes should be done as separate PRs. Try to separate behaviour changes and refactoring into different PRs and not mix the two.

* Large moving of code around and changes to code placement might also be better done separately.

* PR summaries and the first line of commit messages are expected to be of this form:
  * *`affected full packages:  short summary in lowercase`*
    * When too many packages are involved, many can be used instead, or sometimes package names can be abbreviated by using single letters for the top-level package, when non ambiguous combined with the subpackage.
    * Examples:
      * `overlord/devicestate: add test to check connect hooks don't break anything`
      * `gadget,image: remove LayoutConstraints struct`
      * `o/snapstate: add helpers to get user and gating holds`
      * `many: correct struct fields and output key`
  * When no golang code is involved, the context prefix before the colon can refer to directories or top-level files instead.
    * `build-aux,.github/workflows: limit make processes with nproc`

## Further readings

* [Notes on state and changes](https://github.com/snapcore/snapd/blob/master/overlord/README.md)
