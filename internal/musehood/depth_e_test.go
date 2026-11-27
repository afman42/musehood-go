package musehood

import "testing"

func TestDensitySetting(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"settings": object{"density": "compact"}}, 200)
	state := f.state()
	settingsMap, _ := state["settings"].(map[string]any)
	if settingsMap["density"] != "compact" {
		t.Fatalf("density harus tersimpan compact: %v", settingsMap["density"])
	}
	f.api("POST", "/api/settings", object{"settings": object{"density": "sempit"}}, 400)
	f.api("POST", "/api/settings", object{"settings": object{"density": "normal"}}, 200)
	state = f.state()
	settingsMap, _ = state["settings"].(map[string]any)
	if settingsMap["density"] != "normal" {
		t.Fatalf("density harus kembali normal: %v", settingsMap["density"])
	}
}
