package analyze

import "testing"

func TestBuildLightweightGraphIndexes(t *testing.T) {
	summary := Summary{
		RuntimeCalls: []RuntimeCallStats{
			{
				Screen:  "Feed",
				Flow:    "feed.open",
				Step:    "load",
				Caller:  "FeedOwner",
				Callee:  "NetworkOwner",
				Count:   3,
				TotalMS: 120,
			},
		},
		ProblemWindows: []ProblemWindowStats{
			{
				Screen: "Feed",
				Flow:   "feed.open",
				Step:   "load",
				Owner:  "FeedOwner",
				Kind:   "main_thread_stall",
				Count:  2,
				MaxMS:  700,
			},
		},
		LogSpam: []LogSpamStats{
			{
				Screen: "Feed",
				Flow:   "feed.open",
				Step:   "load",
				Owner:  "FeedOwner",
				Source: "android.util.Log.d",
				Count:  9,
			},
		},
	}
	diagnostics := &InstrumentationDiagnostics{
		Classes: []InstrumentationClassDiagnostic{
			{
				ClassName: "com.app.FeedRepository",
				Hooks: []InstrumentationHookSummary{
					{
						Intent:    "okhttp.install_event_listener_factory",
						Signature: "okhttp3.builder.build.v3",
						Bridge:    "okhttp3.bridge.v3",
						Method:    "client()V",
						Count:     1,
					},
				},
				Annotations: []InstrumentationAnnotationSummary{
					{Owner: "FeedOwner", Screen: "Feed", Flow: "feed.open", Trace: "load", Count: 1},
				},
			},
		},
	}

	indexes := BuildLightweightGraphIndexes(summary, diagnostics)

	if calls := indexes.OwnerCalls["FeedOwner"]; len(calls) != 1 || calls[0].Callee != "NetworkOwner" {
		t.Fatalf("owner calls were not indexed: %+v", indexes.OwnerCalls)
	}
	hooks := indexes.MethodHooks["com.app.FeedRepository#client()V"]
	if len(hooks) != 1 || hooks[0].Bridge != "okhttp3.bridge.v3" {
		t.Fatalf("method hooks were not indexed: %+v", indexes.MethodHooks)
	}
	annotations := indexes.ClassAnnotations["com.app.FeedRepository"]
	if len(annotations) != 1 || annotations[0].Trace != "load" {
		t.Fatalf("class annotations were not indexed: %+v", indexes.ClassAnnotations)
	}
	problems := indexes.ScreenProblems["Feed"]
	if len(problems) != 1 || problems[0].Kind != "main_thread_stall" {
		t.Fatalf("screen problems were not indexed: %+v", indexes.ScreenProblems)
	}
	events := indexes.TraceEvents["Feed / feed.open / load"]
	if len(events) != 3 {
		t.Fatalf("trace events len = %d, want 3: %+v", len(events), events)
	}
	if events[0].Kind != "log_spam" || events[0].Count != 9 {
		t.Fatalf("trace events were not sorted by count: %+v", events)
	}
}

func TestBuildLightweightGraphIndexesFallsBackToTopDiagnosticClasses(t *testing.T) {
	diagnostics := &InstrumentationDiagnostics{
		TopClasses: []InstrumentationClassDiagnostic{
			{
				ClassName: "com.app.Fallback",
				Hooks: []InstrumentationHookSummary{
					{Intent: "logspam.android.util.Log.d", Method: "debug()V", Count: 2},
				},
			},
		},
	}

	indexes := BuildLightweightGraphIndexes(Summary{}, diagnostics)
	if hooks := indexes.MethodHooks["com.app.Fallback#debug()V"]; len(hooks) != 1 {
		t.Fatalf("top class fallback did not build method hook index: %+v", indexes.MethodHooks)
	}
}
