package analyze

import "testing"

func TestScenarioARTFixtureBuildsBoundedRootProfileWithOrderedStages(t *testing.T) {
	summary, err := InspectFilesWithOptions(
		"scenario ART",
		[]string{"../../../wire/testdata/scenario-telemetry-5.1.0.jhlog"},
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	analysis := summary.OperationAnalysis
	if analysis == nil || analysis.InvalidProfileSamples != 0 {
		t.Fatalf("operation profiles = %+v", analysis)
	}
	var roots []OperationProfile
	for _, profile := range analysis.Profiles {
		if profile.Root {
			roots = append(roots, profile)
		}
	}
	if len(roots) != 1 {
		t.Fatalf("root operation profiles = %+v", roots)
	}
	profile := roots[0]
	if !profile.Root || profile.Stats.Operation != "chat.open" || profile.Stats.Kind != "user" || profile.Stats.Count != 1 {
		t.Fatalf("root profile = %+v", profile)
	}
	if len(profile.Attributes) != 8 || profile.Attributes[0] != (OperationProfileAttribute{Key: "account", Value: "existing"}) {
		t.Fatalf("attributes = %+v", profile.Attributes)
	}
	wantSteps := []string{"load.messages", "render.messages", "prefetch.attachments"}
	if len(profile.Steps) != len(wantSteps) {
		t.Fatalf("steps = %+v", profile.Steps)
	}
	for index, want := range wantSteps {
		if profile.Steps[index].Operation != want || profile.Steps[index].Kind != "stage" {
			t.Errorf("step %d = %+v, want %s", index, profile.Steps[index], want)
		}
	}
}

func TestScenarioMarkersWithoutConfirmedStagesRemainUnknown(t *testing.T) {
	profile := matchTestProfile("chat.open", "01000000000000000000000000000000", 1)
	profile.Attributes = append(profile.Attributes,
		OperationProfileAttribute{Key: "jh.scenario", Value: "chat.open"},
		OperationProfileAttribute{Key: "jh.scenario_rev", Value: "1"},
	)
	profile.Steps = nil
	result := matchScenarioProfiles(
		&OperationAnalysis{Profiles: []OperationProfile{profile}},
		&OperationAnalysis{Profiles: []OperationProfile{profile}},
	)
	if result.Comparability != ScenarioUnknown || len(result.Matches) != 0 {
		t.Fatalf("unconfirmed stage sequence was matched: %+v", result)
	}
}
