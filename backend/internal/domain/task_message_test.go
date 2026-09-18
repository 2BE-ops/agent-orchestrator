package domain

import (
	"strings"
	"testing"
)

func TestTaskMessageSchemaBoundsAndTypedContracts(t *testing.T) {
	base := TaskMessageDefinition{SchemaVersion: 1, Kind: "finding", TargetTaskID: "target", Subject: "Discovery", Body: "Worker-reported information", CorrelationID: "thread"}
	for _, kind := range []string{"finding", "question", "answer", "blocker", "handoff", "interface_contract", "review_request", "dependency_update"} {
		d := base
		d.Kind = kind
		if kind == "answer" {
			d.ReplyToID = "question"
		}
		if kind == "interface_contract" {
			d.Interface = &TaskInterfaceClaim{Name: "API", Contract: "Stable request shape", Files: []string{"api/schema.json"}}
		}
		if err := d.Validate(); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*TaskMessageDefinition)
	}{
		{"schema", func(d *TaskMessageDefinition) { d.SchemaVersion = 2 }},
		{"kind", func(d *TaskMessageDefinition) { d.Kind = "execute" }},
		{"blank", func(d *TaskMessageDefinition) { d.Subject = " " }},
		{"body", func(d *TaskMessageDefinition) { d.Body = strings.Repeat("a", 16001) }},
		{"encoded", func(d *TaskMessageDefinition) {
			d.Body = strings.Repeat("\t", 15999) + "x"
			d.Subject = strings.Repeat("\t", 299) + "x"
			d.Kind = "interface_contract"
			d.Interface = &TaskInterfaceClaim{Name: "API", Contract: strings.Repeat("a", 8000)}
		}},
		{"control", func(d *TaskMessageDefinition) { d.TargetTaskID = "target\n" }},
		{"null", func(d *TaskMessageDefinition) { d.Body = "a\x00b" }},
		{"answer", func(d *TaskMessageDefinition) { d.Kind = "answer" }},
		{"contract", func(d *TaskMessageDefinition) { d.Kind = "interface_contract" }},
		{"extra_contract", func(d *TaskMessageDefinition) { d.Interface = &TaskInterfaceClaim{Name: "API", Contract: "shape"} }},
		{"unsafe_file", func(d *TaskMessageDefinition) {
			d.Kind = "interface_contract"
			d.Interface = &TaskInterfaceClaim{Name: "API", Contract: "shape", Files: []string{"../key"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := base
			tc.edit(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("invalid message accepted")
			}
		})
	}
	for _, state := range []string{"handed_off", "not_sent", "uncertain"} {
		if err := (TaskMessageDeliveryResolution{ID: "delivery", State: state, Reason: "Observed outcome"}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if err := (TaskMessageDeliveryResolution{ID: "delivery", State: "dispatching", Reason: "Retry"}).Validate(); err == nil {
		t.Fatal("resolution authorized replay")
	}
}
