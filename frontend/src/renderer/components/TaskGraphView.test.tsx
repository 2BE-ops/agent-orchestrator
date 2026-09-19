import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdaptiveTaskView } from "../lib/adaptive-api";
import { TaskGraphView } from "./TaskGraphView";

const api = vi.hoisted(() => ({ list: vi.fn(), attempts: vi.fn() }));
vi.mock("../lib/adaptive-api", () => ({
	adaptiveTasksQueryRoot: ["adaptive-tasks"],
	taskPhases: ["planned", "blocked", "ready", "leased", "working", "failed", "completed", "needs-human", "cancelling", "cancelled"],
	listProjectTasks: api.list,
	listTaskAttempts: api.attempts,
}));
vi.mock("../lib/api-client", () => ({ apiErrorMessage: (error: unknown) => error instanceof Error ? error.message : String(error) }));
const navigateMock = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigateMock }));

let sequence = 0;
function taskView(overrides: Partial<AdaptiveTaskView> & {
	id: string;
	title: string;
	phase?: AdaptiveTaskView["state"]["phase"];
	reason?: string;
	parentId?: string;
	dependencies?: string[];
}): AdaptiveTaskView {
	sequence += 1;
	return {
		task: {
			id: overrides.id,
			projectId: "proj",
			revision: 1,
			createdAt: "2026-09-19T12:00:00Z",
			updatedAt: "2026-09-19T12:00:00Z",
			createdBy: { kind: "ORCHESTRATOR", id: "orch-1", sessionId: "sess-orch" },
		},
		revision: {
			actor: { kind: "ORCHESTRATOR", id: "orch-1", sessionId: "sess-orch" },
			contentHash: `hash-${sequence}`,
			createdAt: "2026-09-19T12:00:00Z",
			criteriaVersion: 1,
			number: 1,
			reason: "Initial plan",
			taskId: overrides.id,
			definition: {
				title: overrides.title,
				brief: `Brief for ${overrides.title}`,
				category: "implementation",
				dependencies: overrides.dependencies ?? null,
				maxAttempts: 3,
				parentId: overrides.parentId,
				priority: 5,
				requiredCapabilities: null,
			},
		},
		state: {
			phase: overrides.phase ?? "planned",
			reason: overrides.reason ?? "Waiting for admission",
			requiresReconciliation: false,
		},
		intent: {
			actor: { kind: "USER", id: "local-user" },
			createdAt: "2026-09-19T12:00:00Z",
			intent: "run",
			reason: "Plan approved",
			taskId: overrides.id,
			taskRevision: 1,
			version: 1,
		},
		completion: {
			taskId: overrides.id,
			taskRevision: 1,
			reason: "",
			verified: false,
		},
		criteria: {
			actor: { kind: "USER", id: "local-user" },
			contentHash: "criteria-hash",
			createdAt: "2026-09-19T12:00:00Z",
			definition: {
				criteria: [
					{ id: "c1", evidenceKind: "test", requirement: "go test ./... passes" },
				],
			},
			number: 1,
			previousVersion: 0,
			reason: "Frozen before work",
			taskId: overrides.id,
		},
		...overrides,
	};
}

function mount() {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={client}>
			<TaskGraphView projectId="proj" />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	vi.clearAllMocks();
	sequence = 0;
	api.attempts.mockResolvedValue({ items: [] });
});

describe("TaskGraphView", () => {
	it("renders the dependency graph with reachable nodes and phase facts", async () => {
		api.list.mockResolvedValue({
			items: [
				taskView({ id: "setup", title: "Set up storage" }),
				taskView({ id: "build", title: "Build service", phase: "blocked", reason: "Waits on dependencies", dependencies: ["setup"] }),
			],
			nextCursor: "",
		});
		mount();
		const build = await screen.findByRole("button", { name: /Build service/ });
		expect(build).toHaveAttribute("aria-pressed", "false");
		expect(screen.getByRole("button", { name: /Set up storage/ })).toBeInTheDocument();
		// Phase facts travel with the node for screen-reader users.
		expect(build.textContent).toContain("blocked");
	});

	it("switches to the accessible list view with the same facts", async () => {
		const user = userEvent.setup();
		api.list.mockResolvedValue({
			items: [taskView({ id: "setup", title: "Set up storage", phase: "ready", reason: "Ready for a worker" })],
			nextCursor: "",
		});
		mount();
		await screen.findByRole("button", { name: /Set up storage/ });
		await user.click(screen.getByRole("button", { name: "List" }));
		expect(screen.getByRole("list", { name: "Task list" })).toBeInTheDocument();
		expect(screen.getByText("Ready for a worker")).toBeInTheDocument();
	});

	it("shows task detail with criteria, structure and dependency navigation", async () => {
		const user = userEvent.setup();
		api.list.mockResolvedValue({
			items: [
				taskView({ id: "setup", title: "Set up storage" }),
				taskView({ id: "build", title: "Build service", phase: "blocked", dependencies: ["setup"], parentId: "setup" }),
			],
			nextCursor: "",
		});
		mount();
		await user.click(await screen.findByRole("button", { name: /Build service/ }));
		expect(await screen.findByText("Brief for Build service")).toBeInTheDocument();
		expect(screen.getByText("go test ./... passes")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Parent: Set up storage/ })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Set up storage · planned/ })).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: /Set up storage · planned/ }));
		expect(await screen.findByText("Brief for Set up storage")).toBeInTheDocument();
	});

	it("opens the dispatched worker session from an attempt", async () => {
		const user = userEvent.setup();
		api.list.mockResolvedValue({
			items: [taskView({ id: "build", title: "Build service", phase: "working" })],
			nextCursor: "",
		});
		api.attempts.mockResolvedValue({
			items: [{
				attempt: {
					actor: { kind: "AGENT_MANAGER", id: "manager" },
					createdAt: "2026-09-19T12:30:00Z",
					criteriaVersion: 1,
					dependencies: [],
					id: "attempt-1",
					launchIntentId: "intent-1",
					number: 1,
					reason: "Dispatched",
					taskId: "build",
					taskRevision: 1,
				},
				dispatch: {
					attemptId: "attempt-1",
					configurationHash: "hash",
					createdAt: "2026-09-19T12:30:00Z",
					sessionId: "worker-session-1",
				},
				lease: {
					attemptId: "attempt-1",
					expiresAt: "2026-09-19T13:30:00Z",
					generation: 1,
					heartbeatAt: "2026-09-19T12:31:00Z",
					taskId: "build",
				},
			}],
			nextCursor: "",
		});
		mount();
		await user.click(await screen.findByRole("button", { name: /Build service/ }));
		await user.click(await screen.findByRole("button", { name: "Open worker session" }));
		expect(navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/sessions/$sessionId",
			params: { projectId: "proj", sessionId: "worker-session-1" },
		});
	});

	it("filters by phase without hiding the filtered-in work", async () => {
		const user = userEvent.setup();
		api.list.mockResolvedValue({
			items: [
				taskView({ id: "setup", title: "Set up storage", phase: "ready" }),
				taskView({ id: "build", title: "Build service", phase: "failed", reason: "Exhausted attempts" }),
			],
			nextCursor: "",
		});
		mount();
		await screen.findByRole("button", { name: /Set up storage/ });
		await user.selectOptions(screen.getByLabelText("Phase"), "failed");
		expect(screen.queryByRole("button", { name: /Set up storage/ })).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: /Build service/ })).toBeInTheDocument();
	});

	it("shows a recoverable load failure", async () => {
		api.list.mockRejectedValue(new Error("Daemon unavailable"));
		mount();
		expect(await screen.findByRole("alert")).toHaveTextContent("Daemon unavailable");
		expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
	});

	it("pages additional task pages through the cursor", async () => {
		const user = userEvent.setup();
		api.list.mockResolvedValueOnce({ items: [taskView({ id: "setup", title: "Set up storage" })], nextCursor: "cursor-2" });
		api.list.mockResolvedValueOnce({ items: [taskView({ id: "build", title: "Build service" })], nextCursor: "" });
		mount();
		await user.click(await screen.findByRole("button", { name: "Load more" }));
		expect(await screen.findByRole("button", { name: /Build service/ })).toBeInTheDocument();
		expect(api.list).toHaveBeenNthCalledWith(2, "proj", "cursor-2");
	});

	it("shows the empty state for a project without task intent", async () => {
		api.list.mockResolvedValue({ items: [], nextCursor: "" });
		mount();
		expect(await screen.findByText("No task intent recorded for this project yet.")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
	});

	it("marks work that waits for human input", async () => {
		const user = userEvent.setup();
		api.list.mockResolvedValue({
			items: [taskView({ id: "stuck", title: "Blocked migration", phase: "needs-human", reason: "approval_required: choose a path" })],
			nextCursor: "",
		});
		mount();
		await user.click(await screen.findByRole("button", { name: /Blocked migration/ }));
		expect(await screen.findByText(/choose a path/)).toBeInTheDocument();
		expect(screen.getAllByText("needs-human").length).toBeGreaterThan(0);
	});
});
