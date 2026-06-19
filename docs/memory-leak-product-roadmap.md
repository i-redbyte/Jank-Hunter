# Memory Leak Detection Product Roadmap

Jank Hunter should beat LeakCanary by combining leak detection, heap evidence, junior-friendly explanations, version comparison, and CI automation. LeakCanary is excellent at local heap leak discovery; Jank Hunter's product edge is that leak evidence is tied to performance context, owner attribution, reports, and regression analysis.

## 1. Heap Analyzer Accuracy

- Parse HPROF safely with bounded object, edge, target, and retained-size traversal limits.
- Classify GC roots into actionable categories: class/static, thread, JNI, monitor, VM/internal, reference processing, and unknown.
- Prefer shortest actionable path from GC root to retained object.
- Ignore weak/soft/phantom `java.lang.ref.Reference.referent` edges when building leak paths; heap-confirmed paths must be strong-reference paths.
- Keep alternative paths when the same retained object is reachable through more than one owner chain.
- Surface retained size, retained object count, dominator sample, and confidence.
- Surface leak pattern and reference matcher hints, for example static/singleton, thread/queue, listener/callback, context reference, and known Android framework risk areas.
- Filter reporting toward app-owned holders while preserving the raw root/path evidence.
- Rank primary heap paths by actionability when retained sizes are close: app holder, holder field, GC root category, leak pattern, matcher hints, and short strong-reference path should beat noisy system-only paths.

Acceptance:

- Heap report shows root kind/category, holder field, retained size/count, primary strong-reference path, alternative paths, pattern, and matcher hints.
- Large HPROF files degrade into bounded warnings instead of hanging.
- Compare mode can fingerprint leaks by normalized chain, not just class name.

## 2. Junior-Friendly Explanation Layer

- Every leak row explains what is retained, who likely holds it, why it matters, where to start, and how to verify the fix.
- Common patterns get specific guidance: Activity/Fragment, View/binding, listener/callback, coroutine/thread/Handler, cache/singleton, resource.
- Light mode clearly separates runtime suspicion from heap-confirmed evidence.

Acceptance:

- A junior developer can open `report-leaks.html` and know the first three checks without reading raw HPROF internals.

## 3. Visual Leak Explorer

- Selecting a leak shows graph, impact, recommendation, quick checks, and verification steps.
- Nodes distinguish GC root, app holder, system/library object, target, and retained dominated classes.
- Graph nodes expose tooltips/details for class, field, root category, and role.
- Alternative paths are visible when available.

Acceptance:

- Reports are standalone, responsive, and do not require CDN/runtime services.

## 4. Runtime Attribution

- Runtime retained events should preserve screen, flow, step, ownerHint, and lifecycle source.
- Activity lifecycle destroy watch should remain automatic and dependency-free.
- ASM lifecycle leak hooks should auto-watch `onDestroy`, `onDestroyView`, `onCleared`, `onDetachedFromWindow`, Dialog/Service stop-destroy boundaries, and RecyclerView adapter/view-holder boundaries without adding AndroidX to runtime core.
- Fragment `onDestroyView` should watch the current view and likely binding/view fields before cleanup, so fixed code naturally disappears after retained-delay.
- Lifecycle classification should produce specific object kinds for Activity, Fragment, ViewModel, View, Service, Dialog, RecyclerView ViewHolder/Adapter, View/binding, resources, and system objects.
- Runtime watcher should deduplicate repeated watch calls for the same live object.
- Developers should be guided to use `withOwner`, `withFlow`, `markFlowStep`, and `watchObject(..., ownerHint)`.

Acceptance:

- Light report is useful without heap dump and becomes more precise when owner/flow data is present.

## 5. Compare And Regression Analysis

- Compare report classifies leaks as new, worse, same, better, or resolved.
- Matching should prefer normalized chain fingerprint and fall back to class/holder/context.
- CI gate should support leak-specific limits such as max new leaks, max worse leaks, and max high severity leaks.

Acceptance:

- CI can fail only on meaningful leak regressions while still producing HTML artifacts.

## 6. Sample/Demo Flow

- Sample app must demonstrate retained Activity, View/binding, listener, cache, and clean object scenarios.
- Sample UI should point users to light report, heap report, and compare report workflows.

Acceptance:

- A developer can create a controlled leak, flush logs, run CLI inspect/compare, and see a clear report.

## 7. Developer Experience Automation

- Provide a host-side script that pulls `.jhlog` and optional `.hprof` artifacts from a device and runs CLI report generation.
- Document Gradle/plugin heap dump knobs and CLI commands together.

Acceptance:

- Debug/QA workflow is one command after a sample scenario has run.

## 8. Real App Validation

- Validate against at least one large Android app and compare output with LeakCanary on the same scenarios.
- Track false positives, false negatives, report readability, and CI usefulness.
- Use `docs/memory-leak-validation-scorecard.json` for scenario-level precision/recall, heap actionability, lifecycle coverage, compare stability, and junior readability scoring.

Acceptance:

- Validation notes are documented separately from synthetic tests.
- A 9/10 claim requires a completed scorecard with controlled positive/negative scenarios and matching Jank Hunter/LeakCanary artifacts.
