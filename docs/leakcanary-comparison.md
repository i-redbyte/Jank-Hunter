# LeakCanary Comparison

Sample app includes a debug-only LeakCanary bridge so the same retained objects can be watched by both tools:

- Jank Hunter records `.jhlog` events with screen, flow, step, owner, counters and optional heap evidence.
- LeakCanary watches objects through `AppWatcher.objectWatcher.expectWeaklyReachable(...)`, dumps/analyzes the heap when its retained-object threshold is reached, and shows the result through its notification or launcher entry.
- Release builds keep a no-op bridge, so LeakCanary classes are not bundled outside debug.

The integration follows the official LeakCanary 2.14 setup: `debugImplementation("com.squareup.leakcanary:leakcanary-android:2.14")`. LeakCanary auto-installs in debug builds, and custom objects are sent to the modern reachability API, `AppWatcher.objectWatcher.expectWeaklyReachable(...)`.

Sources:

- [LeakCanary Getting Started](https://square.github.io/leakcanary/getting_started/)
- [LeakCanary Code Recipes](https://square.github.io/leakcanary/recipes/)
- [How LeakCanary works](https://square.github.io/leakcanary/fundamentals-how-leakcanary-works/)
- [LeakCanary Change Log](https://square.github.io/leakcanary/changelog/)

## How To Run

1. Start the sample app with `./run-sample-app.sh`.
2. In the app, open `LeakCanary benchmark`.
3. Tap `Both: clean object` for a negative control.
4. Tap `Both: retained object` or `Both: cache burst` for positive leak cases.
5. Wait a few seconds. If LeakCanary has not dumped yet, background the app or open its notification/launcher entry.
6. In the script shell, run `log` or `report` to pull Jank Hunter logs and generate HTML.

## What To Compare

| Question | LeakCanary report | Jank Hunter report |
| --- | --- | --- |
| Did the object leak? | Retained object count, heap analysis result and leak signature. | `report-leaks.html` retained count, class, owner, flow and evidence freshness. |
| Why is it retained? | Strong reference path from GC root to leaking object. | Light mode: holder/scope/context. Heap mode: GC root chain, holder field, retained size and alternative paths. |
| Is this a regression? | Manual comparison between separate LeakCanary reports. | `compare-leaks.html` groups new, worse, same, better and resolved leak signatures. |
| Is the report readable for juniors? | Precise but dense heap trace. | Product-style summary, severity, checklist, suspect owner and step-by-step investigation hints. |
| Can it join performance analysis? | Leak-focused only. | Correlates leaks with UI stalls, memory pressure, network, log spam, owners and flows. |

## Expected Demo Behavior

`Both: clean object` should disappear after GC and should not become a persistent leak.

`Both: retained object` keeps a `LeakedActivityScreen` instance in the sample retained list. LeakCanary should eventually show a retained object / leak trace. Jank Hunter should show the same class with `sample.memory_leak.leakcanary_benchmark` owner and `sample.memory_leak.demo` flow.

`Both: cache burst` keeps three cache entries. LeakCanary should group or list retained objects after analysis. Jank Hunter should make the burst obvious in both single-run leak report and baseline/candidate comparison.

## Product Positioning

LeakCanary remains the reference for local heap leak detection and precise GC-root traces. Jank Hunter should be stronger when a team needs readable reporting, regression comparison, ownership attribution, flow context, CI artifacts and correlation with broader performance signals.
