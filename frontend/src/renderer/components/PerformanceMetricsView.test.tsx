import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PerformanceMetricsView } from "./PerformanceMetricsView";

const api = vi.hoisted(() => ({
	summary: vi.fn(),
	routing: vi.fn(),
	planning: vi.fn(),
	attempts: vi.fn(),
}));
vi.mock("../lib/adaptive-api", () => ({
	performanceQueryRoot: ["adaptive-performance"],
	defaultMetricsWindow: () => ({ from: "2026-08-20T10:00:00.000Z", to: "2026-09-19T10:00:00.000Z" }),
	getTaskPerformanceSummary: api.summary,
	getManagerRoutingSummary: api.routing,
	getOrchestratorPlanningSummary: api.planning,
	listTaskPerformance: api.attempts,
}));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<PerformanceMetricsView projectId="proj" />
		</QueryClientProvider>,
	);
}

function metrics(overrides: Record<string, number> = {}) {
	return {
		attempts: 0,
		assessedPassed: 0,
		assessedFailed: 0,
		inconclusive: 0,
		unassessed: 0,
		superseded: 0,
		firstPassCompleted: 0,
		resultRevisions: 0,
		retryAttempts: 0,
		ciFailureAttempts: 0,
		reviewChangesRequested: 0,
		closedReservationSamples: 0,
		closedReservationElapsedMs: 0,
		ongoingReservations: 0,
		unseededAttempts: 0,
		mixedConfigurationAttempts: 0,
		usage: {
			events: 0,
			estimatedEvents: 0,
			incompleteAttempts: 0,
			inputTokens: 0,
			knownInputSamples: 0,
			knownOutputSamples: 0,
			nativeReportedEvents: 0,
			outputTokens: 0,
			pricedCostNanos: 0,
			pricedEvents: 0,
			pricedInputCostNanos: 0,
			pricedOutputCostNanos: 0,
			unknownEvents: 0,
		},
		...overrides,
	};
}

function summaryPage() {
	return {
		groupBy: "agent_type_version",
		from: "2026-08-20T10:00:00.000Z",
		to: "2026-09-19T10:00:00.000Z",
		observedAt: "2026-09-19T11:00:00.000Z",
		total: metrics({ attempts: 12, assessedPassed: 7, assessedFailed: 3, retryAttempts: 2 }),
		groups: [
			{ key: "type-a", version: 2, name: "code-writer", metrics: metrics({ attempts: 8, assessedPassed: 5, assessedFailed: 2, firstPassCompleted: 4 }) },
			{ key: "type-b", name: "reviewer", metrics: metrics({ attempts: 4, assessedPassed: 2, assessedFailed: 1 }) },
		],
		excludedMixedConfigurationAttempts: 1,
		excludedUnseededAttempts: 2,
	};
}

function routingPage() {
	return {
		summary: {
			from: "2026-08-20T10:00:00.000Z",
			to: "2026-09-19T10:00:00.000Z",
			observedAt: "2026-09-19T11:00:00.000Z",
			totals: {
				decisions: 9,
				accepted: 7,
				rejected: 2,
				routedTaskStates: { completed: 4, failed: 1, cancelled: 0, cancelling: 0, working: 1, pending: 1 },
				routedTaskAttempts: 8,
			},
			types: [
				{ agentTypeId: "type-a", name: "code-writer", version: 2, routed: 5, completed: 3, failed: 1, cancelled: 0, open: 1 },
			],
		},
	};
}

function planningPage() {
	return {
		summary: {
			from: "2026-08-20T10:00:00.000Z",
			to: "2026-09-19T10:00:00.000Z",
			observedAt: "2026-09-19T11:00:00.000Z",
			totals: {
				receipts: 6,
				createTask: 4,
				reviseTask: 1,
				freezeCriteria: 1,
				plannedTaskStates: { completed: 2, failed: 0, cancelled: 0, cancelling: 0, working: 1, pending: 1 },
				plannedTaskAttempts: 3,
			},
		},
	};
}

function attemptRow(overrides: Record<string, unknown> = {}) {
	return {
		attemptId: "attempt-1",
		taskId: "task-9",
		sessionId: "session-1",
		taskRevision: 1,
		criteriaVersion: 1,
		attemptNumber: 1,
		createdAt: "2026-09-18T09:00:00.000Z",
		category: "feature",
		requiredCapabilities: [],
		configuration: {
			agentType: { contentHash: "hash", id: "type-a", name: "code-writer", version: 2 },
			attemptNumber: 1,
			category: "feature",
			configurationHash: "chash",
			configurationSequence: 1,
			harness: "codex",
			mode: "agent",
			model: "gpt-5.2-codex",
			resultNumber: 1,
			skills: [],
		},
		mixedConfigurations: false,
		resultVersions: 1,
		evaluations: 1,
		assessedOutcome: "passed",
		firstPassCompleted: true,
		ciFailureObserved: false,
		reviewChangesRequested: 0,
		reservationElapsedMs: 120000,
		reservationOngoing: false,
		usage: { scope: "session", events: 3, nativeReportedEvents: 3, estimatedEvents: 0, unknownEvents: 0, inputTokens: 4200, outputTokens: 900, pricedEvents: 3, pricedCostNanos: 150, incomplete: false },
		...overrides,
	};
}

beforeEach(() => {
	vi.clearAllMocks();
	api.summary.mockResolvedValue(summaryPage());
	api.routing.mockResolvedValue(routingPage());
	api.planning.mockResolvedValue(planningPage());
	api.attempts.mockResolvedValue({ items: [attemptRow()], nextCursor: "", from: "x", to: "y", observedAt: "z" });
});

describe("PerformanceMetricsView", () => {
	it("renders cohort summary, groups, routing and planning summaries and attempt evidence", async () => {
		mount();
		expect((await screen.findAllByText(/code-writer/)).length).toBeGreaterThan(0);
		expect(screen.getAllByText(/reviewer/).length).toBeGreaterThan(0);
		expect(screen.getByText("9 decisions")).toBeInTheDocument();
		expect(screen.getByText("6 receipts")).toBeInTheDocument();
		expect(screen.getByText(/code-writer v2 · gpt-5\.2-codex/)).toBeInTheDocument();
		expect(screen.getByText("passed")).toBeInTheDocument();
		expect(screen.getByText(/exclude 1 mixed-configuration and 2 unseeded attempts/i)).toBeInTheDocument();
	});

	it("applies a new window and refetches every summary with the committed bounds", async () => {
		mount();
		expect((await screen.findAllByText(/code-writer/)).length).toBeGreaterThan(0);
		const expectedFrom = new Date("2026-09-01T08:00").toISOString();
		fireEvent.change(screen.getByLabelText("From", { selector: "input" }), { target: { value: "2026-09-01T08:00" } });
		await userEvent.click(screen.getByRole("button", { name: "Apply window" }));
		await waitFor(() => {
			expect(api.summary).toHaveBeenLastCalledWith("proj", expectedFrom, expect.any(String), "agent_type_version");
		});
		expect(api.routing).toHaveBeenLastCalledWith("proj", expectedFrom, expect.any(String));
		expect(api.planning).toHaveBeenLastCalledWith("proj", expectedFrom, expect.any(String));
	});

	it("disables applying an inverted window and explains why", async () => {
		mount();
		expect((await screen.findAllByText(/code-writer/)).length).toBeGreaterThan(0);
		fireEvent.change(screen.getByLabelText("To", { selector: "input" }), { target: { value: "2026-07-01T08:00" } });
		fireEvent.change(screen.getByLabelText("From", { selector: "input" }), { target: { value: "2026-08-01T08:00" } });
		const apply = screen.getByRole("button", { name: "Apply window" });
		expect(apply).toBeDisabled();
		expect(screen.getByRole("alert")).toHaveTextContent(/From must be before to/i);
	});

	it("surfaces a summary failure with a retry path", async () => {
		api.summary.mockRejectedValue(new Error("performance requires a project"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent(/performance requires a project/);
		api.summary.mockResolvedValue(summaryPage());
		await userEvent.click(screen.getByRole("button", { name: "Retry" }));
		expect((await screen.findAllByText(/code-writer/)).length).toBeGreaterThan(0);
	});

	it("pages attempt evidence by cursor", async () => {
		api.attempts.mockResolvedValueOnce({ items: [attemptRow()], nextCursor: "cursor-2", from: "x", to: "y", observedAt: "z" });
		mount();
		expect(await screen.findByText(/task-9/)).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Load more" }));
		await waitFor(() => expect(api.attempts).toHaveBeenNthCalledWith(2, "proj", "2026-08-20T10:00:00.000Z", "2026-09-19T10:00:00.000Z", "cursor-2"));
	});

	it("reports empty cohorts honestly", async () => {
		api.summary.mockResolvedValue({ ...summaryPage(), total: metrics(), groups: [], excludedMixedConfigurationAttempts: 0, excludedUnseededAttempts: 0 });
		api.routing.mockResolvedValue({ summary: { ...routingPage().summary, totals: { decisions: 0, accepted: 0, rejected: 0, routedTaskStates: { completed: 0, failed: 0, cancelled: 0, cancelling: 0, working: 0, pending: 0 }, routedTaskAttempts: 0 }, types: [] } });
		api.planning.mockResolvedValue({ summary: { ...planningPage().summary, totals: { receipts: 0, createTask: 0, reviseTask: 0, freezeCriteria: 0, plannedTaskStates: { completed: 0, failed: 0, cancelled: 0, cancelling: 0, working: 0, pending: 0 }, plannedTaskAttempts: 0 } } });
		api.attempts.mockResolvedValue({ items: [], nextCursor: "", from: "x", to: "y", observedAt: "z" });
		mount();
		expect(await screen.findByText(/No admitted attempts in this window/i)).toBeInTheDocument();
		expect(screen.getByText(/No accepted routing in this window/i)).toBeInTheDocument();
		expect(screen.getByText(/No attempts were admitted in this window/i)).toBeInTheDocument();
	});
});
