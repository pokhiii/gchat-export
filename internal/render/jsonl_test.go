package render

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWriteJSONLGolden(t *testing.T) {
	var buf bytes.Buffer
	for _, m := range fixtureMessages() {
		if err := WriteJSONL(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	golden(t, "messages.golden.jsonl", buf.Bytes())
}

func TestWriteJSONLSchema(t *testing.T) {
	var buf bytes.Buffer
	WriteJSONL(&buf, fixtureMessages()[2])
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "thread_id", "is_thread_reply", "time", "edited_time", "sender_id", "sender_name", "text", "reactions", "attachments", "quoted_message_id"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	if rec["edited_time"] != nil || rec["quoted_message_id"] != nil {
		t.Errorf("empty optionals not null: %v", rec)
	}
	if r, ok := rec["reactions"].([]any); !ok || len(r) != 0 {
		t.Errorf("reactions = %v, want []", rec["reactions"])
	}
	if !strings.Contains(buf.String(), "<b>bold</b> & more") {
		t.Error("HTML escaping should be off in JSONL")
	}
	if strings.Count(buf.String(), "\n") != 1 {
		t.Error("record is not a single line")
	}
}

func TestJSONLRoundTrip(t *testing.T) {
	in := fixtureMessages()
	var buf bytes.Buffer
	for _, m := range in {
		WriteJSONL(&buf, m)
	}
	out, err := ReadJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		for j := range in[i].Attachments {
			in[i].Attachments[j].ResourceName = ""
		}
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch:\n in  %+v\n out %+v", in, out)
	}
}

func TestJSONLOmitsResourceName(t *testing.T) {
	var buf bytes.Buffer
	WriteJSONL(&buf, fixtureMessages()[0])
	if strings.Contains(buf.String(), "res-1") {
		t.Fatal("resource name leaked into JSONL")
	}
}

func TestReadJSONLBadLine(t *testing.T) {
	var buf bytes.Buffer
	WriteJSONL(&buf, fixtureMessages()[0])
	buf.WriteString("{not json\n")
	if _, err := ReadJSONL(&buf); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v", err)
	}
}
