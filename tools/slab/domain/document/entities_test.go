package document_test

import (
	"encoding/json"
	"testing"

	"github.com/standards-lab/go-web-service/tools/slab/domain/document"
)

func TestBodies_MarshalUnderTheServiceFieldNames(t *testing.T) {
	for name, tc := range map[string]struct {
		body any
		want string
	}{
		"create directory": {document.CreateDirectory{ParentID: document.RootAlias, Name: "reports"}, `{"parent_id":"root","name":"reports"}`},
		"move directory":   {document.MoveDirectory{ParentID: "p1", Name: "archive"}, `{"parent_id":"p1","name":"archive"}`},
		"move file":        {document.MoveFile{DirectoryID: "d1", Name: "q3.pdf"}, `{"directory_id":"d1","name":"q3.pdf"}`},
	} {
		raw, err := json.Marshal(tc.body)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != tc.want {
			t.Errorf("%s: body = %s, want %s", name, raw, tc.want)
		}
	}
}

func TestIdentity_DecodesTheCommandEnvelope(t *testing.T) {
	var id document.Identity
	if err := json.Unmarshal([]byte(`{"id":"x","version":2}`), &id); err != nil {
		t.Fatal(err)
	}
	if id != (document.Identity{ID: "x", Version: 2}) {
		t.Errorf("Identity = %+v", id)
	}
}

func TestPages_AreTheSDKEnvelope(t *testing.T) {
	var dirs document.DirectoryPage
	if err := json.Unmarshal([]byte(`{"items":[{"id":"d","parent_id":"r","name":"reports","version":1}],"page":1,"size":20,"total":1}`), &dirs); err != nil {
		t.Fatal(err)
	}
	if dirs.Page != 1 || len(dirs.Items) != 1 || dirs.Items[0].Name != "reports" || dirs.Items[0].ParentID == nil || *dirs.Items[0].ParentID != "r" {
		t.Errorf("DirectoryPage = %+v", dirs)
	}
	var files document.FilePage
	if err := json.Unmarshal([]byte(`{"items":[{"id":"f","directory_id":"d","name":"a.txt","status":"available","size":3,"content_type":"text/plain","version":1}],"size":20}`), &files); err != nil {
		t.Fatal(err)
	}
	if len(files.Items) != 1 || files.Items[0].Status != "available" || files.Items[0].Size == nil || *files.Items[0].Size != 3 {
		t.Errorf("FilePage = %+v", files)
	}
}
