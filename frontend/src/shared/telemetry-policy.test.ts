import { describe, expect, it } from "vitest";
import { parseTelemetryPolicyDiskRecord, telemetryPolicyRetryable, telemetryPolicySnapshot, type TelemetryPolicyDiskRecord, type TelemetryPolicyView } from "./telemetry-policy";

describe("telemetry policy wire record", () => {
	it("accepts a version 3 record and preserves the GitHub-handle opt-out", () => {
		expect(parseTelemetryPolicyDiskRecord(JSON.stringify({
			schema_version: 3,
			events_enabled: false,
			consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
			consent_production_enabled: true,
			github_identity_enabled: false,
			updated_at: "2026-08-28T10:15:30.000Z",
		}))).toEqual({ ok: true, record: {
			schema_version: 3,
			events_enabled: false,
			consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
			consent_production_enabled: true,
			github_identity_enabled: false,
			updated_at: "2026-08-28T10:15:30.000Z",
		} });
	});

	it("upgrades a version 2 record to v3 and defaults the GitHub-handle opt-in on", () => {
		expect(parseTelemetryPolicyDiskRecord(JSON.stringify({
			schema_version: 2,
			events_enabled: false,
			consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
			consent_production_enabled: true,
			updated_at: "2026-08-28T10:15:30.000Z",
		}))).toEqual({ ok: true, record: {
			schema_version: 3,
			events_enabled: false,
			consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
			consent_production_enabled: true,
			github_identity_enabled: true,
			updated_at: "2026-08-28T10:15:30.000Z",
		} });
	});

	it("reads a version 1 record as consent given while the release gate was closed", () => {
		expect(parseTelemetryPolicyDiskRecord(JSON.stringify({
			schema_version: 1,
			events_enabled: true,
			consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
			updated_at: "2026-08-28T10:15:30.000Z",
		}))).toEqual({ ok: true, record: {
			schema_version: 3,
			events_enabled: true,
			consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
			consent_production_enabled: false,
			github_identity_enabled: true,
			updated_at: "2026-08-28T10:15:30.000Z",
		} });
	});

	it.each([
		"{}",
		'{"schema_version":2,"events_enabled":false,"consent_generation":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","updated_at":"2026-08-28T10:15:30.000Z"}',
		'{"schema_version":1,"events_enabled":false,"consent_generation":"not-a-uuid","updated_at":"2026-08-28T10:15:30.000Z"}',
		'{"schema_version":1,"events_enabled":false,"consent_generation":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","updated_at":"yesterday"}',
		'{"schema_version":1,"events_enabled":false,"consent_generation":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","updated_at":"2026-08-28T10:15:30.000Z","extra":true}',
		'{"schemaVersion":1,"eventsEnabled":false,"consentGeneration":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","updatedAt":"2026-08-28T10:15:30.000Z"}',
		'{"schema_version":1,"events_enabled":true,"consent_generation":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}',
		'{"schema_version":2,"events_enabled":true,"consent_generation":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","consent_production_enabled":"yes","updated_at":"2026-08-28T10:15:30.000Z"}',
		'{"schema_version":3,"events_enabled":true,"consent_generation":"7f80c8a9-ec67-4a16-a067-a444ffcc5cca","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}',
	])("fails closed for malformed or expanded records: %s", (raw) => {
		expect(parseTelemetryPolicyDiskRecord(raw)).toEqual({ ok: false, reason: "invalid_record" });
	});
});

describe("telemetryPolicySnapshot", () => {
	const record = (eventsEnabled: boolean, consentProductionEnabled: boolean, githubIdentityEnabled = true): TelemetryPolicyDiskRecord => ({
		schema_version: 3,
		events_enabled: eventsEnabled,
		consent_generation: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
		consent_production_enabled: consentProductionEnabled,
		github_identity_enabled: githubIdentityEnabled,
		updated_at: "2026-08-28T10:15:30.000Z",
	});

	it("keeps an opt-in given while the gate is closed for as long as it stays closed", () => {
		expect(telemetryPolicySnapshot(record(true, false), true, false).eventsEnabled).toBe(true);
	});

	it("does not resume an opt-in given while the gate was closed once the gate opens", () => {
		expect(telemetryPolicySnapshot(record(true, false), true, true).eventsEnabled).toBe(false);
	});

	it("resumes an opt-in given while the gate was open", () => {
		expect(telemetryPolicySnapshot(record(true, true), true, true).eventsEnabled).toBe(true);
	});

	it("flags an opt-in that has to be asked for again only once the gate opens", () => {
		expect(telemetryPolicySnapshot(record(true, false), true, true).consentRenewalRequired).toBe(true);
		expect(telemetryPolicySnapshot(record(true, false), true, false).consentRenewalRequired).toBe(false);
		expect(telemetryPolicySnapshot(record(true, true), true, true).consentRenewalRequired).toBe(false);
		expect(telemetryPolicySnapshot(record(false, false), true, true).consentRenewalRequired).toBe(false);
	});

	it("never turns an opt-out on", () => {
		expect(telemetryPolicySnapshot(record(false, true), true, true).eventsEnabled).toBe(false);
		expect(telemetryPolicySnapshot(record(false, false), true, false).eventsEnabled).toBe(false);
	});

	it("surfaces the GitHub-handle opt-in independently of failure reporting", () => {
		expect(telemetryPolicySnapshot(record(false, false, true), true, true).githubIdentityEnabled).toBe(true);
		expect(telemetryPolicySnapshot(record(true, true, false), true, true).githubIdentityEnabled).toBe(false);
	});
});

describe("telemetryPolicyRetryable", () => {
	const base: TelemetryPolicyView = {
		eventsEnabled: false,
		githubIdentityEnabled: false,
		consentGeneration: "7f80c8a9-ec67-4a16-a067-a444ffcc5cca",
		updatedAt: "2026-08-28T10:15:30.000Z",
		acknowledged: false,
		consentRenewalRequired: false,
		state: "cleanup_pending",
		environmentVeto: false,
		durabilitySupported: true,
	};

	it("does not retry a settled policy", () => {
		expect(telemetryPolicyRetryable({ ...base, state: "applied" })).toBe(false);
		expect(telemetryPolicyRetryable({ ...base, state: "applied", reason: "release_blocked" })).toBe(false);
	});

	it("retries transient daemon and cleanup failures", () => {
		expect(telemetryPolicyRetryable({ ...base, state: "cleanup_pending", reason: "daemon_cleanup_pending" })).toBe(true);
		expect(telemetryPolicyRetryable({ ...base, state: "cleanup_failed", reason: "cleanup_failed" })).toBe(true);
	});

	it("never retries a platform without durable policy replacement", () => {
		expect(telemetryPolicyRetryable({ ...base, state: "cleanup_failed", durabilitySupported: false, reason: "durability_unsupported" })).toBe(false);
	});

	it("keys on durabilitySupported rather than the reason label", () => {
		expect(telemetryPolicyRetryable({ ...base, state: "cleanup_failed", durabilitySupported: false, reason: "cleanup_failed" })).toBe(false);
		expect(telemetryPolicyRetryable({ ...base, state: "cleanup_failed", durabilitySupported: false, reason: "invalid_authority" })).toBe(false);
	});
});
