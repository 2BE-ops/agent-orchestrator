import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentManagerDashboard } from "./AgentManagerDashboard";

const api = vi.hoisted(() => ({
	controller: vi.fn(),
	configurations: vi.fn(),
	sessions: vi.fn(),
	outcomes: vi.fn(),
}));
vi.mock("../lib/adaptive-api", () => ({
	managerDashboardQueryRoot: ["agent-manager-dashboard"],
	getManagerController: api.controller,
	listManagerConfigurations: api.configurations,
	listProjectActiveSessions: api.sessions,
	listRoutingOutcomes: api.outcomes,
	workerSessionsOf: (sessions: Array<{ kind: string }>) => sessions.filter((session) => session.kind === "worker"),
}));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));
const navigateMock = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigateMock }));

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<AgentManagerDashboard projectId="proj" />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	vi.clearAllMocks();
	api.controller.mockResolvedValue({ state: null });
	api.configurations.mockResolvedValue({ items: [], nextCursor: "" });
	api.sessions.mockResolvedValue([]);
	api.outcomes.mockResolvedValue({ items: [], nextAfter: 0 });
});

describe("AgentManagerDashboard", () => {
	it("reports honestly when no controller is admitted", async () => {
		mount();
		expect(await screen.findByText(/No native Manager is currently admitted/)).toBeInTheDocument();
		expect(screen.getByText(/No Manager configuration recorded/)).toBeInTheDocument();
		expect(screen.getByText(/No active workers/)).toBeInTheDocument();
		expect(screen.getByText(/No routing decisions recorded/)).toBeInTheDocument();
	});

	it("shows controller status, governance and the active configuration", async () => {
		api.controller.mockResolvedValue({
			state: {
				controller: {
					actor: { kind: "AGENT_MANAGER", id: "manager" },
					configurationVersion: 4,
					createdAt: "2026-09-19T10:00:00Z",
					id: "controller-7",
					projectId: "proj",
					reason: "Admitted after routed work appeared",
				},
				dispatch: {
					configurationHash: "hash",
					controllerId: "controller-7",
					createdAt: "2026-09-19T10:00:05Z",
					sessionId: "manager-session-1",
				},
				pendingOperation: null,
			},
		});
		api.configurations.mockResolvedValue({
			items: [{
				actor: { kind: "USER", id: "local-user" },
				contentHash: "hash",
				controllerType: { contentHash: "hash", id: "type-router", name: "Router", version: 2 },
				createdAt: "2026-09-18T09:00:00Z",
				definition: {
					agentTypeId: "type-router",
					agentTypeVersion: 2,
					enabled: true,
					policy: {
						allowCreateSkills: true,
						allowCreateTypes: false,
						allowCreateVersions: true,
						maxCreatedSkills: 5,
						maxCreatedTypes: 2,
						maxPendingRequests: 20,
					},
					schemaVersion: 1,
				},
				number: 4,
				projectId: "proj",
				reason: "Tighten creation policy",
			}],
			nextCursor: "",
		});
		mount();
		expect(await screen.findByText("controller-7")).toBeInTheDocument();
		expect(screen.getByText("v4")).toBeInTheDocument();
		expect(screen.getByText("Active")).toBeInTheDocument();
		expect(screen.getByText("Router · v2")).toBeInTheDocument();
		expect(screen.getByText("May create Skills")).toBeInTheDocument();
		expect(screen.getByText("May create versions")).toBeInTheDocument();
		expect(screen.queryByText("May create Agent Types")).not.toBeInTheDocument();
		expect(screen.getByText(/Tighten creation policy/)).toBeInTheDocument();
	});

	it("opens the manager session from the controller dispatch", async () => {
		const user = userEvent.setup();
		api.controller.mockResolvedValue({
			state: {
				controller: {
					actor: { kind: "AGENT_MANAGER", id: "manager" },
					configurationVersion: 4,
					createdAt: "2026-09-19T10:00:00Z",
					id: "controller-7",
					projectId: "proj",
					reason: "Admitted",
				},
				dispatch: {
					configurationHash: "hash",
					controllerId: "controller-7",
					createdAt: "2026-09-19T10:00:05Z",
					sessionId: "manager-session-1",
				},
				pendingOperation: null,
			},
		});
		mount();
		await user.click(await screen.findByRole("button", { name: "Open session" }));
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj", sessionId: "manager-session-1" },
		});
	});

	it("counts and lists only the active worker population", async () => {
		const user = userEvent.setup();
		api.sessions.mockResolvedValue([
			{
				activity: { kind: "working", at: "2026-09-19T12:00:00Z" },
				autoInjectCI: false,
				autoInjectReview: false,
				autoReviewEnabled: false,
				chatProviderPreserved: false,
				createdAt: "2026-09-19T11:00:00Z",
				displayName: "Storage worker",
				displayStatus: "Working",
				harness: "codex",
				id: "worker-1",
				isPinned: false,
				isTerminated: false,
				kanbanColumn: "building",
				kind: "worker",
				mode: "chat",
				model: "gpt-5.2-codex",
				prs: [],
				projectId: "proj",
				status: "working",
				statusReadiness: "ready",
				terminateOnPrMerge: false,
				updatedAt: "2026-09-19T12:00:00Z",
			},
			{
				activity: { kind: "idle", at: "2026-09-19T12:00:00Z" },
				autoInjectCI: false,
				autoInjectReview: false,
				autoReviewEnabled: false,
				chatProviderPreserved: false,
				createdAt: "2026-09-19T11:00:00Z",
				displayName: "Manager chat",
				displayStatus: "Working",
				harness: "codex",
				id: "chat-1",
				isPinned: false,
				isTerminated: false,
				kanbanColumn: "building",
				kind: "chat",
				mode: "chat",
				prs: [],
				projectId: "proj",
				status: "working",
				statusReadiness: "ready",
				terminateOnPrMerge: false,
				updatedAt: "2026-09-19T12:00:00Z",
			},
		]);
		mount();
		expect(await screen.findByRole("button", { name: "Storage worker" })).toBeInTheDocument();
		expect(screen.getByText("1 active")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Manager chat" })).not.toBeInTheDocument();
		expect(screen.getByText("gpt-5.2-codex")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Storage worker" }));
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj", sessionId: "worker-1" },
		});
	});

	it("explains why the manager accepted and rejected work", async () => {
		api.outcomes.mockResolvedValue({
			items: [
				{
					attempts: 2,
					decidedAt: "2026-09-19T12:10:00Z",
					decisionId: "decision-2",
					optimization: "balanced",
					outcome: "rejected",
					reason: "Only one harness has the required tool; its parallel cap is saturated",
					requestId: "request-2",
					revision: 1,
					sequence: 2,
					state: "pending",
					taskId: "task-2",
					taskTitle: "Harden parser",
				},
				{
					agentType: { contentHash: "hash", id: "type-coder", name: "Backend Coder", version: 3 },
					attempts: 1,
					decidedAt: "2026-09-19T12:00:00Z",
					decisionId: "decision-1",
					optimization: "balanced",
					outcome: "accepted",
					reason: "Pinned exact version keeps the frozen provider binding",
					requestId: "request-1",
					revision: 1,
					sequence: 1,
					state: "completed",
					taskId: "task-1",
					taskTitle: "Set up storage",
				},
			],
			nextAfter: 2,
		});
		mount();
		expect(await screen.findByText("Accepted")).toBeInTheDocument();
		expect(screen.getByText("Rejected")).toBeInTheDocument();
		expect(screen.getByText("Backend Coder · v3")).toBeInTheDocument();
		expect(screen.getByText(/parallel cap is saturated/)).toBeInTheDocument();
		expect(screen.getByText(/Routed task Set up storage · completed · 1 attempt/)).toBeInTheDocument();
	});

	it("pages older decisions through the sequence cursor", async () => {
		const user = userEvent.setup();
		api.outcomes.mockResolvedValueOnce({
			items: [{
				attempts: 0,
				decidedAt: "2026-09-19T12:00:00Z",
				decisionId: "decision-1",
				optimization: "balanced",
				outcome: "accepted",
				reason: "First",
				requestId: "request-1",
				revision: 1,
				sequence: 7,
				state: "pending",
				taskId: "task-1",
				taskTitle: "Set up storage",
			}],
			nextAfter: 7,
		});
		api.outcomes.mockResolvedValueOnce({ items: [], nextAfter: 0 });
		mount();
		await user.click(await screen.findByRole("button", { name: "Load more" }));
		expect(api.outcomes).toHaveBeenNthCalledWith(2, "proj", 7);
	});

	it("surfaces a recoverable decisions failure", async () => {
		api.outcomes.mockRejectedValue(new Error("Daemon unavailable"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent("Daemon unavailable");
		expect(screen.getAllByRole("button", { name: "Retry" }).length).toBeGreaterThan(0);
	});
});
