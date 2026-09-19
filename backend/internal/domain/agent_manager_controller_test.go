package domain

import (
	"testing"
	"time"
)

func TestAgentManagerAdmissionRejectsUntrustedActorsAndUnboundedIdentity(t *testing.T) {
	valid := AgentManagerControllerReservation{AgentManagerControllerToken: AgentManagerControllerToken{ID: "controller", ProjectID: "project", ConfigurationVersion: 1}, Actor: AdaptiveActor{Kind: "SYSTEM", ID: "daemon"}, Reason: "Start configured Manager", Now: time.Now().UTC()}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*AgentManagerControllerReservation){
		"manager": func(r *AgentManagerControllerReservation) { r.Actor.Kind = "AGENT_MANAGER" },
		"worker":  func(r *AgentManagerControllerReservation) { r.Actor.Kind = "WORKER" },
		"orchestrator": func(r *AgentManagerControllerReservation) {
			r.Actor.Kind = "ORCHESTRATOR"
			r.Actor.SessionID = "planner"
		},
		"borrowed session":  func(r *AgentManagerControllerReservation) { r.Actor.SessionID = "worker" },
		"control character": func(r *AgentManagerControllerReservation) { r.ID = "bad\nid" },
		"empty project":     func(r *AgentManagerControllerReservation) { r.ProjectID = "" },
		"floating version":  func(r *AgentManagerControllerReservation) { r.ConfigurationVersion = 0 },
		"unbounded version": func(r *AgentManagerControllerReservation) { r.ConfigurationVersion = 1001 },
		"missing reason":    func(r *AgentManagerControllerReservation) { r.Reason = "" },
		"missing time":      func(r *AgentManagerControllerReservation) { r.Now = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			r := valid
			change(&r)
			if r.Validate() == nil {
				t.Fatal("invalid admission accepted")
			}
		})
	}
}
