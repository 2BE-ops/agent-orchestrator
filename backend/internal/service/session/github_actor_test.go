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

type fakeConsentReader struct {
	enabled bool
	err     error
}

func (f *fakeConsentReader) ReadGithubIdentityConsent(context.Context) (bool, error) {
	return f.enabled, f.err
}

func TestGithubActorGatesOnConsentAndIdentity(t *testing.T) {
	human := ports.SCMIdentity{Login: "octocat", Human: true}
	cases := []struct {
		name      string
		identity  ports.SCMIdentityResolver
		consent   ports.GithubIdentityConsentReader
		wantLogin string
		wantOK    bool
	}{
		{
			name:      "consent on and human resolves",
			identity:  &fakeIdentityResolver{identity: human},
			consent:   &fakeConsentReader{enabled: true},
			wantLogin: "octocat",
			wantOK:    true,
		},
		{
			name:     "consent off stays anonymous",
			identity: &fakeIdentityResolver{identity: human},
			consent:  &fakeConsentReader{enabled: false},
		},
		{
			name:     "consent read error stays anonymous",
			identity: &fakeIdentityResolver{identity: human},
			consent:  &fakeConsentReader{err: errors.New("unsafe policy file")},
		},
		{
			name:     "identity resolver nil stays anonymous",
			identity: nil,
			consent:  &fakeConsentReader{enabled: true},
		},
		{
			name:     "consent reader nil stays anonymous",
			identity: &fakeIdentityResolver{identity: human},
			consent:  nil,
		},
		{
			name:     "identity error stays anonymous",
			identity: &fakeIdentityResolver{err: errors.New("GET /user failed")},
			consent:  &fakeConsentReader{enabled: true},
		},
		{
			name:     "non-human account stays anonymous",
			identity: &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "acme-org", Human: false}},
			consent:  &fakeConsentReader{enabled: true},
		},
		{
			name:     "empty login stays anonymous",
			identity: &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "", Human: true}},
			consent:  &fakeConsentReader{enabled: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{githubIdentity: tc.identity, telemetryConsent: tc.consent}
			login, ok := svc.githubActor(context.Background())
			if ok != tc.wantOK || login != tc.wantLogin {
				t.Fatalf("githubActor = (%q, %v), want (%q, %v)", login, ok, tc.wantLogin, tc.wantOK)
			}
		})
	}
}

func TestEmitSpawnedCarriesGithubActorWhenConsented(t *testing.T) {
	sink := &fakeTelemetrySink{}
	svc := NewWithDeps(Deps{
		Telemetry:        sink,
		GithubIdentity:   &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "octocat", Human: true}},
		TelemetryConsent: &fakeConsentReader{enabled: true},
		Clock:            func() time.Time { return time.Unix(1700000000, 0).UTC() },
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

func TestEmitSpawnedStaysAnonymousWithoutConsent(t *testing.T) {
	sink := &fakeTelemetrySink{}
	svc := NewWithDeps(Deps{
		Telemetry:        sink,
		GithubIdentity:   &fakeIdentityResolver{identity: ports.SCMIdentity{Login: "octocat", Human: true}},
		TelemetryConsent: &fakeConsentReader{enabled: false},
		Clock:            func() time.Time { return time.Unix(1700000000, 0).UTC() },
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
		t.Fatalf("personSet should be nil without consent: %#v", ev.PersonSet)
	}
}
