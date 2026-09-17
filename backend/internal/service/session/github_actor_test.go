package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type fakeIdentityResolver struct {
	identity ports.SCMIdentity
	err      error
	calls    int
}

func (f *fakeIdentityResolver) AuthenticatedIdentity(context.Context) (ports.SCMIdentity, error) {
	f.calls++
	return f.identity, f.err
}

func TestGithubActorGatesOnIdentity(t *testing.T) {
	human := ports.SCMIdentity{Login: "octocat", Human: true}
	cases := []struct {
		name      string
		identity  ports.SCMIdentityResolver
		wantLogin string
		wantOK    bool
	}{
		{
			name:      "human account resolves",
			identity:  &fakeIdentityResolver{identity: human},
			wantLogin: "octocat",
			wantOK:    true,
		},
		{
			name:     "resolver nil stays anonymous",
			identity: nil,
		},
		{
			name:     "identity error stays anonymous",
			identity: &fakeIdentityResolver{err: errors.New("GET /user failed")},
		},
		{
			name:     "non-human account stays anonymous",
			identity: &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "acme-org", Human: false}},
		},
		{
			name:     "empty login stays anonymous",
			identity: &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "", Human: true}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{githubIdentity: tc.identity}
			login, ok := svc.githubActor(context.Background())
			if ok != tc.wantOK || login != tc.wantLogin {
				t.Fatalf("githubActor = (%q, %v), want (%q, %v)", login, ok, tc.wantLogin, tc.wantOK)
			}
		})
	}
}

func TestEmitSpawnedCarriesGithubActor(t *testing.T) {
	sink := &fakeTelemetrySink{}
	svc := NewWithDeps(Deps{
		Telemetry:      sink,
		GithubIdentity: &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "octocat", Human: true}},
		Clock:          func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})

	svc.emitSpawned(context.Background(), domain.SessionRecord{ID: "sess-1", ProjectID: "proj-1", Kind: domain.KindWorker, Harness: "claude-code"}, 12)

	if len(sink.events) != 1 {
		t.Fatalf("telemetry events = %d, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Payload["github_actor"] != "octocat" {
		t.Fatalf("payload.github_actor = %#v, want octocat", ev.Payload["github_actor"])
	}
	if ev.PersonSet["github_actor"] != "octocat" {
		t.Fatalf("personSet.github_actor = %#v, want octocat", ev.PersonSet["github_actor"])
	}
}

func TestEmitSpawnedStaysAnonymousWithoutResolver(t *testing.T) {
	sink := &fakeTelemetrySink{}
	svc := NewWithDeps(Deps{
		Telemetry: sink,
		Clock:     func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})

	svc.emitSpawned(context.Background(), domain.SessionRecord{ID: "sess-1", ProjectID: "proj-1", Kind: domain.KindWorker, Harness: "claude-code"}, 12)

	if len(sink.events) != 1 {
		t.Fatalf("telemetry events = %d, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if _, ok := ev.Payload["github_actor"]; ok {
		t.Fatalf("payload should omit github_actor: %#v", ev.Payload)
	}
	if ev.PersonSet != nil {
		t.Fatalf("personSet should be nil without a resolver: %#v", ev.PersonSet)
	}
}
