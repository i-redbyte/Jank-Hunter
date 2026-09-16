package analyze

import (
	"strings"
	"testing"
)

func TestDiagnoseMainThreadStallExplainsObservedICQStacks(t *testing.T) {
	tests := []struct {
		stack       string
		titlePart   string
		actionParts []string
	}{
		{
			stack:       "ru.mail.im.app.di.components.DaggerAppComponent$Builder.build(DaggerAppComponent.java:2368)",
			titlePart:   "Dagger-компонента",
			actionParts: []string{"предоставления зависимостей", "главного потока"},
		},
		{
			stack:       "ru.mail.im.statistics.event.StatParamValue$OnBoarding.<init>(StatParamValue.kt:308)",
			titlePart:   "конструктора",
			actionParts: []string{"конструктор", "инициализатор"},
		},
	}
	for _, test := range tests {
		diagnosis := DiagnoseMainThreadStall("", test.stack)
		if !strings.Contains(diagnosis.Title, test.titlePart) {
			t.Errorf("DiagnoseMainThreadStall(%q) title = %q", test.stack, diagnosis.Title)
		}
		for _, part := range test.actionParts {
			if !strings.Contains(diagnosis.Action, part) {
				t.Errorf("DiagnoseMainThreadStall(%q) action misses %q: %q", test.stack, part, diagnosis.Action)
			}
		}
		for _, forbidden := range []string{"stack snapshot", "module provider", "provider/getter", "leaf frame", "eager"} {
			if strings.Contains(strings.ToLower(diagnosis.Explanation+" "+diagnosis.Action), forbidden) {
				t.Errorf("DiagnoseMainThreadStall(%q) contains user-facing anglicism %q: %+v", test.stack, forbidden, diagnosis)
			}
		}
	}
}

func TestDiagnoseMainThreadStallDoesNotBlameLibraryConstructor(t *testing.T) {
	diagnosis := DiagnoseMainThreadStall(
		"com.google.android.material.appbar.AppBarLayout",
		"com.google.android.material.appbar.AppBarLayout.<init>(AppBarLayout.java:303)",
	)

	if !strings.Contains(diagnosis.Title, "библиотеки") {
		t.Fatalf("library frame is presented as an application cause: %+v", diagnosis)
	}
	if !strings.Contains(diagnosis.Explanation, "наблюдени") {
		t.Fatalf("diagnosis does not explain the evidence boundary: %+v", diagnosis)
	}
	if !strings.Contains(diagnosis.Action, "приложени") || !strings.Contains(diagnosis.Action, "layout") {
		t.Fatalf("diagnosis does not point to an actionable application callsite: %+v", diagnosis)
	}
}
