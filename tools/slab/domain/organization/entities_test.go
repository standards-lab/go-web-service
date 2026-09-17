package organization_test

import (
	"encoding/json"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/domain/organization"
)

func TestCreateOrganization_MarshalsUnderTheServiceFieldNames(t *testing.T) {
	parent := "00000000-0000-0000-0000-000000000001"
	raw, err := json.Marshal(organization.CreateOrganization{ParentID: &parent, Code: "sales", Name: "Sales"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":"` + parent + `","code":"sales","name":"Sales"}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
	raw, err = json.Marshal(organization.CreateOrganization{Code: "acme", Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":null,"code":"acme","name":"Acme"}`; string(raw) != want {
		t.Errorf("root body = %s, want %s", raw, want)
	}
}

func TestEditOrganization_MarshalsUnderTheServiceFieldNames(t *testing.T) {
	raw, err := json.Marshal(organization.EditOrganization{Code: "finance", Name: "Finance"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":"finance","name":"Finance"}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
}

// The service requires the parent_id key to be present, so a nil parent
// must marshal as an explicit null rather than be omitted.
func TestTransferOrganization_AlwaysCarriesTheParentKey(t *testing.T) {
	raw, err := json.Marshal(organization.TransferOrganization{})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":null}`; string(raw) != want {
		t.Errorf("root body = %s, want %s", raw, want)
	}
	parent := "p1"
	raw, err = json.Marshal(organization.TransferOrganization{ParentID: &parent})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"parent_id":"p1"}`; string(raw) != want {
		t.Errorf("body = %s, want %s", raw, want)
	}
}

func TestIdentity_DecodesTheCommandEnvelope(t *testing.T) {
	var id organization.Identity
	if err := json.Unmarshal([]byte(`{"id":"x","version":2}`), &id); err != nil {
		t.Fatal(err)
	}
	if id != (organization.Identity{ID: "x", Version: 2}) {
		t.Errorf("Identity = %+v", id)
	}
}

func TestPage_IsTheSDKEnvelope(t *testing.T) {
	var p organization.Page
	if err := json.Unmarshal([]byte(`{"items":[{"id":"x","parent_id":null,"code":"acme","name":"Acme","version":1,"path":"/acme"}],"page":1,"size":20,"total":1}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Page != 1 || p.Size != 20 || p.Total != 1 || len(p.Items) != 1 || p.Items[0].Path != "/acme" {
		t.Errorf("Page = %+v", p)
	}
}
