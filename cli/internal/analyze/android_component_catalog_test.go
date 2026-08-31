package analyze

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndroidComponentCatalogBuildsStableLookups(t *testing.T) {
	path := writeAndroidComponentCatalogFixture(t, strings.Join([]string{
		`{"format":1,"class":"com.example.SyncService","componentId":"stable:0x0000000000000011","kind":"service","abstract":false,"coverage":"full","entryPoints":["onCreate()V"],"instrumentedEntryPoints":["onCreate()V"],"uncoveredEntryPoints":[],"transactions":[]}`,
		`{"format":1,"class":"com.example.ISync$Stub","componentId":"stable:0x0000000000000012","kind":"aidl_stub","abstract":true,"coverage":"full","entryPoints":["onTransact(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z"],"instrumentedEntryPoints":["onTransact(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z"],"uncoveredEntryPoints":[],"aidlDescriptor":"com.example.ISync","transactions":[{"code":7,"method":"refresh"}]}`,
	}, "\n")+"\n")

	catalog, err := LoadAndroidComponentCatalog(path)
	if err != nil {
		t.Fatalf("LoadAndroidComponentCatalog() error = %v", err)
	}
	if catalog == nil || !catalog.Available || len(catalog.Components) != 2 {
		t.Fatalf("catalog = %+v", catalog)
	}
	component, ok := catalog.Component(0x11)
	if !ok || component.ClassName != "com.example.SyncService" || component.Coverage != "full" {
		t.Fatalf("component lookup = %+v, %t", component, ok)
	}
	method, ok := catalog.AIDLMethod("com.example.ISync", 7)
	if !ok || method != "refresh" {
		t.Fatalf("AIDL lookup = %q, %t", method, ok)
	}
}

func TestLoadAndroidComponentCatalogRejectsConflictingAIDLTransaction(t *testing.T) {
	path := writeAndroidComponentCatalogFixture(t, strings.Join([]string{
		`{"format":1,"class":"com.example.ISync$Stub","componentId":"stable:0x0000000000000011","kind":"aidl_stub","abstract":true,"coverage":"none","entryPoints":[],"instrumentedEntryPoints":[],"uncoveredEntryPoints":[],"aidlDescriptor":"com.example.ISync","transactions":[{"code":7,"method":"refresh"}]}`,
		`{"format":1,"class":"com.example.OtherStub","componentId":"stable:0x0000000000000012","kind":"aidl_stub","abstract":true,"coverage":"none","entryPoints":[],"instrumentedEntryPoints":[],"uncoveredEntryPoints":[],"aidlDescriptor":"com.example.ISync","transactions":[{"code":7,"method":"delete"}]}`,
	}, "\n")+"\n")

	_, err := LoadAndroidComponentCatalog(path)
	if err == nil || !strings.Contains(err.Error(), "conflicting AIDL transaction") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadAndroidComponentCatalogRejectsInvalidStableID(t *testing.T) {
	path := writeAndroidComponentCatalogFixture(t,
		`{"format":1,"class":"com.example.SyncService","componentId":"0x11","kind":"service","abstract":false,"coverage":"full","entryPoints":[],"instrumentedEntryPoints":[],"uncoveredEntryPoints":[],"transactions":[]}`+"\n",
	)

	_, err := LoadAndroidComponentCatalog(path)
	if err == nil || !strings.Contains(err.Error(), "componentId") {
		t.Fatalf("error = %v", err)
	}
}

func writeAndroidComponentCatalogFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "android-components-catalog.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
