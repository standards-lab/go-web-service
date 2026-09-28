package document_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/blobfs"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/sqlate/sqltest"

	"github.com/standards-lab/go-web-service/domain/document"
)

// module compiles the layer's route group the way the composition root
// does, over the scripted driver with the given responses; with none
// scripted, only the handler-local rejection paths can answer.
func module(t *testing.T, responses ...sqltest.Response) http.Handler {
	t.Helper()
	svc, _, _ := serviceOver(t, responses...)
	return routes(svc)
}

// routes mounts the layer's route group over svc as its own module.
func routes(svc *document.Service) http.Handler {
	r := web.NewRouter()
	r.Mount(web.NewModule(document.Routes(svc, web.Limits{DefaultSize: 20, MaxSize: 100, Cursor: true})))
	return r
}

// send sends a request with an optional If-Match and body; a Content-Type
// makes it an upload of the body at its length.
func send(t *testing.T, h http.Handler, method, path, ifMatch, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if ifMatch != "" {
		r.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func problem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, web.ProblemMediaType)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return body
}

const base = "/documents/" + orgID

// Every refusal the handler or a command can make from the request alone
// answers before any statement runs: the module has nothing scripted.
func TestRoutes_RejectBeforeAnyOperation(t *testing.T) {
	cases := map[string]struct {
		method, path, ifMatch, body string
		status                      int
		detail                      string
	}{
		"malformed organization":          {"GET", "/documents/nope/directories/root", "", "", 400, "must be a UUID"},
		"malformed directory id":          {"GET", base + "/directories/nope", "", "", 400, "must be a UUID"},
		"malformed file id":               {"GET", base + "/files/nope", "", "", 400, "must be a UUID"},
		"list: malformed page":            {"GET", base + "/directories/root/files?page=x", "", "", 400, "query page"},
		"list: oversized page":            {"GET", base + "/directories/root/directories?size=1000", "", "", 400, "query size"},
		"create: malformed body":          {"POST", base + "/directories", "", "{", 400, "body:"},
		"create: unknown field":           {"POST", base + "/directories", "", `{"parent":"root","name":"a"}`, 400, "parent"},
		"create: no parent":               {"POST", base + "/directories", "", `{"name":"a"}`, 400, "parent_id is required"},
		"create: bad parent":              {"POST", base + "/directories", "", `{"parent_id":"nope","name":"a"}`, 400, "parent_id must be root or a UUID"},
		"create: empty name":              {"POST", base + "/directories", "", `{"parent_id":"root","name":""}`, 400, "name"},
		"create: slash in name":           {"POST", base + "/directories", "", `{"parent_id":"root","name":"a/b"}`, 400, "name"},
		"delete: bad recursive":           {"DELETE", base + "/directories/root?recursive=maybe", `"1"`, "", 400, "recursive must be"},
		"delete directory: no If-Match":   {"DELETE", base + "/directories/" + dirID, "", "", 428, "If-Match"},
		"recursive delete: no If-Match":   {"DELETE", base + "/directories/" + dirID + "?recursive=true", "", "", 428, "If-Match"},
		"delete directory: weak If-Match": {"DELETE", base + "/directories/" + dirID, `W/"1"`, "", 400, "If-Match"},
		"delete file: no If-Match":        {"DELETE", base + "/files/" + fileID, "", "", 428, "If-Match"},
		"delete file: weak If-Match":      {"DELETE", base + "/files/" + fileID, `W/"1"`, "", 400, "If-Match"},
		"move directory: no If-Match":     {"POST", base + "/directories/" + dirID + "/move", "", `{"parent_id":"root","name":"a"}`, 428, "If-Match"},
		"move directory: weak If-Match":   {"POST", base + "/directories/" + dirID + "/move", `W/"1"`, `{"parent_id":"root","name":"a"}`, 400, "If-Match"},
		"move directory: key omitted":     {"POST", base + "/directories/" + dirID + "/move", `"1"`, `{"name":"a"}`, 400, "parent_id is required"},
		"move directory: the root":        {"POST", base + "/directories/root/move", `"1"`, `{"parent_id":"root","name":"a"}`, 400, "document root cannot be moved"},
		"move file: no If-Match":          {"POST", base + "/files/" + fileID + "/move", "", `{"directory_id":"root","name":"a"}`, 428, "If-Match"},
		"move file: malformed id":         {"POST", base + "/files/nope/move", `"1"`, `{"directory_id":"root","name":"a"}`, 400, "must be a UUID"},
		"move file: bad directory":        {"POST", base + "/files/" + fileID + "/move", `"1"`, `{"directory_id":"x","name":"a"}`, 400, "directory_id must be root or a UUID"},
		"move file: missing name":         {"POST", base + "/files/" + fileID + "/move", `"1"`, `{"directory_id":"root"}`, 400, "name"},
		"move file: malformed body":       {"POST", base + "/files/" + fileID + "/move", `"1"`, `[`, 400, "body:"},
		"move directory on PUT unrouted":  {"PUT", base + "/directories/" + dirID + "/move", `"1"`, `{}`, 405, ""},
	}
	h := module(t)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, h, c.method, c.path, c.ifMatch, c.body)
			if c.detail == "" {
				if rec.Code != c.status {
					t.Fatalf("status = %d, want %d", rec.Code, c.status)
				}
				return
			}
			body := problem(t, rec, c.status)
			if detail, _ := body["detail"].(string); !strings.Contains(detail, c.detail) {
				t.Errorf("detail = %q, want it to contain %q", detail, c.detail)
			}
		})
	}
}

// upload sends a raw body with the given Content-Type, none when empty,
// and declared length; a length of -1 is a chunked body.
func upload(t *testing.T, h http.Handler, path, contentType, body string, length int64) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("PUT", path, strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	r.ContentLength = length
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// An upload the layer would refuse is refused on its path and headers,
// before any statement runs or byte is stored.
func TestPutFile_RefusesBeforeAnyIO(t *testing.T) {
	path := base + "/directories/root/files/report.txt"
	cases := map[string]struct {
		path, contentType string
		length            int64
		status            int
		detail            string
	}{
		"malformed directory": {base + "/directories/nope/files/report.txt", "text/plain", 6, 400, "must be a UUID"},
		"missing type":        {path, "", 6, 415, "Content-Type"},
		"chunked body":        {path, "text/plain", -1, 411, "Content-Length"},
		"over 10 MiB":         {path, "text/plain", 10<<20 + 1, 413, "exceed"},
		"a slash in the name": {base + "/directories/root/files/a%2Fb", "text/plain", 6, 400, "name"},
	}
	h := module(t)
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			body := problem(t, upload(t, h, c.path, c.contentType, "report", c.length), c.status)
			if detail, _ := body["detail"].(string); !strings.Contains(detail, c.detail) {
				t.Errorf("detail = %q, want it to contain %q", detail, c.detail)
			}
		})
	}
}

// Any media type is accepted, and the upload answers 201 with the new
// file's identity and its metadata as the Location.
func TestPutFile_AnswersCreatedWithLocation(t *testing.T) {
	h := module(t,
		root(rootID), within(true), fileRows(file(fileID, dirID, blobfs.StatusPending, 1)),
		fileRows(file(fileID, dirID, blobfs.StatusAvailable, 2)),
	)
	rec := upload(t, h, base+"/directories/"+dirID+"/files/page.html", "text/html", "<p>hi</p>", 9)
	if rec.Code != 201 || rec.Header().Get("Location") != base+"/files/"+fileID || !strings.Contains(rec.Body.String(), `"id":"`+fileID+`","version":2`) {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
}

// A taken name is the unique violation blobfs classifies, a conflict.
func TestPutFile_ATakenNameIs409(t *testing.T) {
	violation := sqltest.Response{Err: takenName()}
	h := module(t, root(rootID), within(true), violation)
	problem(t, upload(t, h, base+"/directories/"+dirID+"/files/report.txt", "text/plain", "report", 6), 409)
}

func TestCreateDirectory_AnswersCreatedWithLocation(t *testing.T) {
	h := module(t, root(rootID), root(rootID), dirRows(directory(dirID, rootID, "reports", 1)))
	rec := send(t, h, "POST", base+"/directories", "", `{"parent_id":"root","name":"reports"}`)
	if rec.Code != 201 || rec.Header().Get("Location") != base+"/directories/"+dirID {
		t.Fatalf("status %d, Location %q, body %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
}

// The scope check: an id outside the organization's root is not found,
// exactly as an absent id is, and so is any id of an organization with no
// root.
func TestRoutes_OutsideTheRootIs404(t *testing.T) {
	cases := map[string]struct {
		responses    []sqltest.Response
		method, path string
		ifMatch      string
		body         string
	}{
		"directory read":   {[]sqltest.Response{root(rootID), within(false)}, "GET", base + "/directories/" + otherID, "", ""},
		"directory list":   {[]sqltest.Response{root(rootID), within(false)}, "GET", base + "/directories/" + otherID + "/files", "", ""},
		"directory delete": {[]sqltest.Response{root(rootID), within(false)}, "DELETE", base + "/directories/" + otherID, `"1"`, ""},
		"branch delete":    {[]sqltest.Response{root(rootID), within(false)}, "DELETE", base + "/directories/" + otherID + "?recursive=true", `"1"`, ""},
		"directory move": {
			[]sqltest.Response{root(rootID), within(false)},
			"POST", base + "/directories/" + otherID + "/move", `"1"`, `{"parent_id":"root","name":"a"}`,
		},
		"file read":     {[]sqltest.Response{root(rootID), fileRows(file(fileID, otherID, blobfs.StatusAvailable, 2)), within(false)}, "GET", base + "/files/" + fileID, "", ""},
		"file content":  {[]sqltest.Response{root(rootID), fileRows(file(fileID, otherID, blobfs.StatusAvailable, 2)), within(false)}, "GET", base + "/files/" + fileID + "/content", "", ""},
		"file delete":   {[]sqltest.Response{root(rootID), fileRows(file(fileID, otherID, blobfs.StatusAvailable, 2)), within(false)}, "DELETE", base + "/files/" + fileID, `"2"`, ""},
		"upload":        {[]sqltest.Response{root(rootID), within(false)}, "PUT", base + "/directories/" + otherID + "/files/a.txt", "", ""},
		"absent file":   {[]sqltest.Response{root(rootID), fileRows()}, "GET", base + "/files/" + fileID, "", ""},
		"no root":       {[]sqltest.Response{root()}, "GET", base + "/directories/" + dirID, "", ""},
		"no root, list": {[]sqltest.Response{root()}, "GET", base + "/directories/" + dirID + "/directories", "", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := module(t, c.responses...)
			if c.method == "PUT" {
				problem(t, upload(t, h, c.path, "text/plain", "x", 1), 404)
				return
			}
			problem(t, send(t, h, c.method, c.path, c.ifMatch, c.body), 404)
		})
	}
}

// An organization without a root yet lists an empty page at its alias.
func TestListFiles_BeforeTheRootIsAnEmptyPage(t *testing.T) {
	rec := send(t, module(t, root()), "GET", base+"/directories/root/files", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
}

// The download is an attachment, never rendered inline, whatever the
// stored type: the filename quoted, and a name outside ASCII carried
// exactly in filename*.
func TestContent_IsAnAttachment(t *testing.T) {
	cases := map[string]struct{ name, disposition string }{
		"plain":     {"report.txt", `attachment; filename="report.txt"`},
		"quoted":    {`a "b"\c.html`, `attachment; filename="a \"b\"\\c.html"`},
		"non-ascii": {"résumé 1.pdf", `attachment; filename="r_sum_ 1.pdf"; filename*=UTF-8''r%C3%A9sum%C3%A9%201.pdf`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := file(fileID, dirID, blobfs.StatusAvailable, 2)
			f.Name, f.ContentType = c.name, "text/html"
			svc, _, fake := serviceOver(t, root(rootID), fileRows(f), within(true))
			if _, err := fake.Put(context.Background(), f.Key, strings.NewReader("report"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			rec := send(t, routes(svc), "GET", base+"/files/"+fileID+"/content", "", "")
			if rec.Code != 200 {
				t.Fatalf("status %d, body %s", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Content-Disposition"); got != c.disposition {
				t.Errorf("Content-Disposition = %s, want %s", got, c.disposition)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != `"etag-`+fileID+`"` || rec.Body.String() != "report" {
				t.Errorf("headers = %v", rec.Header())
			}
		})
	}
}

// A file whose write has not completed has no content to serve.
func TestContent_OfAPendingFileIs404(t *testing.T) {
	h := module(t, root(rootID), fileRows(file(fileID, dirID, blobfs.StatusPending, 1)), within(true))
	problem(t, send(t, h, "GET", base+"/files/"+fileID+"/content", "", ""), 404)
}

// Without recursive, a directory with contents is a conflict.
func TestDeleteDirectory_NotEmptyIs409(t *testing.T) {
	h := module(t, root(rootID), within(true), sqltest.Response{Err: notEmpty()})
	problem(t, send(t, h, "DELETE", base+"/directories/"+dirID, `"1"`, ""), 409)
}

// A directory read carries its status, and a directory in a branch marked
// for its delete still reads by id, deleting.
func TestDirectory_CarriesItsStatus(t *testing.T) {
	ancestors := sqltest.Response{
		Columns: []string{"id", "parent_id", "name"},
		Rows:    [][]driver.Value{{dirID, rootID, "reports"}, {rootID, blobfs.RootID, orgID}, {blobfs.RootID, nil, "/"}},
	}
	cases := map[string]struct {
		dir  blobfs.Directory
		want string
	}{
		"active":   {directory(dirID, rootID, "reports", 1), `"status":"active"`},
		"deleting": {deleting(directory(dirID, rootID, "reports", 1)), `"status":"deleting"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := module(t, root(rootID), within(true), dirRows(c.dir), ancestors)
			rec := send(t, h, "GET", base+"/directories/"+dirID, "", "")
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), c.want) {
				t.Fatalf("status %d, body %s; want %s", rec.Code, rec.Body, c.want)
			}
		})
	}
}

// A listing within a branch being deleted is not found, not a conflict:
// the directory still reads by id, but it lists nothing.
func TestListings_WithinADeletingBranchAre404(t *testing.T) {
	marked := deleting(directory(dirID, rootID, "reports", 1))
	cases := map[string]sqltest.Response{
		"directories": sqltest.WithTotal(dirRows(), 0),
		"files":       sqltest.WithTotal(fileRows(), 0),
	}
	for listing, page := range cases {
		t.Run(listing, func(t *testing.T) {
			h := module(t, root(rootID), within(true), page, dirRows(marked))
			problem(t, send(t, h, "GET", base+"/directories/"+dirID+"/"+listing, "", ""), 404)
		})
	}
}

// A conflict answers 409 with its fixed detail and none of the error's own
// text, which names blobfs's operation, its ids, and its constraints: an
// upload under a taken name, the delete of a directory with contents, and
// a create under a directory whose branch is being deleted.
func TestRoutes_ConflictsCarryAFixedDetail(t *testing.T) {
	marked := dirRows(deleting(directory(dirID, rootID, "reports", 1)))
	cases := map[string]struct {
		responses                   []sqltest.Response
		method, path, ifMatch, body string
		detail                      string
	}{
		"taken name": {
			[]sqltest.Response{root(rootID), within(true), {Err: takenName()}},
			"PUT", base + "/directories/" + dirID + "/files/report.txt", "", "report",
			"an entry with that name already exists",
		},
		"not empty": {
			[]sqltest.Response{root(rootID), within(true), {Err: notEmpty()}},
			"DELETE", base + "/directories/" + dirID, `"1"`, "",
			"the directory is not empty",
		},
		"deleting parent": {
			// The insert selects nothing from the deleting parent, and blobfs
			// reads the parent, twice, to tell deleting from missing.
			[]sqltest.Response{root(rootID), within(true), dirRows(), marked, marked},
			"POST", base + "/directories", "", `{"parent_id":"` + dirID + `","name":"q3"}`,
			"the directory is being deleted",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := module(t, c.responses...)
			var rec *httptest.ResponseRecorder
			if c.method == "PUT" {
				rec = upload(t, h, c.path, "text/plain", c.body, int64(len(c.body)))
			} else {
				rec = send(t, h, c.method, c.path, c.ifMatch, c.body)
			}
			body := problem(t, rec, 409)
			if body["detail"] != c.detail {
				t.Errorf("detail = %v, want %q", body["detail"], c.detail)
			}
			for _, leak := range []string{"data:", "blobfs", "constraint", "_fk_", "_uq_", rootID} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("body %s carries %q", rec.Body, leak)
				}
			}
		})
	}
}

// A recursive delete answers 202 with no body once the branch is marked,
// its Location the directory's read, and a repeated one, the mark's retry,
// is accepted again. The root's alias locates the root by its id.
func TestDeleteDirectory_RecursiveIs202(t *testing.T) {
	cases := map[string]struct {
		responses []sqltest.Response
		id, want  string
	}{
		"a directory": {[]sqltest.Response{root(rootID), within(true), exec(1), exec(1)}, dirID, dirID},
		"a retry": {
			[]sqltest.Response{root(rootID), within(true), exec(0), dirRows(deleting(directory(dirID, rootID, "reports", 1))), exec(0)},
			dirID, dirID,
		},
		"the root": {[]sqltest.Response{root(rootID), exec(1), exec(0)}, document.RootAlias, rootID},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, module(t, c.responses...), "DELETE", base+"/directories/"+c.id+"?recursive=true", `"1"`, "")
			if rec.Code != 202 || rec.Header().Get("Location") != base+"/directories/"+c.want || rec.Body.Len() != 0 {
				t.Fatalf("status %d, Location %q, body %q", rec.Code, rec.Header().Get("Location"), rec.Body)
			}
		})
	}
}

// The deletes answer 204, and 412 at a version the row no longer holds.
func TestDeletes_GuardOnIfMatch(t *testing.T) {
	cases := map[string]struct {
		responses     []sqltest.Response
		path, ifMatch string
		status        int
	}{
		"empty directory": {[]sqltest.Response{root(rootID), within(true), exec(1)}, "/directories/" + dirID, `"1"`, 204},
		"stale directory": {
			[]sqltest.Response{root(rootID), within(true), exec(0), dirRows(directory(dirID, rootID, "reports", 2))},
			"/directories/" + dirID, `"1"`, 412,
		},
		"stale branch": {
			[]sqltest.Response{root(rootID), within(true), exec(0), dirRows(directory(dirID, rootID, "reports", 2))},
			"/directories/" + dirID + "?recursive=true", `"1"`, 412,
		},
		"stale file": {
			[]sqltest.Response{root(rootID), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 3)), within(true), fileRows(), fileRows(file(fileID, dirID, blobfs.StatusAvailable, 3))},
			"/files/" + fileID, `"2"`, 412,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, module(t, c.responses...), "DELETE", base+c.path, c.ifMatch, "")
			if c.status == 204 {
				if rec.Code != 204 {
					t.Fatalf("status %d, body %s", rec.Code, rec.Body)
				}
				return
			}
			problem(t, rec, c.status)
		})
	}
}
